package partnermanager

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	ErrWorkConflict  = errors.New("partnermanager/work-conflict")
	ErrWorkLeaseLost = errors.New("partnermanager/work-lease-lost")
	ErrWorkUncertain = errors.New("partnermanager/work-uncertain")
)

const (
	WorkSignup        = "signup"
	WorkRevenue       = "revenue"
	WorkRevenueSource = "revenue_source"
	WorkMaturity      = "maturity"
	WorkPending       = "pending"
	WorkComplete      = "complete"
	WorkAccepted      = "accepted"
	WorkNoEntitlement = "no_entitlement"
	WorkQuarantined   = "quarantined"
)

var workCode = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// WorkCandidate identifies immutable owning-service input. Fingerprints are
// derived from private canonical source fields, not public JSON that omits them.
type WorkCandidate struct {
	SourceID          string `json:"-"`
	SourceFingerprint string `json:"-"`
	// DueAt freezes the owning maturity deadline. Other work kinds require zero.
	DueAt time.Time `json:"-"`
}

// DiscoveryCursor is a read position, never a financial acknowledgement. Queue
// insertion and cursor advancement commit together. A completed sweep resets
// the position; unresolved queued jobs have independent durable retry times.
type DiscoveryCursor struct {
	Revision      int64
	AfterID       string
	AfterSequence int64
}

// WorkDecision records the owning consumer's immutable acceptance or explicit
// refusal before another service may acknowledge its source. It does not create
// a commission: accepted decisions refer to the financial/referral owner proof.
type WorkDecision struct {
	ID, Outcome, AcceptanceID, ReasonCode string
	ActorID                               string `json:"-"`
	Fingerprint                           string `json:"-"`
	RecordedAt                            time.Time
}

// WorkItem is one durable queue record. JSON-hidden fields carry the private
// source identity, fingerprint, fencing lease token and InitialDueAt, which
// survives retries and cannot be retimed except by maturity deadlines.
// Decision, when present, is the recorded acceptance or refusal receipt.
type WorkItem struct {
	ID, ProgramID, Kind, State string
	SourceID                   string `json:"-"`
	SourceFingerprint          string `json:"-"`
	// InitialDueAt survives cursor advancement, retries and receipt replay.
	// Only maturity work has a deadline; it cannot be retimed by the queue.
	InitialDueAt                          time.Time `json:"-"`
	CreatedAt, NextAttemptAt, LeasedUntil time.Time
	Attempts                              int64
	LeaseToken                            string `json:"-"`
	LastErrorCode                         string
	Decision                              *WorkDecision
}

// WorkRepository owns guarded atomic discovery, leases and decision receipts.
// Lease tokens fence stale workers. Decision receipt replay cannot advance an
// owning feed; CompleteWork is called only after that feed confirms acceptance.
type WorkRepository interface {
	// GetDiscoveryCursor returns the DiscoveryCursor for the identified work kind
	// within a program, reading discovery progress owned by WorkRepository.
	GetDiscoveryCursor(context.Context, string, string) (DiscoveryCursor, error)
	// EnqueueWorkPage stores a discovered page of WorkItems between the prior and
	// new cursors, atomically advancing guarded discovery within WorkRepository.
	EnqueueWorkPage(context.Context, string, string, DiscoveryCursor, DiscoveryCursor, []WorkItem) error
	// GetWorkItem reads the WorkItem for the identified program and item, a lookup
	// owned by the work repository.
	GetWorkItem(context.Context, string, string) (WorkItem, error)
	// LeaseWork grants a lease on pending WorkItems from the given time for the
	// duration, count and token, fencing stale workers per the repository's
	// contract.
	LeaseWork(context.Context, string, string, time.Time, time.Duration, string, int) ([]WorkItem, error)
	// RecordWorkDecision records a WorkDecision for the item at the given time,
	// returning the stored receipt; replay cannot advance an owning feed per the
	// contract.
	RecordWorkDecision(context.Context, WorkItem, WorkDecision, time.Time) (WorkDecision, error)
	// RetryWork schedules the given WorkItem for retry with the supplied reason and
	// times, a mutation owned by WorkRepository.
	RetryWork(context.Context, WorkItem, string, time.Time, time.Time) error
	// CompleteWork marks the identified WorkItem complete at the given time;
	// callers invoke it only after the owning feed confirms acceptance.
	CompleteWork(context.Context, WorkItem, string, time.Time) error
}

// WorkQueueConfig bounds queue behavior: program identity, lease duration
// (1s..10m) and exponential retry timing capped at RetryMax.
type WorkQueueConfig struct {
	ProgramID                          string
	LeaseDuration, RetryBase, RetryMax time.Duration
}

// WorkQueue is an in-process owning workflow service. Current source-scoped
// authority belongs to the manager/worker before every call, including replay.
// No HTTP route, automatic startup or financial provider side effect is supplied.
type WorkQueue struct {
	repo   WorkRepository
	clock  Clock
	config WorkQueueConfig
}

// NewWorkQueue validates repository, clock and configuration bounds, failing
// with ErrUnavailable or ErrInvalid; it performs no I/O or seeding.
func NewWorkQueue(repo WorkRepository, clock Clock, cfg WorkQueueConfig) (*WorkQueue, error) {
	if nilManagerDependency(repo) || nilManagerDependency(clock) {
		return nil, ErrUnavailable
	}
	if !validWorkText(cfg.ProgramID, 128) || cfg.LeaseDuration < time.Second || cfg.LeaseDuration > 10*time.Minute || cfg.RetryBase < time.Second || cfg.RetryMax < cfg.RetryBase || cfg.RetryMax > 24*time.Hour {
		return nil, ErrInvalid
	}
	return &WorkQueue{repo, clock, cfg}, nil
}

// validWorkText accepts a non-empty trimmed string of at most max bytes
// containing no CR, LF or NUL.
func validWorkText(value string, max int) bool {
	return value != "" && len(value) <= max && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\r\n\x00")
}

// validWorkKind accepts exactly the four supported work kinds.
func validWorkKind(kind string) bool {
	return kind == WorkSignup || kind == WorkRevenue || kind == WorkRevenueSource || kind == WorkMaturity
}

// validWorkDeadline requires a deadline exactly for maturity work and its
// absence for all other kinds.
func validWorkDeadline(kind string, at time.Time) bool {
	return (kind == WorkMaturity) != at.IsZero()
}

// workDecisionDigest fingerprints a decision's immutable fields; maturity
// decisions additionally include the retained InitialDueAt so replay cannot
// retiming the deadline.
func workDecisionDigest(item WorkItem, actor, outcome, acceptance, reason string) (string, error) {
	fields := []string{item.ID, item.SourceFingerprint, actor, outcome, acceptance, reason}
	if item.Kind == WorkMaturity {
		fields = append(fields, item.InitialDueAt.UTC().Format(time.RFC3339Nano))
	}
	return sourceDigest(fields)
}

// workID derives the deterministic work-item identity from program, kind and
// source, so the same source always maps to one queue record.
func workID(program, kind, source string) string {
	body, _ := json.Marshal([]string{program, kind, source})
	sum := sha256.Sum256(body)
	return "work_" + hex.EncodeToString(sum[:])
}

// validWorkCursor accepts non-negative positions with a clean AfterID; revenue
// cursors use only AfterID while all other kinds use only AfterSequence.
func validWorkCursor(kind string, c DiscoveryCursor) bool {
	if c.Revision < 0 || c.AfterSequence < 0 || len(c.AfterID) > 256 || strings.TrimSpace(c.AfterID) != c.AfterID || strings.ContainsAny(c.AfterID, "\r\n\x00") {
		return false
	}
	if kind == WorkRevenue {
		return c.AfterID == ""
	}
	return c.AfterSequence == 0
}

// ready rejects a nil context, an expired context, and a queue with nil
// repository or clock.
func (q *WorkQueue) ready(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if q == nil || nilManagerDependency(q.repo) || nilManagerDependency(q.clock) {
		return ErrUnavailable
	}
	return nil
}

// Cursor returns the durable discovery position for a kind, treating not-found
// as an empty cursor and rejecting a persisted malformed cursor with
// ErrUnavailable.
func (q *WorkQueue) Cursor(ctx context.Context, kind string) (DiscoveryCursor, error) {
	if err := q.ready(ctx); err != nil {
		return DiscoveryCursor{}, err
	}
	if !validWorkKind(kind) {
		return DiscoveryCursor{}, ErrInvalid
	}
	cursor, err := q.repo.GetDiscoveryCursor(ctx, q.config.ProgramID, kind)
	if singleManagerAbsence(err, ErrNotFound) {
		return DiscoveryCursor{}, nil
	}
	if err == nil && !validWorkCursor(kind, cursor) {
		return DiscoveryCursor{}, ErrUnavailable
	}
	return cursor, err
}

// EnqueuePage advances only discovery, atomically retaining every source in the
// page. Changed immutable input conflicts; errors cannot skip the whole page.
func (q *WorkQueue) EnqueuePage(ctx context.Context, kind string, expected, next DiscoveryCursor, candidates []WorkCandidate) error {
	if err := q.ready(ctx); err != nil {
		return err
	}
	if !validWorkKind(kind) || !validWorkCursor(kind, expected) || !validWorkCursor(kind, next) || len(candidates) > 200 {
		return ErrInvalid
	}
	now := q.clock.Now().UTC()
	if now.IsZero() {
		return ErrInvalid
	}
	items := make([]WorkItem, 0, len(candidates))
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if !validWorkText(candidate.SourceID, 256) || !validWorkText(candidate.SourceFingerprint, 256) || !validWorkDeadline(kind, candidate.DueAt) || seen[candidate.SourceID] {
			return ErrInvalid
		}
		seen[candidate.SourceID] = true
		due := candidate.DueAt.UTC()
		nextAttempt := now
		if due.After(nextAttempt) {
			nextAttempt = due
		}
		items = append(items, WorkItem{ID: workID(q.config.ProgramID, kind, candidate.SourceID), ProgramID: q.config.ProgramID, Kind: kind, State: WorkPending, SourceID: candidate.SourceID, SourceFingerprint: candidate.SourceFingerprint, InitialDueAt: due, CreatedAt: now, NextAttemptAt: nextAttempt})
	}
	return q.repo.EnqueueWorkPage(ctx, q.config.ProgramID, kind, expected, next, items)
}

// Get returns the work item for a kind and source, addressing it by the derived
// work ID.
func (q *WorkQueue) Get(ctx context.Context, kind, source string) (WorkItem, error) {
	if err := q.ready(ctx); err != nil {
		return WorkItem{}, err
	}
	if !validWorkKind(kind) || !validWorkText(source, 256) {
		return WorkItem{}, ErrInvalid
	}
	return q.repo.GetWorkItem(ctx, q.config.ProgramID, workID(q.config.ProgramID, kind, source))
}

// Lease returns due items under an opaque fencing token. Restarting the process
// does not erase attempts, backoff or committed-but-unacknowledged decisions.
func (q *WorkQueue) Lease(ctx context.Context, kind string, limit int) ([]WorkItem, error) {
	if err := q.ready(ctx); err != nil {
		return nil, err
	}
	if !validWorkKind(kind) || limit < 1 || limit > 200 {
		return nil, ErrInvalid
	}
	at := q.clock.Now().UTC()
	if at.IsZero() {
		return nil, ErrInvalid
	}
	return q.repo.LeaseWork(ctx, q.config.ProgramID, kind, at, q.config.LeaseDuration, rand.Text(), limit)
}

// validateLease checks that an item belongs to this queue's program, carries a
// well-formed identity, fingerprint and fencing lease, and has been attempted
// at least once.
func (q *WorkQueue) validateLease(item WorkItem) bool {
	return item.ProgramID == q.config.ProgramID && validWorkKind(item.Kind) && validWorkDeadline(item.Kind, item.InitialDueAt) && validWorkText(item.SourceID, 256) && item.ID == workID(item.ProgramID, item.Kind, item.SourceID) && validWorkText(item.SourceFingerprint, 256) && validWorkText(item.LeaseToken, 128) && item.Attempts > 0 && !item.LeasedUntil.IsZero()
}

// Decide is called only after the owning use case durably succeeds or supplies
// conclusive no-entitlement evidence. Raw errors, absence guesses and browser
// eligibility fields must never be converted into a successful decision.
func (q *WorkQueue) Decide(ctx context.Context, item WorkItem, actor, outcome, acceptance, reason string) (WorkDecision, error) {
	return q.decide(ctx, item, actor, outcome, acceptance, reason, time.Time{})
}

// decide binds an optional owning receipt time to the same clock sample that
// becomes RecordedAt. A pre-call clock check cannot fence a rollback between
// that check and recording; the financial worker supplies this minimum itself.
func (q *WorkQueue) decide(ctx context.Context, item WorkItem, actor, outcome, acceptance, reason string, acceptedAt time.Time) (WorkDecision, error) {
	if err := q.ready(ctx); err != nil {
		return WorkDecision{}, err
	}
	if !q.validateLease(item) || !validWorkText(actor, 256) || !validWorkText(acceptance, 256) || !workCode.MatchString(reason) || (outcome != WorkAccepted && outcome != WorkNoEntitlement && outcome != WorkQuarantined) {
		return WorkDecision{}, ErrInvalid
	}
	fp, err := workDecisionDigest(item, actor, outcome, acceptance, reason)
	if err != nil {
		return WorkDecision{}, err
	}
	now := q.clock.Now().UTC()
	if now.IsZero() || now.Before(item.CreatedAt) || now.Before(acceptedAt) {
		return WorkDecision{}, ErrInvalid
	}
	decision := WorkDecision{ID: "decision_" + fp, Outcome: outcome, AcceptanceID: acceptance, ReasonCode: reason, ActorID: actor, Fingerprint: fp, RecordedAt: now}
	return q.repo.RecordWorkDecision(ctx, item, decision, now)
}

// Retry reschedules a leased item with exponential backoff capped at RetryMax,
// never earlier than its InitialDueAt or CreatedAt, and records the validated
// error code.
func (q *WorkQueue) Retry(ctx context.Context, item WorkItem, errorCode string) error {
	if err := q.ready(ctx); err != nil {
		return err
	}
	if !q.validateLease(item) || !workCode.MatchString(errorCode) {
		return ErrInvalid
	}
	delay := q.config.RetryBase
	for n := int64(1); n < item.Attempts && delay < q.config.RetryMax; n++ {
		if delay > q.config.RetryMax/2 {
			delay = q.config.RetryMax
			break
		}
		delay *= 2
	}
	if delay > q.config.RetryMax {
		delay = q.config.RetryMax
	}
	now := q.clock.Now().UTC()
	if now.IsZero() {
		return ErrInvalid
	}
	next := now.Add(delay)
	if next.Before(item.InitialDueAt) {
		next = item.InitialDueAt.UTC()
	}
	if next.Before(item.CreatedAt) {
		next = item.CreatedAt.UTC()
	}
	return q.repo.RetryWork(ctx, item, errorCode, now, next)
}

// Complete acknowledges the work item's decision receipt using the fencing
// lease and a sample of the shared clock.
func (q *WorkQueue) Complete(ctx context.Context, item WorkItem, decisionID string) error {
	if err := q.ready(ctx); err != nil {
		return err
	}
	if !q.validateLease(item) || !validWorkText(decisionID, 256) {
		return ErrInvalid
	}
	at := q.clock.Now().UTC()
	if at.IsZero() {
		return ErrInvalid
	}
	return q.repo.CompleteWork(ctx, item, decisionID, at)
}
