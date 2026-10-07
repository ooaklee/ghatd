package partnerearnings

import (
	"context"
	"errors"
	"time"
)

// Typed outcomes distinguish the financial failure classes the service surfaces.
// Hosts map these to transport responses; consumers select on them directly.
var (
	// ErrNotFound is absence: an unknown ledger entry, claim or receipt.
	ErrNotFound = errors.New("partnerearnings/not-found")
	// ErrAlreadyExists is a duplicate identity where replay is not the answer.
	ErrAlreadyExists = errors.New("partnerearnings/already-exists")
	// ErrInvalid is a malformed or out-of-bounds request or configuration.
	ErrInvalid = errors.New("partnerearnings/invalid")
	// ErrReportTooLarge is an owning history capacity limit, not an invalid
	// customer request, missing attribution or proof of zero entitlement.
	ErrReportTooLarge = errors.New("partnerearnings/report-too-large")
	// ErrDenied is a caller that may not perform the operation or transition.
	ErrDenied = errors.New("partnerearnings/denied")
	// ErrUnavailable is missing or un-wired service dependencies (fails closed).
	ErrUnavailable = errors.New("partnerearnings/unavailable")
	// ErrUncertain marks an outcome whose commit could not be confirmed. Retry
	// the same request (same identity) rather than inventing a new one.
	ErrUncertain = errors.New("partnerearnings/uncertain")
	// ErrConflict is a replay of an existing identity with a different payload,
	// or a state transition that contradicts the current claim state.
	ErrConflict = errors.New("partnerearnings/conflict")
	// ErrStaleWrite is an optimistic-concurrency revision mismatch.
	ErrStaleWrite = errors.New("partnerearnings/stale-write")
	// ErrInsufficient is a claim that exceeds currently available matured funds.
	ErrInsufficient = errors.New("partnerearnings/insufficient-available")
	// ErrInvalidState is a claim transition that the state machine rejects.
	ErrInvalidState = errors.New("partnerearnings/invalid-transition")
	// ErrDuplicateEvent is a source event that was already journaled.
	ErrDuplicateEvent = errors.New("partnerearnings/duplicate-source-event")
	// ErrUnresolved is a refund received before its payment is journaled; the
	// caller replays the same refund once the accrual exists.
	ErrUnresolved = errors.New("partnerearnings/unresolved")
	// ErrCurrencyMismatch is a source event whose currency is not the configured
	// program currency. The ledger never converts between currencies.
	ErrCurrencyMismatch = errors.New("partnerearnings/currency-mismatch")
)

// Clock supplies the current instant. Financial maturity and payment timestamps
// always come from the injected clock, never the system wall clock.
type Clock interface{ Now() time.Time }

// RealClock is the production UTC clock.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }

// ClockFunc adapts a function to Clock for tests and composition.
type ClockFunc func() time.Time

func (f ClockFunc) Now() time.Time { return f() }

// IDGenerator mints the IDs the service assigns to entries, claims and receipts.
// Implementations must return unique, stable, non-enumerable identifiers.
type IDGenerator interface{ NewID() string }

// ReceiptKey scopes one idempotency receipt. It is the actor/use-case/partner/
// currency identity required for claim creation and manual payment recording.
type ReceiptKey struct {
	ProgramID string
	PartnerID string
	ActorID   string
	UseCase   string
	Currency  string
	Key       string
}

// Receipt records the canonical fingerprint of an admitted idempotent request so
// the same identity replays the original outcome and a changed payload conflicts.
type Receipt struct {
	ProgramID   string    `json:"program_id" bson:"program_id"`
	PartnerID   string    `json:"partner_id" bson:"partner_id"`
	ActorID     string    `json:"actor_id" bson:"actor_id"`
	UseCase     string    `json:"use_case" bson:"use_case"`
	Currency    string    `json:"currency" bson:"currency"`
	Key         string    `json:"key" bson:"key"`
	Fingerprint string    `json:"fingerprint" bson:"fingerprint"`
	ClaimID     string    `json:"claim_id" bson:"claim_id"`
	OperationID string    `json:"operation_id,omitempty" bson:"operation_id,omitempty"`
	CreatedAt   time.Time `json:"created_at" bson:"created_at"`
}

// Repository is the narrow typed persistence port. Implementations own every
// datastore read/write, retry and the ledger write guard; they never compute
// rates, maturity, availability or claim transitions. Every method that mutates
// the ledger must be called with the Repository passed to WithTransaction's
// callback (that is, inside the transaction) so that all of a financial
// operation's writes commit or roll back together.
type Repository interface {
	MaturityRepository
	// WithTransaction runs fn atomically against the partner's ledger. The
	// adapter owns retry and a write guard against concurrent operations on the
	// same (programID, partnerID, currency) ledger. fn may run more than once;
	// it must use only the transaction Repository, perform no external effects,
	// and propagate database errors. An error returned by fn rolls back all of
	// its writes. A commit error may be reported as ErrUncertain.
	WithTransaction(ctx context.Context, programID, partnerID, currency string, fn func(Repository) error) error

	// ListEntries returns the partner's journal ordered by stable sequence.
	ListEntries(ctx context.Context, programID, partnerID string) ([]Entry, error)
	// AppendEntry appends one journal entry. The caller assigns a unique sequence
	// via NextSequence; the adapter rejects a reused sequence as ErrConflict.
	AppendEntry(ctx context.Context, e Entry) error
	// EntryBySource finds one entry by kind and source event id (idempotency anchor).
	EntryBySource(ctx context.Context, programID, partnerID, kind, sourceEventID string) (Entry, error)
	// NextSequence allocates the next stable, monotonic journal sequence for the
	// ledger. Committed sequences are stable and dense; a sequence allocated by
	// an aborted write is not a committed identity and may be reused by a later
	// commit, so callers must never persist a sequence outside the transaction
	// that allocated it.
	NextSequence(ctx context.Context, programID, partnerID string) (int64, error)

	// GetClaim fetches one claim by id.
	GetClaim(ctx context.Context, programID, id string) (Claim, error)
	// InsertClaim inserts a new claim.
	InsertClaim(ctx context.Context, c Claim) error
	// ReplaceClaim replaces a claim only if its revision matches, returning the
	// updated claim or ErrStaleWrite.
	ReplaceClaim(ctx context.Context, c Claim, expectedRevision int64) (Claim, error)
	// ListClaims pages claims; empty states means all states, empty partnerID
	// means all partners (administrator queue).
	ListClaims(ctx context.Context, programID, partnerID string, states []string, limit int, afterID string) ([]Claim, error)

	// GetReceipt fetches an idempotency receipt, ErrNotFound when absent.
	GetReceipt(ctx context.Context, key ReceiptKey) (Receipt, error)
	// PutReceipt stores an idempotency receipt, ErrAlreadyExists on duplicate.
	PutReceipt(ctx context.Context, r Receipt) error
}
