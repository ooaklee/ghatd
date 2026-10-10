package billing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
)

// ErrSubscriptionStatusStale distinguishes expired observation coverage from
// a known inactive subscription. Missing/stale status must not become zero.
var ErrSubscriptionStatusStale = errors.New("billing/subscription-status-stale")

// SubscriptionStatusCheckoutSource separates pre-payment lifecycle provenance
// from the original payment-only receipt representation. Empty Source preserves
// that original representation; no explicit payment alias is accepted.
const SubscriptionStatusCheckoutSource = "checkout-lifecycle-v1"

// SubscriptionStatusPreparation freezes owning payer/scope and the head revision
// BEFORE provider I/O. It is private in-process input, never HTTP-decoded or an
// authorization token. Capture identity supports original lost-ack replay.
type SubscriptionStatusPreparation struct {
	Source, CheckoutIntentID, CheckoutFingerprint   string       `json:"-"`
	CaptureID, FactID, FactFingerprint, ActorID     string       `json:"-"`
	Scope                                           RevenueScope `json:"-"`
	PrincipalID, ProviderCustomerID, SubscriptionID string       `json:"-"`
	ExpectedRevision                                int64        `json:"-"`
	ExpectedFingerprint                             string       `json:"-"`
	RequestedAt                                     time.Time    `json:"-"`
}

// VerifiedSubscriptionStatusEvidence comes only from authenticated provider
// lookup through the billing manager; it creates no financial entitlement.
type VerifiedSubscriptionStatusEvidence struct {
	Scope                                      RevenueScope `json:"-"`
	SubscriptionID, ProviderCustomerID, Status string       `json:"-"`
	CancellationScheduled                      bool         `json:"-"`
}

// Validate requires exact authenticated subscription/customer scope and a
// recognized lifecycle state. Unknown states are not classified as inactive.
func (e VerifiedSubscriptionStatusEvidence) Validate(p SubscriptionStatusPreparation) error {
	if p.Validate() != nil || e.Scope != p.Scope || e.SubscriptionID != p.SubscriptionID || e.ProviderCustomerID != p.ProviderCustomerID || !validStatus(e.Status) {
		return ErrRevenueConflict
	}
	return nil
}

// SubscriptionStatus retains minimal private lifecycle evidence and its original
// receipt. ObservedAt is the owning reception time, not a provider event time.
// RequestedAt/ObservedAt bound lookup; Revision orders accepted observations.
type SubscriptionStatus struct {
	Preparation           SubscriptionStatusPreparation `json:"-"`
	Status                string                        `json:"-"`
	CancellationScheduled bool                          `json:"-"`
	ObservedAt            time.Time                     `json:"-"`
	Revision              int64                         `json:"-"`
	Fingerprint           string                        `json:"-"`
}

// SubscriptionStatusSnapshot pairs current head and its immutable receipt in
// one owning read. Missing/corrupt joined evidence is not a fresh status.
type SubscriptionStatusSnapshot struct {
	Current, Receipt SubscriptionStatus `json:"-"`
}

// SubscriptionStatusTx is bound to one subscription's owning transaction.
// Callbacks may repeat; provider I/O and external effects are prohibited.
type SubscriptionStatusTx interface {
	// GetCurrent returns the subscription's current status snapshot within the
	// owning transaction bound by SubscriptionStatusTx; callbacks may repeat, so
	// implementations must avoid provider I/O and external effects.
	GetCurrent(context.Context) (SubscriptionStatusSnapshot, error)
	// GetCapture returns the captured SubscriptionStatus identified by its string
	// capture ID within the transaction, without performing provider I/O or
	// external effects.
	GetCapture(context.Context, string) (SubscriptionStatus, error)
	// InsertCapture records the supplied SubscriptionStatus as a capture within the
	// owning transaction; callbacks may repeat and provider I/O or external effects
	// are prohibited.
	InsertCapture(context.Context, SubscriptionStatus) error
	// PutCurrent stores the supplied SubscriptionStatus as the current head with
	// the given int64 sequence within the owning transaction, repeating safely
	// without provider I/O or external effects.
	PutCurrent(context.Context, SubscriptionStatus, int64) error
}

// SubscriptionStatusRepository owns encryption, joined snapshot reads and
// atomic capture/head CAS under one subscription guard. It does not call a
// provider or decide active paid reporting, identity or financial eligibility.
type SubscriptionStatusRepository interface {
	// ReadSubscriptionStatus returns the joined snapshot for the subscription
	// identified by RevenueScope and string ID; the repository owns encryption and
	// joined reads but performs no provider calls or eligibility decisions.
	ReadSubscriptionStatus(context.Context, RevenueScope, string) (SubscriptionStatusSnapshot, error)
	// WithSubscriptionStatusTransaction runs the supplied callback with a
	// SubscriptionStatusTx under the subscription's single guard, providing atomic
	// capture/head compare-and-swap within that subscription's transaction.
	WithSubscriptionStatusTransaction(context.Context, RevenueScope, string, func(SubscriptionStatusTx) error) error
}

// SubscriptionStatusIdentity is the opaque storage scope; equal subscription
// IDs in different merchant accounts/modes are separate lifecycle evidence.
func SubscriptionStatusIdentity(scope RevenueScope, subscription string) string {
	return "subscription_status_" + subscriptionDigest([]any{scope.Provider, scope.AccountID, scope.LiveMode, subscription})
}

// subscriptionDigest returns the SHA-256 hex digest of value's canonical JSON
// encoding; values that cannot marshal yield the empty string.
func subscriptionDigest(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// cleanStatusID reports whether a status identifier is a valid revenue identity
// without carriage return, newline or NUL characters.
func cleanStatusID(value string) bool {
	return validRevenueIdentity(value) && !strings.ContainsAny(value, "\r\n\x00")
}

// validStatus reports whether status is one of the recognized provider
// lifecycle states; unknown strings are rejected rather than treated as
// inactive.
func validStatus(status string) bool {
	switch status {
	case "active", "trialing", "incomplete", "incomplete_expired", "past_due", "unpaid", "canceled", "paused":
		return true
	default:
		return false
	}
}

// preparationBody builds the canonical fingerprint body for a preparation,
// preserving the legacy fact-based shape and wrapping checkout-sourced
// preparations with their intent identity and fingerprint.
func preparationBody(p SubscriptionStatusPreparation) any {
	legacy := struct {
		Fact, Fingerprint, Actor, Provider, Account, Principal, Customer, Subscription string
		Live                                                                           bool
		ExpectedRevision                                                               int64
		ExpectedFingerprint                                                            string
		RequestedAt                                                                    string
	}{p.FactID, p.FactFingerprint, p.ActorID, p.Scope.Provider, p.Scope.AccountID, p.PrincipalID, p.ProviderCustomerID, p.SubscriptionID, p.Scope.LiveMode, p.ExpectedRevision, p.ExpectedFingerprint, p.RequestedAt.UTC().Format(time.RFC3339Nano)}
	if p.Source == SubscriptionStatusCheckoutSource {
		return struct {
			Source, Intent, Fingerprint string
			Preparation                 any
		}{p.Source, p.CheckoutIntentID, p.CheckoutFingerprint, legacy}
	}
	return legacy
}

// Validate checks canonical preparation identity and revision/clock bounds;
// it does not establish billing provenance or current caller permission.
func (p SubscriptionStatusPreparation) Validate() error {
	if !validRevenueScope(p.Scope) || p.ExpectedRevision < 0 || p.ExpectedRevision == math.MaxInt64 || p.RequestedAt.IsZero() {
		return ErrRevenueInvalid
	}
	sourceIDs := []string{p.FactID, p.FactFingerprint}
	switch p.Source {
	case "":
		if p.CheckoutIntentID != "" || p.CheckoutFingerprint != "" {
			return ErrRevenueInvalid
		}
	case SubscriptionStatusCheckoutSource:
		if p.FactID != "" || p.FactFingerprint != "" {
			return ErrRevenueInvalid
		}
		sourceIDs = []string{p.CheckoutIntentID, p.CheckoutFingerprint}
	default:
		return ErrRevenueInvalid
	}
	for _, value := range append(sourceIDs, p.ActorID, p.Scope.Provider, p.Scope.AccountID, p.PrincipalID, p.ProviderCustomerID, p.SubscriptionID) {
		if !cleanStatusID(value) {
			return ErrRevenueInvalid
		}
	}
	if (p.ExpectedRevision == 0 && p.ExpectedFingerprint != "") || (p.ExpectedRevision > 0 && !cleanStatusID(p.ExpectedFingerprint)) {
		return ErrRevenueInvalid
	}
	if p.CaptureID != "subscription_capture_"+subscriptionDigest(preparationBody(p)) {
		return ErrRevenueConflict
	}
	return nil
}

// statusFingerprint digests the canonical preparation body together with status
// and cancellation scheduling; identical observations therefore share a
// fingerprint.
func statusFingerprint(p SubscriptionStatusPreparation, status string, scheduled bool) string {
	return subscriptionDigest([]any{preparationBody(p), status, scheduled})
}

// Validate checks retained receipt consistency, accepted revision and owning
// chronology. The owning service separately verifies original payment or checkout provenance.
func (s SubscriptionStatus) Validate() error {
	if s.Preparation.Validate() != nil || !validStatus(s.Status) || s.Revision != s.Preparation.ExpectedRevision+1 || s.ObservedAt.IsZero() || s.ObservedAt.Before(s.Preparation.RequestedAt) || s.Fingerprint != statusFingerprint(s.Preparation, s.Status, s.CancellationScheduled) {
		return ErrRevenueConflict
	}
	return nil
}

// sameSubscriptionStatus reports whether two statuses are the exact same
// observation: equal fingerprint, capture, revision and ObservedAt.
func sameSubscriptionStatus(a, b SubscriptionStatus) bool {
	return a.Fingerprint == b.Fingerprint && a.Preparation.CaptureID == b.Preparation.CaptureID && a.Revision == b.Revision && a.ObservedAt.Equal(b.ObservedAt)
}

// statusRepository returns the repository's optional
// SubscriptionStatusRepository capability after verifying the revenue context
// and non-nil service, clock and repository; absence or nil ports yield
// ErrRevenueUnavailable.
func (s *RevenueService) statusRepository(ctx context.Context) (SubscriptionStatusRepository, error) {
	if err := revenueContext(ctx); err != nil {
		return nil, err
	}
	if s == nil || revenueNil(s.repo) || revenueNil(s.clock) {
		return nil, ErrRevenueUnavailable
	}
	repo, ok := s.repo.(SubscriptionStatusRepository)
	if !ok || revenueNil(repo) {
		return nil, ErrRevenueUnavailable
	}
	return repo, nil
}

// statusFact loads one payment fact and rejects anything not canonical, not a
// payment, with zero sequence/time, or with unclean
// principal/customer/subscription IDs as unassessable; it also enforces
// retained paid-checkout ownership.
func (s *RevenueService) statusFact(ctx context.Context, id string) (RevenueFact, error) {
	f, err := s.GetRevenueFact(ctx, id)
	if err != nil {
		return RevenueFact{}, err
	}
	canonical, err := canonicalRevenueFact(f)
	if err != nil || canonical.ID != f.ID || canonical.Fingerprint != f.Fingerprint || f.ID != id || f.Kind != RevenuePayment || f.Sequence < 1 || f.AcceptedAt.IsZero() || !cleanStatusID(f.PrincipalID) || !cleanStatusID(f.ProviderCustomerID) || !cleanStatusID(f.SubscriptionID) {
		return RevenueFact{}, ErrRevenueUnassessable
	}
	if err := s.validatePaidCheckoutOwner(ctx, f); err != nil {
		return RevenueFact{}, err
	}
	return f, nil
}

// matchesStatusFact reports whether a legacy fact-sourced preparation exactly
// matches the fact's ID, fingerprint, scope, principal, customer and
// subscription with RequestedAt not before acceptance.
func matchesStatusFact(p SubscriptionStatusPreparation, f RevenueFact) bool {
	return p.Source == "" && p.FactID == f.ID && p.FactFingerprint == f.Fingerprint && p.Scope == f.Scope && p.PrincipalID == f.PrincipalID && p.ProviderCustomerID == f.ProviderCustomerID && p.SubscriptionID == f.SubscriptionID && !p.RequestedAt.Before(f.AcceptedAt)
}

// ValidateSubscriptionStatusPreparation rechecks immutable billing provenance
// before provider I/O or recovery. Authorship is retained from preparation;
// the manager must independently check the current caller's permission.
func (s *RevenueService) ValidateSubscriptionStatusPreparation(ctx context.Context, p SubscriptionStatusPreparation) error {
	if _, err := s.statusRepository(ctx); err != nil {
		return err
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if err := s.validateStatusProvenance(ctx, p); err != nil {
		return err
	}
	return ctx.Err()
}

// verifyStatus revalidates a snapshot's head and receipt, requires them to be
// the same observation, and rechecks the preparation's immutable billing
// provenance.
func (s *RevenueService) verifyStatus(ctx context.Context, snapshot SubscriptionStatusSnapshot) error {
	v := snapshot.Current
	if v.Validate() != nil || snapshot.Receipt.Validate() != nil || !sameSubscriptionStatus(v, snapshot.Receipt) {
		return ErrRevenueConflict
	}
	return s.validateStatusProvenance(ctx, v.Preparation)
}

// PrepareSubscriptionStatus must precede provider lookup. Current status is
// optional only on conclusive first absence; a joined outage never starts a
// new head. The same revenue owner establishes immutable accepted payer proof.
func (s *RevenueService) PrepareSubscriptionStatus(ctx context.Context, actor, factID string) (SubscriptionStatusPreparation, error) {
	repo, err := s.statusRepository(ctx)
	if err != nil {
		return SubscriptionStatusPreparation{}, err
	}
	if !cleanStatusID(actor) {
		return SubscriptionStatusPreparation{}, ErrRevenueInvalid
	}
	f, err := s.statusFact(ctx, factID)
	if err != nil {
		return SubscriptionStatusPreparation{}, err
	}
	p := SubscriptionStatusPreparation{FactID: f.ID, FactFingerprint: f.Fingerprint, ActorID: actor, Scope: f.Scope, PrincipalID: f.PrincipalID, ProviderCustomerID: f.ProviderCustomerID, SubscriptionID: f.SubscriptionID}
	return s.prepareStatus(ctx, repo, p, f.AcceptedAt)
}

// CaptureVerifiedSubscriptionStatus is private authenticated-provider input.
// Exact original receipts recover before later heads/clock changes; changed
// input conflicts. No provider I/O or financial journal mutation occurs here.
func (s *RevenueService) CaptureVerifiedSubscriptionStatus(ctx context.Context, p SubscriptionStatusPreparation, e VerifiedSubscriptionStatusEvidence) (SubscriptionStatus, error) {
	repo, err := s.statusRepository(ctx)
	if err != nil {
		return SubscriptionStatus{}, err
	}
	if e.Validate(p) != nil {
		return SubscriptionStatus{}, ErrRevenueConflict
	}
	if err := s.validateStatusProvenance(ctx, p); err != nil {
		return SubscriptionStatus{}, err
	}
	fp := statusFingerprint(p, e.Status, e.CancellationScheduled)
	var out SubscriptionStatus
	err = repo.WithSubscriptionStatusTransaction(ctx, p.Scope, p.SubscriptionID, func(tx SubscriptionStatusTx) error {
		out = SubscriptionStatus{}
		if revenueNil(tx) {
			return ErrRevenueUnavailable
		}
		original, err := tx.GetCapture(ctx, p.CaptureID)
		if err == nil {
			if original.Validate() != nil || original.Fingerprint != fp {
				return ErrRevenueConflict
			}
			out = original
			return nil
		}
		if !singleRevenueCause(err, ErrRevenueNotFound) {
			return err
		}
		current, err := tx.GetCurrent(ctx)
		if err == nil {
			if current.Current.Validate() != nil || current.Receipt.Validate() != nil || !sameSubscriptionStatus(current.Current, current.Receipt) || current.Current.Revision != p.ExpectedRevision || current.Current.Fingerprint != p.ExpectedFingerprint || current.Current.Preparation.Scope != p.Scope || current.Current.Preparation.SubscriptionID != p.SubscriptionID || current.Current.Preparation.PrincipalID != p.PrincipalID || current.Current.Preparation.ProviderCustomerID != p.ProviderCustomerID {
				return ErrRevenueConflict
			}
		} else if !singleRevenueCause(err, ErrRevenueNotFound) {
			return err
		} else if p.ExpectedRevision != 0 {
			return ErrRevenueConflict
		}
		at := s.clock.Now().UTC()
		if at.IsZero() || at.Before(p.RequestedAt) || (err == nil && at.Before(current.Current.ObservedAt)) {
			return ErrRevenueInvalid
		}
		v := SubscriptionStatus{Preparation: p, Status: e.Status, CancellationScheduled: e.CancellationScheduled, ObservedAt: at, Revision: p.ExpectedRevision + 1, Fingerprint: fp}
		if v.Validate() != nil {
			return ErrRevenueInvalid
		}
		if err := tx.InsertCapture(ctx, v); err != nil {
			return err
		}
		if err := tx.PutCurrent(ctx, v, p.ExpectedRevision); err != nil {
			return err
		}
		out = v
		return ctx.Err()
	})
	if err != nil {
		return SubscriptionStatus{}, err
	}
	if err := ctx.Err(); err != nil {
		return SubscriptionStatus{}, err
	}
	if out.Validate() != nil || out.Fingerprint != fp {
		return SubscriptionStatus{}, ErrRevenueUnavailable
	}
	return out, nil
}

// GetSubscriptionStatusForFact uses one owning head/receipt snapshot, checks
// its immutable billing provenance, then samples freshness. maxAge is explicit
// host-approved coverage (1s..24h), measured conservatively from lookup start.
// Active status alone never proves paid revenue; no caller identity is exposed.
func (s *RevenueService) GetSubscriptionStatusForFact(ctx context.Context, factID string, maxAge time.Duration) (SubscriptionStatus, error) {
	repo, err := s.statusRepository(ctx)
	if err != nil {
		return SubscriptionStatus{}, err
	}
	if maxAge < time.Second || maxAge > 24*time.Hour {
		return SubscriptionStatus{}, ErrRevenueInvalid
	}
	f, err := s.statusFact(ctx, factID)
	if err != nil {
		return SubscriptionStatus{}, err
	}
	return s.readStatusForIdentity(ctx, repo, f.Scope, f.SubscriptionID, f.PrincipalID, f.ProviderCustomerID, maxAge)
}

// prepareStatus completes a preparation against current state: an existing
// verified head must keep the same owner and pins expected
// revision/fingerprint; RequestedAt is set from the clock and must not precede
// the source or head observation. CaptureID is derived from the canonical
// preparation body.
func (s *RevenueService) prepareStatus(ctx context.Context, repo SubscriptionStatusRepository, p SubscriptionStatusPreparation, sourceAt time.Time) (SubscriptionStatusPreparation, error) {
	current, err := repo.ReadSubscriptionStatus(ctx, p.Scope, p.SubscriptionID)
	if err == nil {
		if err := s.verifyStatus(ctx, current); err != nil {
			return SubscriptionStatusPreparation{}, err
		}
		if current.Current.Preparation.Scope != p.Scope || current.Current.Preparation.SubscriptionID != p.SubscriptionID || current.Current.Preparation.PrincipalID != p.PrincipalID || current.Current.Preparation.ProviderCustomerID != p.ProviderCustomerID {
			return SubscriptionStatusPreparation{}, ErrRevenueConflict
		}
		p.ExpectedRevision = current.Current.Revision
		p.ExpectedFingerprint = current.Current.Fingerprint
	} else if !singleRevenueCause(err, ErrRevenueNotFound) {
		return SubscriptionStatusPreparation{}, err
	}
	p.RequestedAt = s.clock.Now().UTC()
	if p.RequestedAt.IsZero() || p.RequestedAt.Before(sourceAt) || (err == nil && p.RequestedAt.Before(current.Current.ObservedAt)) {
		return SubscriptionStatusPreparation{}, ErrRevenueInvalid
	}
	p.CaptureID = "subscription_capture_" + subscriptionDigest(preparationBody(p))
	if err := p.Validate(); err != nil {
		return SubscriptionStatusPreparation{}, err
	}
	if err := ctx.Err(); err != nil {
		return SubscriptionStatusPreparation{}, err
	}
	return p, nil
}

// readStatusForIdentity reads one verified joined snapshot and requires its
// preparation to exactly match the requested scope, subscription, principal and
// customer. Freshness is measured from now back to RequestedAt within maxAge,
// returning ErrSubscriptionStatusStale beyond it; a rewound clock is
// unavailable.
func (s *RevenueService) readStatusForIdentity(ctx context.Context, repo SubscriptionStatusRepository, scope RevenueScope, subscription, principal, customer string, maxAge time.Duration) (SubscriptionStatus, error) {
	snapshot, err := repo.ReadSubscriptionStatus(ctx, scope, subscription)
	if err != nil {
		return SubscriptionStatus{}, err
	}
	if err := s.verifyStatus(ctx, snapshot); err != nil {
		return SubscriptionStatus{}, err
	}
	p := snapshot.Current.Preparation
	if p.Scope != scope || p.SubscriptionID != subscription || p.PrincipalID != principal || p.ProviderCustomerID != customer {
		return SubscriptionStatus{}, ErrRevenueConflict
	}
	at := s.clock.Now().UTC()
	if at.IsZero() || at.Before(snapshot.Current.ObservedAt) {
		return SubscriptionStatus{}, ErrRevenueUnavailable
	}
	if at.Sub(p.RequestedAt) > maxAge {
		return SubscriptionStatus{}, ErrSubscriptionStatusStale
	}
	if err := ctx.Err(); err != nil {
		return SubscriptionStatus{}, err
	}
	return snapshot.Current, nil
}
