package billing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	ErrRevenueInvalid      = errors.New("billing/revenue-invalid")
	ErrRevenueNotFound     = errors.New("billing/revenue-not-found")
	ErrRevenueConflict     = errors.New("billing/revenue-conflict")
	ErrRevenueUnavailable  = errors.New("billing/revenue-unavailable")
	ErrRevenueUncertain    = errors.New("billing/revenue-uncertain")
	ErrRevenueUnassessable = errors.New("billing/revenue-unassessable")
)

const (
	RevenuePayment     = "payment"
	RevenueRefund      = "refund"
	RevenueDisputeHold = "dispute_hold"
	RevenueDisputeLost = "dispute_lost"
	RevenueDisputeWon  = "dispute_won"
)

var revenueCurrency = regexp.MustCompile(`^[A-Z]{3}$`)

// RevenueScope is the verified provider identity; equal invoice IDs from
// different accounts or test/live modes are different economic sources.
type RevenueScope struct {
	Provider  string `json:"provider" bson:"provider"`
	AccountID string `json:"account_id" bson:"account_id"`
	LiveMode  bool   `json:"live_mode" bson:"live_mode"`
}

// RevenueFact is one verified economic allocation, independent of transport
// envelope IDs. PaidMinor is net paid line revenue after discounts and credits,
// excluding tax. It is not the invoice total, catalogue price or active access.
// Only billing owning-service/provider contracts may establish these fields.
type RevenueFact struct {
	ID           string       `json:"id" bson:"_id"`
	Sequence     int64        `json:"sequence" bson:"sequence"`
	Scope        RevenueScope `json:"scope" bson:"scope"`
	Kind         string       `json:"kind" bson:"kind"`
	PaymentID    string       `json:"payment_id" bson:"payment_id"`
	InvoiceID    string       `json:"invoice_id" bson:"invoice_id"`
	AllocationID string       `json:"allocation_id" bson:"allocation_id"`
	AdjustmentID string       `json:"adjustment_id,omitempty" bson:"adjustment_id,omitempty"`
	// PrincipalID is the owning paying account, never an email or organization seat.
	PrincipalID             string    `json:"principal_id" bson:"principal_id"`
	SubscriptionID          string    `json:"subscription_id" bson:"subscription_id"`
	PlanID                  string    `json:"plan_id" bson:"plan_id"`
	CostID                  string    `json:"cost_id" bson:"cost_id"`
	ProviderPriceID         string    `json:"provider_price_id,omitempty" bson:"provider_price_id,omitempty"`
	ProviderCustomerID      string    `json:"provider_customer_id,omitempty" bson:"provider_customer_id,omitempty"`
	Currency                string    `json:"currency" bson:"currency"`
	CurrencyExponent        int       `json:"currency_exponent" bson:"currency_exponent"`
	PaidMinor               int64     `json:"paid_minor" bson:"paid_minor"`
	CumulativeRefundedMinor int64     `json:"cumulative_refunded_minor,omitempty" bson:"cumulative_refunded_minor,omitempty"`
	EffectiveAt             time.Time `json:"effective_at" bson:"effective_at"`
	AcceptedAt              time.Time `json:"accepted_at" bson:"accepted_at"`
	Fingerprint             string    `json:"-" bson:"fingerprint"`
}

// PaymentFactID is stable across payment/refund/dispute envelopes and names
// the original paid allocation for journal provenance and reconciliation.
func (f RevenueFact) PaymentFactID() string {
	return revenueID(f.Scope, RevenuePayment, f.PaymentID, f.InvoiceID, f.AllocationID, "")
}

// revenueID derives the deterministic "revenue_"-prefixed fact identity from
// scope, kind, payment, invoice, allocation and adjustment components.
func revenueID(scope RevenueScope, kind, payment, invoice, allocation, adjustment string) string {
	body, _ := json.Marshal([]any{scope.Provider, scope.AccountID, scope.LiveMode, kind, payment, invoice, allocation, adjustment})
	sum := sha256.Sum256(body)
	return "revenue_" + hex.EncodeToString(sum[:])
}

// VerifiedRevenueRequest is in-process billing input, never HTTP-decoded.
// The caller must first authenticate the provider and resolve the server-owned
// payer/plan/cost association. Missing or ambiguous allocation evidence becomes
// a durable quarantine; no fallback invents a payment or eligibility amount.
type VerifiedRevenueRequest struct {
	Scope             RevenueScope  `json:"-"`
	SourceFingerprint string        `json:"-"`
	EnvelopeID        string        `json:"-"`
	Facts             []RevenueFact `json:"-"`
	QuarantineReason  string        `json:"-"`
}

// RevenueObservation records acceptance of one delivery, including duplicates
// and unresolved allocation evidence. It contains no raw payload or email.
type RevenueObservation struct {
	ID                string `bson:"_id"`
	SourceFingerprint string `json:"-" bson:"source_fingerprint,omitempty"`
	// RecoveryFingerprint binds an authenticated resolution to a stable provider
	// snapshot while leaving the original quarantine fingerprint unchanged.
	RecoveryFingerprint string       `json:"-" bson:"recovery_fingerprint,omitempty"`
	Scope               RevenueScope `bson:"scope"`
	EnvelopeID          string       `bson:"envelope_id"`
	FactIDs             []string     `bson:"fact_ids"`
	Fingerprint         string       `bson:"fingerprint"`
	QuarantineReason    string       `bson:"quarantine_reason,omitempty"`
	ResolutionOf        string       `bson:"resolution_of,omitempty"`
	ResolutionReason    string       `bson:"resolution_reason,omitempty"`
	ResolutionBy        string       `json:"-" bson:"resolution_by,omitempty"`
	AcceptedAt          time.Time    `bson:"accepted_at"`
}

// RevenueAcknowledgement advances one consumer only after its durable owning
// financial acceptance (or explicit durable no-entitlement decision). There is
// no global cursor that can skip an earlier unresolved fact.
type RevenueAcknowledgement struct {
	ConsumerID   string    `bson:"consumer_id"`
	FactID       string    `bson:"fact_id"`
	AcceptanceID string    `bson:"acceptance_id"`
	Outcome      string    `bson:"outcome"`
	ActorID      string    `json:"-" bson:"actor_id"`
	AcceptedAt   time.Time `bson:"accepted_at"`
}

// RevenueTx is a transaction-bound repository. AppendFact atomically allocates
// and returns a monotonic durable sequence; callbacks may repeat on transient
// conflicts, so no external effects are allowed within them.
type RevenueTx interface {
	// GetObservation reads a revenue observation by identity within the bound
	// RevenueTx transaction.
	GetObservation(context.Context, string) (RevenueObservation, error)
	// InsertObservation persists a revenue observation within the bound RevenueTx
	// transaction, committing atomically with other facts and observations.
	InsertObservation(context.Context, RevenueObservation) error
	// GetFact reads a revenue fact by identity within the bound RevenueTx
	// transaction.
	GetFact(context.Context, string) (RevenueFact, error)
	// AppendFact appends a revenue fact within the bound transaction, atomically
	// allocating and returning the fact with a monotonic durable sequence.
	AppendFact(context.Context, RevenueFact) (RevenueFact, error)
}

// RevenueRepository owns driver I/O, unique source keys, complete indexed feed
// reads and per-consumer acknowledgement CAS. A failed transaction has no
// partial observations/facts. Ambiguous commit is ErrRevenueUncertain.
type RevenueRepository interface {
	// WithRevenueTransaction runs the callback with a RevenueTx; a failed
	// transaction leaves no partial observations or facts, and an ambiguous commit
	// returns ErrRevenueUncertain. Callbacks may repeat on transient conflicts, so
	// no external effects are allowed within them.
	WithRevenueTransaction(context.Context, func(RevenueTx) error) error
	// GetRevenueFact returns the persisted verified revenue fact for the given
	// identity from the owning repository; the service implementation validates the
	// identity and rejects invalid revenue contexts before forwarding.
	GetRevenueFact(context.Context, string) (RevenueFact, error)
	// PendingRevenueFacts returns up to the requested limit of unacknowledged facts
	// for a consumer; the service implementation validates the consumer identity
	// and a 1–200 limit. Poison records may be quarantined independently of later
	// facts.
	PendingRevenueFacts(context.Context, string, int) ([]RevenueFact, error)
	// AcknowledgeRevenueFact records a consumer's durable acknowledgement decision
	// for a fact; the service implementation validates identities and outcome,
	// requires the fact to exist, and timestamps acceptance. Unavailable
	// dependencies must remain pending.
	AcknowledgeRevenueFact(context.Context, RevenueAcknowledgement) error
}

// RevenueClock supplies the service's notion of now, allowing tests to control
// acceptance timing.
type RevenueClock interface {
	// Now returns the service's current notion of time for acceptance timing,
	// allowing tests to control it.
	Now() time.Time
}

// RevenueService owns verified fact validation, economic deduplication and
// durable feed acceptance. The existing billing manager remains the provider
// verification/identity boundary; the adapter never calculates commission.
type RevenueService struct {
	repo  RevenueRepository
	clock RevenueClock
}

// NewRevenueService rejects a nil repository or clock with
// ErrRevenueUnavailable and otherwise returns the wired service.
func NewRevenueService(repo RevenueRepository, clock RevenueClock) (*RevenueService, error) {
	if revenueNil(repo) || revenueNil(clock) {
		return nil, ErrRevenueUnavailable
	}
	return &RevenueService{repo, clock}, nil
}

// revenueNil reports whether v is nil or a nil channel, function, interface,
// map, pointer or slice; other values are non-nil.
func revenueNil(v any) bool {
	if v == nil {
		return true
	}
	value := reflect.ValueOf(v)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	}
	return false
}

// revenueContext returns ErrRevenueInvalid for a nil context, otherwise its
// cancellation or deadline error.
func revenueContext(ctx context.Context) error {
	if ctx == nil {
		return ErrRevenueInvalid
	}
	return ctx.Err()
}

// validRevenueIdentity accepts non-empty IDs of at most 256 characters with no
// surrounding whitespace.
func validRevenueIdentity(id string) bool {
	return id != "" && len(id) <= 256 && strings.TrimSpace(id) == id
}

// validRevenueScope requires provider and account identifiers that each satisfy
// validRevenueIdentity.
func validRevenueScope(s RevenueScope) bool {
	return validRevenueIdentity(s.Provider) && validRevenueIdentity(s.AccountID)
}

// canonicalRevenueFact validates field shapes and per-kind rules (payments
// carry no adjustment or refunds; adjustments require one), then rewrites the
// deterministic ID, normalizes effective time to UTC, clears sequence and
// acceptance, and fingerprints the canonical JSON encoding. Violations return
// ErrRevenueInvalid.
func canonicalRevenueFact(f RevenueFact) (RevenueFact, error) {
	if !validRevenueScope(f.Scope) || !validRevenueIdentity(f.PaymentID) || !validRevenueIdentity(f.InvoiceID) || !validRevenueIdentity(f.AllocationID) || !validRevenueIdentity(f.PrincipalID) || !validRevenueIdentity(f.SubscriptionID) || !validRevenueIdentity(f.PlanID) || !validRevenueIdentity(f.CostID) || f.EffectiveAt.IsZero() || !revenueCurrency.MatchString(f.Currency) || f.CurrencyExponent < 0 || f.CurrencyExponent > 3 || f.PaidMinor < 0 || f.CumulativeRefundedMinor < 0 || f.CumulativeRefundedMinor > f.PaidMinor {
		return RevenueFact{}, ErrRevenueInvalid
	}
	if (f.ProviderPriceID != "" && !validRevenueIdentity(f.ProviderPriceID)) || (f.ProviderCustomerID != "" && !validRevenueIdentity(f.ProviderCustomerID)) {
		return RevenueFact{}, ErrRevenueInvalid
	}
	switch f.Kind {
	case RevenuePayment:
		if f.AdjustmentID != "" || f.CumulativeRefundedMinor != 0 {
			return RevenueFact{}, ErrRevenueInvalid
		}
	case RevenueRefund, RevenueDisputeHold, RevenueDisputeLost, RevenueDisputeWon:
		if !validRevenueIdentity(f.AdjustmentID) {
			return RevenueFact{}, ErrRevenueInvalid
		}
	default:
		return RevenueFact{}, ErrRevenueInvalid
	}
	f.ID = revenueID(f.Scope, f.Kind, f.PaymentID, f.InvoiceID, f.AllocationID, f.AdjustmentID)
	f.Sequence = 0
	f.AcceptedAt = time.Time{}
	f.Fingerprint = ""
	f.EffectiveAt = f.EffectiveAt.UTC()
	encoded, err := json.Marshal(f)
	if err != nil {
		return RevenueFact{}, ErrRevenueInvalid
	}
	sum := sha256.Sum256(encoded)
	f.Fingerprint = hex.EncodeToString(sum[:])
	return f, nil
}

// AcceptVerified commits the entire verified envelope before acknowledging its
// reception. Different envelopes for the same economics add observations, not
// commissions. Changed economics under an existing source identity conflict.
func (s *RevenueService) AcceptVerified(ctx context.Context, req VerifiedRevenueRequest) (RevenueObservation, error) {
	if err := revenueContext(ctx); err != nil {
		return RevenueObservation{}, err
	}
	if !validRevenueScope(req.Scope) || !validRevenueIdentity(req.EnvelopeID) || (req.SourceFingerprint != "" && !validRevenueIdentity(req.SourceFingerprint)) || len(req.Facts) > 200 || len(req.QuarantineReason) > 128 || (len(req.Facts) == 0 && req.QuarantineReason == "") || (len(req.Facts) > 0 && req.QuarantineReason != "") {
		return RevenueObservation{}, ErrRevenueInvalid
	}
	facts := make([]RevenueFact, len(req.Facts))
	seen := map[string]bool{}
	for i, f := range req.Facts {
		if f.Scope != req.Scope {
			return RevenueObservation{}, ErrRevenueInvalid
		}
		canonical, err := canonicalRevenueFact(f)
		if err != nil {
			return RevenueObservation{}, err
		}
		if seen[canonical.ID] {
			return RevenueObservation{}, ErrRevenueInvalid
		}
		seen[canonical.ID] = true
		facts[i] = canonical
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
	encoded, _ := json.Marshal(struct {
		Facts             []RevenueFact
		Reason            string
		SourceFingerprint string
	}{facts, req.QuarantineReason, req.SourceFingerprint})
	sum := sha256.Sum256(encoded)
	observation := RevenueObservation{SourceFingerprint: req.SourceFingerprint, ID: revenueID(req.Scope, "delivery", req.EnvelopeID, "", "", ""), Scope: req.Scope, EnvelopeID: req.EnvelopeID, Fingerprint: hex.EncodeToString(sum[:]), QuarantineReason: req.QuarantineReason, AcceptedAt: s.clock.Now().UTC(), FactIDs: []string{}}
	for _, f := range facts {
		observation.FactIDs = append(observation.FactIDs, f.ID)
	}
	var result RevenueObservation
	err := s.repo.WithRevenueTransaction(ctx, func(tx RevenueTx) error {
		result = RevenueObservation{}
		old, err := tx.GetObservation(ctx, observation.ID)
		if err == nil {
			if old.Fingerprint != observation.Fingerprint {
				return ErrRevenueConflict
			}
			result = old
			return nil
		}
		if !singleRevenueCause(err, ErrRevenueNotFound) {
			return err
		}
		for _, f := range facts {
			old, err := tx.GetFact(ctx, f.ID)
			if err == nil {
				if old.Fingerprint != f.Fingerprint {
					return ErrRevenueConflict
				}
				continue
			}
			if !singleRevenueCause(err, ErrRevenueNotFound) {
				return err
			}
			f.AcceptedAt = observation.AcceptedAt
			if _, err := tx.AppendFact(ctx, f); err != nil {
				return err
			}
		}
		if err := tx.InsertObservation(ctx, observation); err != nil {
			return err
		}
		result = observation
		return nil
	})
	if err != nil {
		return RevenueObservation{}, err
	}
	return result, nil
}

// GetRevenueFact returns only the owning persisted verified record.
func (s *RevenueService) GetRevenueFact(ctx context.Context, id string) (RevenueFact, error) {
	if err := revenueContext(ctx); err != nil {
		return RevenueFact{}, err
	}
	if !validRevenueIdentity(id) {
		return RevenueFact{}, ErrRevenueInvalid
	}
	return s.repo.GetRevenueFact(ctx, id)
}

// PendingRevenueFacts returns bounded unacknowledged records. Poison records
// may be acknowledged as durable quarantine independently of later facts.
func (s *RevenueService) PendingRevenueFacts(ctx context.Context, consumer string, limit int) ([]RevenueFact, error) {
	if err := revenueContext(ctx); err != nil {
		return nil, err
	}
	if !validRevenueIdentity(consumer) || limit < 1 || limit > 200 {
		return nil, ErrRevenueInvalid
	}
	return s.repo.PendingRevenueFacts(ctx, consumer, limit)
}

// AcknowledgeRevenueFact requires the worker's durable decision reference.
// An unavailable dependency must remain pending; it is never an absence result.
func (s *RevenueService) AcknowledgeRevenueFact(ctx context.Context, ack RevenueAcknowledgement) error {
	if err := revenueContext(ctx); err != nil {
		return err
	}
	if !validRevenueIdentity(ack.ConsumerID) || !validRevenueIdentity(ack.FactID) || !validRevenueIdentity(ack.AcceptanceID) || !validRevenueIdentity(ack.ActorID) {
		return ErrRevenueInvalid
	}
	switch ack.Outcome {
	case "accepted", "no_entitlement", "quarantined":
	default:
		return fmt.Errorf("%w: acknowledgement outcome", ErrRevenueInvalid)
	}
	if _, err := s.repo.GetRevenueFact(ctx, ack.FactID); err != nil {
		return err
	}
	ack.AcceptedAt = s.clock.Now().UTC()
	return s.repo.AcknowledgeRevenueFact(ctx, ack)
}
