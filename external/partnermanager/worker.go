package partnermanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/ooaklee/ghatd/external/referral"
	"github.com/ooaklee/ghatd/external/user/v2"
)

// SignupFeed is the owning identity creation/consumption capability. Discovery
// does not read current profile fields or manufacture capture for old accounts.
type SignupFeed interface {
	// GetSignupAttribution returns the SignupAttribution for the identified signup,
	// a read owned by the identity-creating SignupFeed.
	GetSignupAttribution(context.Context, string) (user.SignupAttribution, error)
	// PendingSignupAttributionsAfter returns pending SignupAttributions after the
	// cursor bounded by the count, feeding discovery from the signup owner.
	PendingSignupAttributionsAfter(context.Context, string, int) ([]user.SignupAttribution, error)
	// ConsumeSignupAttribution records the SignupConsumption for the identified
	// attribution, consuming it within the owning feed.
	ConsumeSignupAttribution(context.Context, string, user.SignupConsumption) error
}

// RevenueFeed combines immutable billing inputs and independent consumer
// receipts. Acknowledgements are separate from source quarantine resolution.
type RevenueFeed interface {
	// GetRevenueFact reads the immutable billing RevenueFact for the identified
	// source, an input owned by RevenueFeed.
	GetRevenueFact(context.Context, string) (billing.RevenueFact, error)
	// PendingRevenueFactsAfter returns pending RevenueFacts after the cursor
	// identifier and revision bounded by the count, feeding revenue discovery.
	PendingRevenueFactsAfter(context.Context, string, int64, int) ([]billing.RevenueFact, error)
	// GetRevenueAcknowledgement reads the stored RevenueAcknowledgement for the
	// identified fact and consumer, an independent receipt within RevenueFeed.
	GetRevenueAcknowledgement(context.Context, string, string) (billing.RevenueAcknowledgement, error)
	// AcknowledgeRevenueFact stores the supplied RevenueAcknowledgement, recording
	// consumption separately from source quarantine resolution.
	AcknowledgeRevenueFact(context.Context, billing.RevenueAcknowledgement) error
	// GetRevenueObservation returns the RevenueObservation for the identified
	// source, a read owned by the revenue feed.
	GetRevenueObservation(context.Context, string) (billing.RevenueObservation, error)
	// GetRevenueSourceResolution returns the RevenueObservation resolving the
	// identified revenue source's quarantine within the feed.
	GetRevenueSourceResolution(context.Context, string) (billing.RevenueObservation, error)
	// UnresolvedRevenueObservationsAfter returns unresolved RevenueObservations
	// after the cursor bounded by the count, driving source reconciliation.
	UnresolvedRevenueObservationsAfter(context.Context, string, int) ([]billing.RevenueObservation, error)
}

// RevenueSourceReconciler authenticates provider evidence and current scoped
// authority. The billing manager implements this owning capability; the worker
// never interprets raw provider objects or accepts a browser payment assertion.
type RevenueSourceReconciler interface {
	// ReconcileRevenueSource authenticates provider evidence per the
	// ReconcileRevenueSourceRequest and returns the resulting RevenueObservation;
	// workers never interpret raw provider objects.
	ReconcileRevenueSource(context.Context, billingmanager.ReconcileRevenueSourceRequest) (billing.RevenueObservation, error)
}

// WorkerConfig holds the worker identity used for authorization and receipts,
// the revenue ConsumerID, and discovery/processing page and batch sizes.
type WorkerConfig struct {
	ActorID, ConsumerID string
	PageSize, BatchSize int
}

// WorkerReport contains bounded operational counts and structured issue codes.
// Source identities and raw dependency errors are deliberately not report data.
type WorkerReport struct {
	Discovered, Completed, Retried int
	Issues                         []WorkerIssue
}

// WorkerIssue is a bounded structured issue classification; it carries no
// source identity or raw dependency error.
type WorkerIssue struct{ Kind, Code string }

// Worker performs one bounded sweep/attempt batch. The host owns scheduling,
// cancellation and enablement. All capabilities are required even when new
// commercial admission is paused, so existing obligations remain recoverable.
type Worker struct {
	manager    *Manager
	queue      *WorkQueue
	signups    SignupFeed
	revenue    RevenueFeed
	reconciler RevenueSourceReconciler
	maturity   MaturityFeed
	config     WorkerConfig
}

// NewWorker validates all capabilities and cross-owner configuration: the
// manager, queue, maturity feed program/currency agreement and worker config
// bounds. It revalidates the manager and queue wiring and fails closed on any
// mismatch; it starts nothing.
func NewWorker(manager *Manager, queue *WorkQueue, signups SignupFeed, revenue RevenueFeed, reconciler RevenueSourceReconciler, config WorkerConfig) (*Worker, error) {
	if manager == nil || queue == nil || nilManagerDependency(signups) || nilManagerDependency(revenue) || nilManagerDependency(reconciler) {
		return nil, ErrUnavailable
	}
	if _, err := NewManager(manager.deps); err != nil {
		return nil, err
	}
	if _, err := NewWorkQueue(queue.repo, queue.clock, queue.config); err != nil {
		return nil, err
	}
	maturity, ok := manager.deps.Earnings.(MaturityFeed)
	if !ok || nilManagerDependency(maturity) {
		return nil, ErrUnavailable
	}
	financial := maturity.Config()
	if financial.Validate() != nil || financial.ProgramID != queue.config.ProgramID || financial.Currency != manager.deps.Program.Config().Currency {
		return nil, ErrInvalid
	}
	if queue.config.ProgramID != partnerprogram.ProgramID || !validWorkText(config.ActorID, 256) || !validWorkText(config.ConsumerID, 256) || config.PageSize < 1 || config.PageSize > 200 || config.BatchSize < 1 || config.BatchSize > 200 {
		return nil, ErrInvalid
	}
	return &Worker{manager: manager, queue: queue, signups: signups, revenue: revenue, reconciler: reconciler, maturity: maturity, config: config}, nil
}

// authorize maps a work kind to its capability (signup, maturity or revenue
// worker) and checks current authority for the configured ActorID against the
// target source.
func (w *Worker) authorize(ctx context.Context, kind, target string) error {
	capability := CapabilityRevenueWorker
	if kind == WorkSignup {
		capability = CapabilitySignupWorker
	} else if kind == WorkMaturity {
		capability = CapabilityMaturityWorker
	}
	return w.manager.authorize(ctx, w.config.ActorID, capability, target)
}

// RunOnce discovers one page per kind and attempts one due batch per kind.
// Poison sources persist with independent backoff; a failed item cannot prevent
// later work in the batch or later discovery pages. Cancellation ends the run.
func (w *Worker) RunOnce(ctx context.Context) (WorkerReport, error) {
	var report WorkerReport
	if ctx == nil {
		return report, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return report, err
	}
	if w == nil || w.manager == nil || w.queue == nil {
		return report, ErrUnavailable
	}
	var failures []error
	for _, kind := range []string{WorkSignup, WorkRevenueSource, WorkRevenue, WorkMaturity} {
		if err := w.authorize(ctx, kind, ""); err != nil {
			return report, err
		}
		n, err := w.discover(ctx, kind)
		if err != nil {
			if ctx.Err() != nil {
				return report, ctx.Err()
			}
			report.Issues = append(report.Issues, WorkerIssue{kind, "discovery_pending"})
			failures = append(failures, err)
		} else {
			report.Discovered += n
		}
		if err := w.authorize(ctx, kind, ""); err != nil {
			return report, err
		}
		items, err := w.queue.Lease(ctx, kind, w.config.BatchSize)
		if err != nil {
			report.Issues = append(report.Issues, WorkerIssue{kind, "lease_pending"})
			failures = append(failures, err)
			continue
		}
		for _, item := range items {
			target := item.SourceID
			if kind == WorkMaturity {
				// Initial lookup is program-scoped. The financial owner supplies
				// the partner target; a queue source ID cannot confer that scope.
				target = ""
			}
			if err := w.authorize(ctx, kind, target); err != nil {
				return report, err
			}
			var err error
			if kind == WorkMaturity {
				target, err = w.processMaturity(ctx, item)
			} else {
				err = w.process(ctx, item)
			}
			if err == nil {
				report.Completed++
				continue
			}
			if ctx.Err() != nil {
				return report, ctx.Err()
			}
			// Authority can be revoked while an owning call is in flight. Do not
			// mutate retry state or use stored decision authorship as authority.
			if authErr := w.authorize(ctx, kind, target); authErr != nil {
				return report, authErr
			}
			code := workerErrorCode(err)
			report.Issues = append(report.Issues, WorkerIssue{kind, code})
			failures = append(failures, err)
			if kind == WorkMaturity && target == "" {
				// Missing or invalid owning evidence cannot establish partner
				// retry authority. Retain the lease until expiry and continue
				// the batch; never guess a target or acknowledge this source.
				continue
			}
			if singleManagerAbsence(err, ErrWorkLeaseLost) {
				// The owning fence already rejected this token. A retry write
				// under the same stale token cannot recover it.
				continue
			}
			if retryErr := w.queue.Retry(ctx, item, code); retryErr != nil {
				failures = append(failures, retryErr)
			} else {
				report.Retried++
			}
		}
	}
	return report, errors.Join(failures...)
}

// workerErrorCode maps dependency failures to bounded issue codes: fenced
// leases, pending receipts, conflicts, admission/evidence denial, review-
// pending unassessable facts, and missing owning evidence; everything else is
// dependency_pending. Raw errors are not propagated.
func workerErrorCode(err error) string {
	switch {
	case singleManagerAbsence(err, ErrWorkLeaseLost):
		return "lease_fenced"
	case singleManagerAbsence(err, ErrWorkUncertain):
		return "work_receipt_pending"
	case singleManagerAbsence(err, ErrWorkConflict), singleManagerAbsence(err, partnerearnings.ErrConflict), singleManagerAbsence(err, billing.ErrRevenueConflict), singleManagerAbsence(err, user.ErrSignupEvidenceConflict):
		return "source_conflict"
	case singleManagerAbsence(err, ErrDenied), singleManagerAbsence(err, referral.ErrDenied):
		return "admission_or_evidence_pending"
	case singleManagerAbsence(err, billing.ErrRevenueUnassessable), singleManagerAbsence(err, partnerearnings.ErrCurrencyMismatch):
		return "review_pending"
	case singleManagerAbsence(err, partnerearnings.ErrUnresolved), singleManagerAbsence(err, referral.ErrNotFound), singleManagerAbsence(err, user.ErrUserNotFound):
		return "owning_evidence_pending"
	default:
		return "dependency_pending"
	}
}

// sourceDigest returns the SHA-256 hex fingerprint of value's canonical JSON
// encoding, failing only when marshaling fails.
func sourceDigest(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", ErrInvalid
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// signupCandidate converts a signup attribution into queue input, validating
// program, customer text, timestamp consistency and evidence size. The
// fingerprint covers immutable private creation fields omitted from public JSON
// and excludes mutable consumption state.
func signupCandidate(c user.SignupAttribution, program string) (WorkCandidate, error) {
	at, err := time.Parse(time.RFC3339Nano, c.CreatedAtUTC)
	if err != nil || !at.Equal(c.CreatedAt) || c.CreatedAt.IsZero() || c.ProgramID != program || !validWorkText(c.CustomerID, 256) || len(c.Evidence) > 2048 {
		return WorkCandidate{}, ErrUnavailable
	}
	// Public SignupAttribution JSON omits every creation field. Include the
	// private immutable fields explicitly and exclude mutable consumption.
	fp, err := sourceDigest(struct {
		Program, Customer, CreatedAt, Evidence string
		Individual                             bool
	}{c.ProgramID, c.CustomerID, at.UTC().Format(time.RFC3339Nano), c.Evidence, c.Individual})
	return WorkCandidate{SourceID: c.CustomerID, SourceFingerprint: fp}, err
}

// revenueCandidate converts an accepted revenue fact into queue input,
// requiring a valid ID, positive sequence, fingerprint and acceptance time, and
// fingerprinting the fact with its source fingerprint.
func revenueCandidate(f billing.RevenueFact) (WorkCandidate, error) {
	if !validWorkText(f.ID, 256) || f.Sequence < 1 || !validWorkText(f.Fingerprint, 256) || f.AcceptedAt.IsZero() {
		return WorkCandidate{}, ErrUnavailable
	}
	fp, err := sourceDigest(struct {
		Fact        billing.RevenueFact
		Fingerprint string
	}{f, f.Fingerprint})
	return WorkCandidate{SourceID: f.ID, SourceFingerprint: fp}, err
}

// observationCandidate converts a quarantined revenue observation (not a
// resolution) into queue input, fingerprinting the observation with its source
// fingerprint.
func observationCandidate(o billing.RevenueObservation) (WorkCandidate, error) {
	if !validWorkText(o.ID, 256) || !validWorkText(o.Fingerprint, 256) || o.AcceptedAt.IsZero() || o.QuarantineReason == "" || o.ResolutionOf != "" {
		return WorkCandidate{}, ErrUnavailable
	}
	fp, err := sourceDigest(struct {
		Observation billing.RevenueObservation
		Source      string
	}{o, o.SourceFingerprint})
	return WorkCandidate{SourceID: o.ID, SourceFingerprint: fp}, err
}

// discover reads one page of pending sources for the kind, validating strictly
// increasing positions and candidate well-formedness, then authorizes the
// capability and enqueues the retained candidates with the cursor advance.
// Empty discovery at a fresh cursor advances nothing; maturity discovery may
// race financial completion and relies on queued rechecks.
func (w *Worker) discover(ctx context.Context, kind string) (int, error) {
	cursor, err := w.queue.Cursor(ctx, kind)
	if err != nil {
		return 0, err
	}
	next := DiscoveryCursor{}
	var candidates []WorkCandidate
	switch kind {
	case WorkSignup:
		page, err := w.signups.PendingSignupAttributionsAfter(ctx, cursor.AfterID, w.config.PageSize)
		if err != nil {
			return 0, err
		}
		for _, source := range page {
			candidate, err := signupCandidate(source, w.queue.config.ProgramID)
			if err != nil || source.State != "pending" || candidate.SourceID <= cursor.AfterID || (next.AfterID != "" && candidate.SourceID <= next.AfterID) {
				return 0, ErrUnavailable
			}
			candidates = append(candidates, candidate)
			next.AfterID = candidate.SourceID
		}
	case WorkRevenue:
		page, err := w.revenue.PendingRevenueFactsAfter(ctx, w.config.ConsumerID, cursor.AfterSequence, w.config.PageSize)
		if err != nil {
			return 0, err
		}
		for _, source := range page {
			candidate, err := revenueCandidate(source)
			if err != nil || source.Sequence <= cursor.AfterSequence || (next.AfterSequence != 0 && source.Sequence <= next.AfterSequence) {
				return 0, ErrUnavailable
			}
			candidates = append(candidates, candidate)
			next.AfterSequence = source.Sequence
		}
	case WorkRevenueSource:
		page, err := w.revenue.UnresolvedRevenueObservationsAfter(ctx, cursor.AfterID, w.config.PageSize)
		if err != nil {
			return 0, err
		}
		for _, source := range page {
			candidate, err := observationCandidate(source)
			if err != nil || candidate.SourceID <= cursor.AfterID || (next.AfterID != "" && candidate.SourceID <= next.AfterID) {
				return 0, ErrUnavailable
			}
			candidates = append(candidates, candidate)
			next.AfterID = candidate.SourceID
		}
	case WorkMaturity:
		page, err := w.maturity.PendingMaturitySourcesAfter(ctx, cursor.AfterID, w.config.PageSize)
		if err != nil {
			return 0, err
		}
		for _, source := range page {
			candidate, err := maturityCandidate(source, w.queue.config.ProgramID, w.maturity.Config().Currency)
			if err != nil || candidate.SourceID <= cursor.AfterID || (next.AfterID != "" && candidate.SourceID <= next.AfterID) {
				return 0, ErrUnavailable
			}
			// Discovery may race accepted financial completion. Both states
			// retain the same source/deadline; queued work rechecks its receipt.
			candidates = append(candidates, candidate)
			next.AfterID = candidate.SourceID
		}
	default:
		return 0, ErrInvalid
	}
	if len(candidates) > w.config.PageSize {
		return 0, ErrUnavailable
	}
	if err := w.authorize(ctx, kind, ""); err != nil {
		return 0, err
	}
	if len(candidates) == 0 && cursor.AfterID == "" && cursor.AfterSequence == 0 {
		return 0, nil
	}
	if err := w.queue.EnqueuePage(ctx, kind, cursor, next, candidates); err != nil {
		return 0, err
	}
	return len(candidates), nil
}

// process dispatches one item to its kind-specific owning use case and returns
// ErrInvalid for unknown kinds.
func (w *Worker) process(ctx context.Context, item WorkItem) error {
	switch item.Kind {
	case WorkSignup:
		return w.processSignup(ctx, item)
	case WorkRevenue:
		return w.processRevenue(ctx, item)
	case WorkRevenueSource:
		return w.processSource(ctx, item)
	case WorkMaturity:
		_, err := w.processMaturity(ctx, item)
		return err
	}
	return ErrInvalid
}

// decide rechecks current authority for the item's source before recording the
// immutable decision receipt.
func (w *Worker) decide(ctx context.Context, item WorkItem, outcome, acceptance, reason string) (WorkDecision, error) {
	if err := w.authorize(ctx, item.Kind, item.SourceID); err != nil {
		return WorkDecision{}, err
	}
	return w.queue.Decide(ctx, item, w.config.ActorID, outcome, acceptance, reason)
}

// complete rechecks current authority for the item's source before
// acknowledging the decision receipt with the queue.
func (w *Worker) complete(ctx context.Context, item WorkItem, decision WorkDecision) error {
	if err := w.authorize(ctx, item.Kind, item.SourceID); err != nil {
		return err
	}
	return w.queue.Complete(ctx, item, decision.ID)
}

// processSignup revalidates the immutable capture fingerprint, then consumes
// pending evidence through the manager (attributed), records
// no_evidence/ineligible refusals, or replays an existing decision. It verifies
// the source's consumption receipt matches the decision before completing;
// conflicts and unknown states return without discarding evidence.
func (w *Worker) processSignup(ctx context.Context, item WorkItem) error {
	capture, err := w.signups.GetSignupAttribution(ctx, item.SourceID)
	if err != nil {
		return err
	}
	candidate, err := signupCandidate(capture, item.ProgramID)
	if err != nil || candidate.SourceID != item.SourceID || candidate.SourceFingerprint != item.SourceFingerprint {
		return ErrWorkConflict
	}
	var decision WorkDecision
	if item.Decision != nil {
		decision = *item.Decision
	} else {
		if capture.State != "pending" || capture.Consumption != nil {
			return user.ErrSignupEvidenceConflict
		}
		outcome, acceptance, reason := WorkNoEntitlement, capture.CustomerID, "no_evidence"
		if !capture.Individual {
			reason = "ineligible"
		} else if capture.Evidence != "" {
			accepted, err := w.manager.ConsumeSignup(ctx, w.config.ActorID, item.SourceID)
			if err != nil {
				return err
			}
			if accepted.ID == "" || accepted.ReferredCustomer != capture.CustomerID || accepted.ProgramID != item.ProgramID {
				return ErrUnavailable
			}
			outcome, acceptance, reason = WorkAccepted, accepted.ID, "attributed"
		}
		decision, err = w.decide(ctx, item, outcome, acceptance, reason)
		if err != nil {
			return err
		}
	}
	outcome := decision.ReasonCode
	if (decision.Outcome == WorkAccepted && outcome != "attributed") || (decision.Outcome == WorkNoEntitlement && outcome != "no_evidence" && outcome != "ineligible") || (decision.Outcome != WorkAccepted && decision.Outcome != WorkNoEntitlement) {
		return ErrWorkConflict
	}
	if err := w.authorize(ctx, item.Kind, item.SourceID); err != nil {
		return err
	}
	if capture.State == "consumed" {
		if capture.Consumption == nil || capture.Consumption.ReceiptID != decision.ID || capture.Consumption.Outcome != outcome {
			return user.ErrSignupEvidenceConflict
		}
	} else if capture.State == "pending" {
		if err := w.signups.ConsumeSignupAttribution(ctx, item.SourceID, user.SignupConsumption{ReceiptID: decision.ID, Outcome: outcome, ActorID: w.config.ActorID}); err != nil {
			return err
		}
	} else {
		return user.ErrSignupEvidenceConflict
	}
	return w.complete(ctx, item, decision)
}

// processRevenue revalidates the fact fingerprint, processes it through the
// financial owner (or replays the existing decision), then cross-checks the
// consumer acknowledgement: an existing receipt must match the decision
// exactly, an absent one is written idempotently after re-authorization. Only
// after this agreement does it complete the work item.
func (w *Worker) processRevenue(ctx context.Context, item WorkItem) error {
	fact, err := w.revenue.GetRevenueFact(ctx, item.SourceID)
	if err != nil {
		return err
	}
	candidate, err := revenueCandidate(fact)
	if err != nil || candidate.SourceID != item.SourceID || candidate.SourceFingerprint != item.SourceFingerprint {
		return ErrWorkConflict
	}
	var decision WorkDecision
	if item.Decision != nil {
		decision = *item.Decision
	} else {
		_, processErr := w.manager.ProcessRevenueFact(ctx, w.config.ActorID, fact.ID)
		outcome, acceptance, reason := WorkAccepted, fact.ID, "financial_accepted"
		if processErr != nil {
			reason, err = w.refusedRevenue(ctx, fact, processErr)
			if err != nil {
				return err
			}
			outcome = WorkNoEntitlement
		}
		// Success means the financial owner committed its idempotent operation,
		// including zero-delta adjustment receipts; entries alone are not proof.
		decision, err = w.decide(ctx, item, outcome, acceptance, reason)
		if err != nil {
			return err
		}
	}
	if decision.AcceptanceID != fact.ID || (decision.Outcome != WorkAccepted && decision.Outcome != WorkNoEntitlement) {
		return ErrWorkConflict
	}
	if err := w.authorize(ctx, item.Kind, item.SourceID); err != nil {
		return err
	}
	ack, err := w.revenue.GetRevenueAcknowledgement(ctx, w.config.ConsumerID, fact.ID)
	if err == nil {
		if ack.ConsumerID != w.config.ConsumerID || ack.FactID != fact.ID || ack.AcceptanceID != decision.ID || ack.Outcome != decision.Outcome {
			return billing.ErrRevenueConflict
		}
	} else if singleManagerAbsence(err, billing.ErrRevenueNotFound) {
		if err := w.authorize(ctx, item.Kind, item.SourceID); err != nil {
			return err
		}
		if err := w.revenue.AcknowledgeRevenueFact(ctx, billing.RevenueAcknowledgement{ConsumerID: w.config.ConsumerID, FactID: fact.ID, AcceptanceID: decision.ID, Outcome: decision.Outcome, ActorID: w.config.ActorID}); err != nil {
			return err
		}
	} else {
		return err
	}
	return w.complete(ctx, item, decision)
}

// refusedRevenue derives a conclusive refusal reason for a failed financial
// operation: a verified NoEntitlementReason, an original payment's recorded no-
// entitlement for non-payment facts, or signup evidence proving
// paid_before_signup or an earlier ineligible/no-evidence consumption. Anything
// else returns the original error or ErrUnresolved; historical signup absence
// never becomes permission to discard money.
func (w *Worker) refusedRevenue(ctx context.Context, fact billing.RevenueFact, processErr error) (string, error) {
	if reason := noEntitlementReason(processErr); reason != "" {
		return reason, nil
	}
	if fact.Kind != billing.RevenuePayment && (singleManagerAbsence(processErr, partnerearnings.ErrUnresolved) || singleManagerAbsence(processErr, partnerearnings.ErrNotFound) || singleManagerAbsence(processErr, referral.ErrNotFound)) {
		original, err := w.revenue.GetRevenueAcknowledgement(ctx, w.config.ConsumerID, fact.PaymentFactID())
		if err == nil && original.ConsumerID == w.config.ConsumerID && original.FactID == fact.PaymentFactID() && original.AcceptanceID != "" && original.Outcome == WorkNoEntitlement {
			return "original_no_entitlement", nil
		}
		if err != nil && !singleManagerAbsence(err, billing.ErrRevenueNotFound) {
			return "", err
		}
	}
	if !singleManagerAbsence(processErr, referral.ErrNotFound) {
		return "", processErr
	}
	capture, err := w.signups.GetSignupAttribution(ctx, fact.PrincipalID)
	if err != nil {
		// Historical absence is not a capture or permission to discard money.
		return "", err
	}
	if _, err := signupCandidate(capture, w.queue.config.ProgramID); err != nil || capture.CustomerID != fact.PrincipalID {
		return "", ErrUnavailable
	}
	if fact.EffectiveAt.Before(capture.CreatedAt) {
		return "paid_before_signup", nil
	}
	if capture.State == "consumed" && capture.Consumption != nil && capture.Consumption.ReceiptID != "" && (capture.Consumption.Outcome == "no_evidence" || capture.Consumption.Outcome == "ineligible") {
		return "signup_" + capture.Consumption.Outcome, nil
	}
	return "", partnerearnings.ErrUnresolved
}

// processSource revalidates the observation fingerprint and resolves an
// unresolved source through the reconciler when no resolution exists, or
// replays an existing accepted decision against the existing resolution. It
// verifies resolution/observation agreement and completes only with the
// resolution identity as acceptance.
func (w *Worker) processSource(ctx context.Context, item WorkItem) error {
	source, err := w.revenue.GetRevenueObservation(ctx, item.SourceID)
	if err != nil {
		return err
	}
	candidate, err := observationCandidate(source)
	if err != nil || candidate.SourceID != item.SourceID || candidate.SourceFingerprint != item.SourceFingerprint {
		return ErrWorkConflict
	}
	resolution, err := w.revenue.GetRevenueSourceResolution(ctx, source.ID)
	if singleManagerAbsence(err, billing.ErrRevenueNotFound) {
		if item.Decision != nil {
			return billing.ErrRevenueConflict
		}
		if err := w.authorize(ctx, item.Kind, item.SourceID); err != nil {
			return err
		}
		resolution, err = w.reconciler.ReconcileRevenueSource(ctx, billingmanager.ReconcileRevenueSourceRequest{ObservationID: source.ID, ExpectedFingerprint: source.Fingerprint, Reason: "partner_source_recovery", ActorID: w.config.ActorID})
	}
	if err != nil {
		return err
	}
	if resolution.ID == "" || resolution.ResolutionOf != source.ID || resolution.Scope != source.Scope || resolution.EnvelopeID != source.EnvelopeID || resolution.SourceFingerprint != source.SourceFingerprint || resolution.Fingerprint == "" || resolution.AcceptedAt.IsZero() {
		return billing.ErrRevenueConflict
	}
	var decision WorkDecision
	if item.Decision != nil {
		decision = *item.Decision
		if decision.Outcome != WorkAccepted || decision.AcceptanceID != resolution.ID {
			return ErrWorkConflict
		}
	} else {
		decision, err = w.decide(ctx, item, WorkAccepted, resolution.ID, "source_resolved")
		if err != nil {
			return err
		}
	}
	return w.complete(ctx, item, decision)
}
