package partnerearnings

import "time"

// Journal entry kinds. The set is closed; balances are derived solely from
// these append-only entries plus claim states.
const (
	// EntryAccrued is a pending commission credit awaiting maturity.
	EntryAccrued = "accrued"
	// EntryMatured is the explicit, idempotent movement of one accrued commission
	// from pending to the matured balance.
	EntryMatured = "matured"
	// EntryReversed is a commission reversal delta (a debit to matured).
	EntryReversed = "reversed"
	// EntryAllocated links a claim's reservation to a specific payment's matured
	// credit (oldest first, partial portions).
	EntryAllocated = "allocated"
	// EntryAllocationReleased releases an allocation on reject, cancel or refund.
	EntryAllocationReleased = "allocation-released"
	// EntryAllocationSettled consumes an allocation when its claim is paid.
	EntryAllocationSettled = "allocation-settled"
	// EntryPaid is the single payout debit that settles a claim's reservation.
	EntryPaid = "paid"
	// EntryPaymentObserved records a durable, non-settling manual payment
	// observation (partial/unknown/mismatched) that sends a claim to review
	// without a debit or reservation release.
	EntryPaymentObserved = "payment-observed"
	// EntryReturned restores a returned-transfer obligation: a credit that
	// reinstates settled debit, at most the original settled amount net returns.
	EntryReturned = "returned"
	// EntryDisputeHold freezes matured, unrefunded credit pending a dispute.
	EntryDisputeHold     = "dispute-hold"
	EntryDisputeReleased = "dispute-released"
	EntryDisputeDecision = "dispute-decision"
	// EntryDisputeWon releases a dispute hold, reinstating the held credit.
	EntryDisputeWon = "dispute-won"
	// EntryDisputeLost permanently reverses the payment's remaining eligible
	// matured credit.
	EntryDisputeLost = "dispute-lost"
)

// Claim states. Only RecordPayment transitions into paid; DecideClaim never may.
const (
	ClaimRequested   = "requested"
	ClaimProcessing  = "processing"
	ClaimNeedsReview = "needs_review"
	ClaimPaid        = "paid"
	ClaimRejected    = "rejected"
	ClaimCancelled   = "cancelled"
)

// Receipt use cases.
const (
	UseCaseClaim     = "claim"
	UseCasePayment   = "payment"
	UseCaseAmendment = "amendment"
	UseCaseReturn    = "return"
	UseCaseDispute   = "dispute"
)

// Payment observation states reported with a manual payout.
const (
	// PaymentStateFull is a complete transfer of the exact claim amount and
	// currency; the only state that may settle a claim.
	PaymentStateFull = "full"
	// PaymentStatePartial is a confirmed transfer short of the full amount.
	PaymentStatePartial = "partial"
	// PaymentStateUnknown is an attempt whose outcome cannot yet be confirmed.
	PaymentStateUnknown = "unknown"
	// PaymentStateMismatched is a transfer whose amount or currency differs.
	PaymentStateMismatched = "mismatched"
)

// Dispute actions, applied against a payment's matured, unrefunded credit.
const (
	DisputeHold = "hold"
	DisputeWon  = "won"
	DisputeLost = "lost"
)

// Rate bounds, in basis points (100 bp = 1%).
const (
	MinRateBasisPoints = 0
	MaxRateBasisPoints = 10000
	BasisPointsPerUnit = 10000
)

// Bounds for user-supplied payment and idempotency metadata.
const (
	maxIDLength          = 256
	maxMethodLength      = 64
	maxReferenceLength   = 256
	maxReasonLength      = 1024
	maxIdempotencyKeyLen = 256
	maxTermsLength       = 256
	maxPolicyLength      = 256
	maxSourceKindLength  = 256
	// maxHoldDuration caps the maturity hold. The financial rule is bounded so a
	// single event cannot freeze credit beyond a sane horizon.
	maxHoldDuration = 28 * 24 * time.Hour
)

// Config captures host wiring: the single program and approved currency. The
// ledger never converts currencies; every source event must match Currency.
type Config struct {
	// ProgramID is the owning program (single program per process).
	ProgramID string
	// Currency is the single approved program/payout currency (three letters).
	Currency string
}

func (c Config) validate() error {
	if _, ok := cleanPlain(c.ProgramID, maxIDLength); !ok {
		return errWrap(ErrInvalid, "program id required")
	}
	if c.Currency == "" || len(c.Currency) != 3 || !isUpperAlpha(c.Currency) {
		return errWrap(ErrInvalid, "one approved three-letter currency required")
	}
	return nil
}

// Validate exposes configuration validation so hosts reject invalid wiring
// before opening any store.
func (c Config) Validate() error { return c.validate() }

// Entry is one append-only journal line. AmountMinor is signed: credits are
// positive (accrued, matured, allocated), debits negative (reversed, paid,
// allocation releases/settlements). The accrual provenance fields are populated
// only on accrued entries; the refund fields only on reversed entries.
type Entry struct {
	ID            string    `json:"id" bson:"id"`
	ProgramID     string    `json:"program_id" bson:"program_id"`
	PartnerID     string    `json:"partner_id" bson:"partner_id"`
	Sequence      int64     `json:"sequence" bson:"sequence"`
	Kind          string    `json:"kind" bson:"kind"`
	SourceEventID string    `json:"source_event_id" bson:"source_event_id"` // payment/refund/claim id
	DisputeID     string    `json:"dispute_id,omitempty" bson:"dispute_id,omitempty"`
	SourceRef     string    `json:"source_ref,omitempty" bson:"source_ref,omitempty"` // payment id for reversals/allocations
	AmountMinor   int64     `json:"amount_minor" bson:"amount_minor"`
	Currency      string    `json:"currency" bson:"currency"`
	Note          string    `json:"note,omitempty" bson:"note,omitempty"`
	ActorID       string    `json:"actor_id,omitempty" bson:"actor_id,omitempty"`
	Fingerprint   string    `json:"fingerprint,omitempty" bson:"fingerprint,omitempty"`
	CreatedAt     time.Time `json:"created_at" bson:"created_at"`
	OccurredAt    time.Time `json:"occurred_at" bson:"occurred_at"` // business event time (paidAt / refundedAt)

	// Accrual provenance (accrued entries only).
	PaymentMinor    int64  `json:"payment_minor,omitempty" bson:"payment_minor,omitempty"`       // original eligible paid amount
	CommissionMinor int64  `json:"commission_minor,omitempty" bson:"commission_minor,omitempty"` // original commission
	ReferralID      string `json:"referral_id,omitempty" bson:"referral_id,omitempty"`
	// PlanID is the immutable billing-owned plan of this original allocation.
	// Empty means unavailable provenance, never today's catalogue default.
	PlanID          string        `json:"plan_id,omitempty" bson:"plan_id,omitempty"`
	TermsVersion    string        `json:"terms_version,omitempty" bson:"terms_version,omitempty"`
	PolicyID        string        `json:"policy_id,omitempty" bson:"policy_id,omitempty"`
	RateBasisPoints int           `json:"rate_basis_points,omitempty" bson:"rate_basis_points,omitempty"`
	HoldDuration    time.Duration `json:"hold_duration,omitempty" bson:"hold_duration,omitempty"`
	AvailableAt     *time.Time    `json:"available_at,omitempty" bson:"available_at,omitempty"` // paidAt + hold

	// Refund provenance (reversed entries only).
	RefundedMinor           int64 `json:"refunded_minor,omitempty" bson:"refunded_minor,omitempty"`                       // this event's refund delta in payment units
	CumulativeRefundedMinor int64 `json:"cumulative_refunded_minor,omitempty" bson:"cumulative_refunded_minor,omitempty"` // cumulative refunded paid amount
}

// Claim is one payout request and its lifecycle. AmountMinor and Currency are
// fixed at request time; corrections to payment details are append-only.
type Claim struct {
	ID                  string               `json:"id" bson:"id"`
	ProgramID           string               `json:"program_id" bson:"program_id"`
	PartnerID           string               `json:"partner_id" bson:"partner_id"`
	AmountMinor         int64                `json:"amount_minor" bson:"amount_minor"`
	Currency            string               `json:"currency" bson:"currency"`
	State               string               `json:"state" bson:"state"`
	DestinationID       string               `json:"destination_id" bson:"destination_id"`
	DestinationSnapshot map[string]string    `json:"destination_snapshot" bson:"destination_snapshot"`
	RequestedAt         time.Time            `json:"requested_at" bson:"requested_at"`
	UpdatedAt           time.Time            `json:"updated_at" bson:"updated_at"`
	UpdatedBy           string               `json:"updated_by" bson:"updated_by"`
	Reason              string               `json:"reason,omitempty" bson:"reason,omitempty"`
	ReviewReason        string               `json:"review_reason,omitempty" bson:"review_reason,omitempty"`
	RequestedBy         string               `json:"requested_by,omitempty" bson:"requested_by,omitempty"`
	RequestedReason     string               `json:"requested_reason,omitempty" bson:"requested_reason,omitempty"`
	ProcessingActor     string               `json:"processing_actor,omitempty" bson:"processing_actor,omitempty"`
	Payment             *ManualPayment       `json:"payment,omitempty" bson:"payment,omitempty"`
	PaymentAmendments   []PaymentAmendment   `json:"payment_amendments,omitempty" bson:"payment_amendments,omitempty"`
	PaymentObservations []PaymentObservation `json:"payment_observations,omitempty" bson:"payment_observations,omitempty"`
	ReturnedAdjustments []ReturnedAdjustment `json:"returned_adjustments,omitempty" bson:"returned_adjustments,omitempty"`
	Revision            int64                `json:"revision" bson:"revision"`
}

// ManualPayment is the bounded, plain payment record written only by RecordPayment.
type ManualPayment struct {
	Method      string    `json:"method" bson:"method"`
	Reference   string    `json:"reference" bson:"reference"`
	PaidAt      time.Time `json:"paid_at" bson:"paid_at"`
	RecordedBy  string    `json:"recorded_by" bson:"recorded_by"`
	RecordedAt  time.Time `json:"recorded_at" bson:"recorded_at"`
	AmountMinor int64     `json:"amount_minor" bson:"amount_minor"`
	Currency    string    `json:"currency" bson:"currency"`
	State       string    `json:"state" bson:"state"`
}

// PaymentAmendment corrects a recorded payment's method, reference or date with a
// required reason. Amendments never create a second debit. IdempotencyKey is the
// replay identity so a retry never appends a duplicate audit record.
type PaymentAmendment struct {
	Method         string    `json:"method,omitempty" bson:"method,omitempty"`
	Reference      string    `json:"reference,omitempty" bson:"reference,omitempty"`
	PaidAt         time.Time `json:"paid_at,omitempty" bson:"paid_at,omitempty"`
	Reason         string    `json:"reason" bson:"reason"`
	By             string    `json:"by" bson:"by"`
	At             time.Time `json:"at" bson:"at"`
	IdempotencyKey string    `json:"idempotency_key,omitempty" bson:"idempotency_key,omitempty"`
}

// PaymentObservation is durable evidence of a non-settling manual payment
// attempt. The claim's reservation is preserved; the observation never settles.
type PaymentObservation struct {
	AmountMinor int64     `json:"amount_minor" bson:"amount_minor"`
	Currency    string    `json:"currency" bson:"currency"`
	State       string    `json:"state" bson:"state"`
	Method      string    `json:"method,omitempty" bson:"method,omitempty"`
	Reference   string    `json:"reference,omitempty" bson:"reference,omitempty"`
	PaidAt      time.Time `json:"paid_at,omitempty" bson:"paid_at,omitempty"`
	RecordedBy  string    `json:"recorded_by" bson:"recorded_by"`
	RecordedAt  time.Time `json:"recorded_at" bson:"recorded_at"`
}

// ReturnedAdjustment records one returned transfer against a paid claim. The
// returned obligation is restored at most up to the original settled debit net
// of prior returns; the paid evidence and history are never rewritten.
type ReturnedAdjustment struct {
	OperationID    string    `json:"operation_id" bson:"operation_id"` // immutable typed return journal linkage
	IdempotencyKey string    `json:"idempotency_key" bson:"idempotency_key"`
	AmountMinor    int64     `json:"amount_minor" bson:"amount_minor"`
	Currency       string    `json:"currency" bson:"currency"`
	Reference      string    `json:"reference,omitempty" bson:"reference,omitempty"`
	ReturnedAt     time.Time `json:"returned_at" bson:"returned_at"`
	Reason         string    `json:"reason" bson:"reason"`
	By             string    `json:"by" bson:"by"`
	At             time.Time `json:"at" bson:"at"`
}

// Balances is a consistent snapshot derived from the journal and claim states.
type Balances struct {
	PendingMinor            int64 `json:"pending_minor" bson:"pending_minor"`                           // accrued, not yet matured
	MatchedMinor            int64 `json:"matched_minor" bson:"matched_minor"`                           // M: net matured balance (may be negative)
	ReservedMinor           int64 `json:"reserved_minor" bson:"reserved_minor"`                         // R: active reservations
	ReviewHoldMinor         int64 `json:"review_hold_minor" bson:"review_hold_minor"`                   // H: matured review holds
	PendingDisputeHoldMinor int64 `json:"pending_dispute_hold_minor" bson:"pending_dispute_hold_minor"` // frozen part of PendingMinor
	DisputeHoldMinor        int64 `json:"dispute_hold_minor" bson:"dispute_hold_minor"`                 // D: matured dispute freezes
	AvailableMinor          int64 `json:"available_minor" bson:"available_minor"`                       // max(0, M - R - H - D)
	DebtMinor               int64 `json:"debt_minor" bson:"debt_minor"`                                 // max(0, -M)
	PaidOutMinor            int64 `json:"paid_out_minor" bson:"paid_out_minor"`                         // cumulative payouts
}

// AccrualRequest books one commission. PaymentID is the idempotency anchor;
// Currency must match the configured program currency.
type AccrualRequest struct {
	PartnerID       string
	PaymentID       string
	PaymentMinor    int64
	RateBasisPoints int
	HoldDuration    time.Duration
	Currency        string
	OccurredAt      time.Time
	ReferralID      string
	PlanID          string // optional immutable owning billing plan; not a client-supplied grouping
	TermsVersion    string
	PolicyID        string
	SourceKind      string
}

// ReversalRequest books one refund against a payment. CumulativeRefundedMinor is
// the total eligible refund in payment units after this refund, so split refunds
// produce the same cumulative reversal as one refund.
type ReversalRequest struct {
	PartnerID               string
	RefundID                string
	PaymentID               string
	CumulativeRefundedMinor int64
	Currency                string
	OccurredAt              time.Time
}

// ClaimRequest reserves matured funds for a payout. IdempotencyKey scopes the
// request to the actor/use-case/partner/currency identity.
type ClaimRequest struct {
	ActorID             string `json:"-"`
	PartnerID           string
	AmountMinor         int64
	Currency            string
	DestinationID       string
	DestinationSnapshot map[string]string
	IdempotencyKey      string
	Reason              string
}

// ClaimDecision moves a claim through the operator queue. NewState may never be
// paid; that is RecordPayment's sole privilege. ConfirmedUnsent must be true to
// reject or cancel a claim whose transfer could already be in flight.
type ClaimDecision struct {
	ClaimID          string
	NewState         string
	Reason           string
	ActorID          string `json:"-"`
	ConfirmedUnsent  bool
	ExpectedRevision int64 `json:"expected_revision"`
}

// RecordPaymentRequest settles a claim with a privileged manual payout. ActorID
// and the recorded time are server-bound, never read from an untrusted payload.
// AmountMinor, Currency and State are the observed transfer evidence; a claim is
// settled only on a proven full, exact-amount, exact-currency transfer.
type RecordPaymentRequest struct {
	ClaimID          string
	Method           string
	Reference        string
	PaidAt           time.Time
	AmountMinor      int64
	Currency         string
	State            string
	ActorID          string `json:"-"`
	IdempotencyKey   string
	ExpectedRevision int64 `json:"expected_revision"`
}

// AmendPaymentRequest corrects recorded payment details without a new debit.
// IdempotencyKey is the replay identity so a retry never appends a duplicate
// amendment; ExpectedRevision is a precondition on the claim revision.
type AmendPaymentRequest struct {
	ClaimID          string
	Method           string
	Reference        string
	PaidAt           time.Time
	Reason           string
	ActorID          string `json:"-"`
	IdempotencyKey   string
	ExpectedRevision int64 `json:"expected_revision"`
}

// DisputeRequest applies an authoritative dispute action (hold, won, lost)
// against one payment's matured, unrefunded credit. OperationID is the durable,
// unique dispute operation identity: an exact replay returns the original
// entries, a changed payload under the same OperationID conflicts.
type DisputeRequest struct {
	DisputeID   string
	PartnerID   string
	PaymentID   string
	Action      string
	OperationID string
	Reason      string
	ActorID     string `json:"-"`
	Currency    string
	OccurredAt  time.Time
}

// DisputeResult carries the journal entries produced (or replayed) by a dispute
// action.
type DisputeResult struct {
	Entries []Entry `json:"entries" bson:"entries"`
}

// ReturnRequest records one returned transfer against a paid claim, restoring
// the payout obligation at most once per settled debit net of prior returns.
type ReturnRequest struct {
	ClaimID          string
	AmountMinor      int64
	Currency         string
	Reference        string
	ReturnedAt       time.Time
	Reason           string
	ActorID          string `json:"-"`
	IdempotencyKey   string
	ExpectedRevision int64 `json:"expected_revision"`
}

// ReturnResult carries the updated claim and the journal entry produced (or
// replayed) by a return adjustment.
type ReturnResult struct {
	Claim   Claim   `json:"claim" bson:"claim"`
	Entries []Entry `json:"entries" bson:"entries"`
}
