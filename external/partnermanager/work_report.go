package partnermanager

import (
	"context"
	"errors"
	"sort"
	"time"
)

// ErrReportCapacity means the complete pending set exceeds its explicit budget.
// Callers must not replace this error with truncated or zero backlog counts.
var ErrReportCapacity = errors.New("partnermanager/report-capacity")

// WorkReportCapacity bounds the combined pending rows across all four kinds.
const WorkReportCapacity = 10000

// WorkSnapshot is private, complete pending-queue evidence from one owning
// read, transferred without concurrent mutation. Completed history is not needed to report current processing backlog.
// Discovery positions are read positions, not source-acceptance watermarks.
type WorkSnapshot struct {
	ProgramID string                     `json:"-"`
	Pending   []WorkItem                 `json:"-"`
	Cursors   map[string]DiscoveryCursor `json:"-"`
}

// WorkReportingRepository returns complete owning evidence without mutation.
// A failed page or scope must return an empty snapshot, never partial counts.
type WorkReportingRepository interface {
	// ReadWorkSnapshot returns the complete WorkSnapshot of pending work for the
	// program without mutation; failed pages must yield an empty snapshot, never
	// partial counts.
	ReadWorkSnapshot(context.Context, string) (WorkSnapshot, error)
}

// WorkBacklogCounts separates disjoint scheduling states from overlapping
// attempted/decision subsets. Attempted includes the first active attempt.
// Pending is Ready+Delayed+Backoff+Leased; never add subsets to it. Delayed
// work has never been attempted, while backoff work has at least one attempt.
type WorkBacklogCounts struct {
	Pending                    int64      `json:"pending"`
	Ready                      int64      `json:"ready"`
	Delayed                    int64      `json:"delayed"`
	Backoff                    int64      `json:"backoff"`
	Leased                     int64      `json:"leased"`
	AttemptedPending           int64      `json:"attempted_pending"`
	DecisionAwaitingCompletion int64      `json:"decision_awaiting_completion"`
	OldestCreatedAt            *time.Time `json:"oldest_created_at,omitempty"`
	OldestReadyAt              *time.Time `json:"oldest_ready_at,omitempty"`
	OldestDecisionAt           *time.Time `json:"oldest_decision_at,omitempty"`
}

// KindWorkBacklog reports one supported work kind without source identities.
type KindWorkBacklog struct {
	Kind   string            `json:"kind"`
	Counts WorkBacklogCounts `json:"counts"`
}

// WorkBacklog describes only discovered durable work. An empty queue is not
// proof that source discovery, provider delivery or financial maturity is caught
// up. AsOf is the owning classification clock sampled after the read, not its
// database snapshot timestamp; no other owner participates in this snapshot.
type WorkBacklog struct {
	ProgramID string            `json:"program_id"`
	AsOf      time.Time         `json:"as_of"`
	Revision  string            `json:"revision"`
	Coverage  string            `json:"coverage"`
	Counts    WorkBacklogCounts `json:"counts"`
	Kinds     []KindWorkBacklog `json:"kinds"`
}

// earliest returns a UTC copy of value when it precedes at, or at unchanged,
// treating nil as unbounded.
func earliest(at *time.Time, value time.Time) *time.Time {
	if at == nil || value.Before(*at) {
		copy := value.UTC()
		return &copy
	}
	return at
}

// countPending classifies one pending item into disjoint scheduling states at
// time at (leased, delayed, backoff or ready) and updates overlapping
// attempted/decision subsets and oldest timestamps.
func countPending(c *WorkBacklogCounts, item WorkItem, at time.Time) {
	c.Pending++
	c.OldestCreatedAt = earliest(c.OldestCreatedAt, item.CreatedAt)
	if item.LeasedUntil.After(at) {
		c.Leased++
	} else if item.NextAttemptAt.After(at) {
		if item.Attempts == 0 {
			c.Delayed++
		} else {
			c.Backoff++
		}
	} else {
		c.Ready++
		c.OldestReadyAt = earliest(c.OldestReadyAt, item.NextAttemptAt)
	}
	if item.Attempts > 0 {
		c.AttemptedPending++
	}
	if item.Decision != nil {
		c.DecisionAwaitingCompletion++
		c.OldestDecisionAt = earliest(c.OldestDecisionAt, item.Decision.RecordedAt)
	}
}

// validatePending rejects malformed persisted items: identity, state, timing,
// lease pairing, error codes and, for decisions, outcome, timing and recomputed
// digest must all agree.
func validatePending(item WorkItem, program string, at time.Time) bool {
	if item.ProgramID != program || !validWorkKind(item.Kind) || !validWorkDeadline(item.Kind, item.InitialDueAt) || item.NextAttemptAt.Before(item.InitialDueAt) || item.State != WorkPending || !validWorkText(item.SourceID, 256) || item.ID != workID(program, item.Kind, item.SourceID) || !validWorkText(item.SourceFingerprint, 256) || item.CreatedAt.IsZero() || item.CreatedAt.After(at) || item.NextAttemptAt.Before(item.CreatedAt) || item.NextAttemptAt.IsZero() || item.Attempts < 0 || (item.Attempts == 0 && item.LastErrorCode != "") || (item.LastErrorCode != "" && !workCode.MatchString(item.LastErrorCode)) {
		return false
	}
	if (item.LeaseToken == "") != item.LeasedUntil.IsZero() || (item.LeaseToken != "" && (!validWorkText(item.LeaseToken, 128) || item.Attempts == 0 || !item.LeasedUntil.After(item.CreatedAt))) {
		return false
	}
	if d := item.Decision; d != nil {
		if !validWorkText(d.Fingerprint, 256) || d.ID != "decision_"+d.Fingerprint || !validWorkText(d.ActorID, 256) || !validWorkText(d.AcceptanceID, 256) || !workCode.MatchString(d.ReasonCode) || (d.Outcome != WorkAccepted && d.Outcome != WorkNoEntitlement && d.Outcome != WorkQuarantined) || d.RecordedAt.Before(item.CreatedAt) || d.RecordedAt.After(at) || item.Attempts == 0 {
			return false
		}
		want, err := workDecisionDigest(item, d.ActorID, d.Outcome, d.AcceptanceID, d.ReasonCode)
		if err != nil || d.Fingerprint != want {
			return false
		}
	}
	return true
}

// GetBacklog does no mutation or discovery. Capacity, corruption, cancellation
// or any failed scope discards the entire aggregate instead of returning zero.
func (q *WorkQueue) GetBacklog(ctx context.Context) (WorkBacklog, error) {
	if err := q.ready(ctx); err != nil {
		return WorkBacklog{}, err
	}
	repo, ok := q.repo.(WorkReportingRepository)
	if !ok || nilManagerDependency(repo) {
		return WorkBacklog{}, ErrUnavailable
	}
	snapshot, err := repo.ReadWorkSnapshot(ctx, q.config.ProgramID)
	if err != nil {
		return WorkBacklog{}, err
	}
	if len(snapshot.Pending) > WorkReportCapacity {
		return WorkBacklog{}, ErrReportCapacity
	}
	at := q.clock.Now().UTC()
	if at.IsZero() || snapshot.ProgramID != q.config.ProgramID || len(snapshot.Cursors) != 4 {
		return WorkBacklog{}, ErrUnavailable
	}
	byKind := map[string]*KindWorkBacklog{}
	cursors := make(map[string]DiscoveryCursor, 4)
	for _, kind := range []string{WorkSignup, WorkRevenue, WorkRevenueSource, WorkMaturity} {
		cursor, ok := snapshot.Cursors[kind]
		if !ok || !validWorkCursor(kind, cursor) || (cursor.Revision == 0 && cursor != (DiscoveryCursor{})) {
			return WorkBacklog{}, ErrUnavailable
		}
		cursors[kind] = cursor
		byKind[kind] = &KindWorkBacklog{Kind: kind}
	}
	seen := map[string]bool{}
	out := WorkBacklog{ProgramID: q.config.ProgramID, AsOf: at, Coverage: "discovered_pending_work", Kinds: []KindWorkBacklog{}}
	for _, item := range snapshot.Pending {
		if err := ctx.Err(); err != nil {
			return WorkBacklog{}, err
		}
		if !validatePending(item, q.config.ProgramID, at) || seen[item.ID] || cursors[item.Kind].Revision < 1 {
			return WorkBacklog{}, ErrUnavailable
		}
		seen[item.ID] = true
		countPending(&out.Counts, item, at)
		countPending(&byKind[item.Kind].Counts, item, at)
	}
	for _, kind := range []string{WorkSignup, WorkRevenue, WorkRevenueSource, WorkMaturity} {
		out.Kinds = append(out.Kinds, *byKind[kind])
	}
	// Copy before canonical sorting so a repository's retained arrays cannot be
	// changed by reporting. Private source/lease/decision fields remain in the
	// revision's canonical evidence despite being excluded from public JSON.
	items := append([]WorkItem(nil), snapshot.Pending...)
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	private := make([]persistedWorkRevision, 0, len(items))
	for _, item := range items {
		private = append(private, persistedWorkRevision{Item: item, Source: item.SourceID, Fingerprint: item.SourceFingerprint, InitialDueAt: item.InitialDueAt.UTC(), Lease: item.LeaseToken, DecisionActor: decisionActor(item), DecisionFingerprint: decisionFingerprint(item)})
	}
	out.Revision, err = sourceDigest(struct {
		Program string
		Pending []persistedWorkRevision
		Cursors map[string]DiscoveryCursor
	}{snapshot.ProgramID, private, cursors})
	if err != nil {
		return WorkBacklog{}, err
	}
	if err := ctx.Err(); err != nil {
		return WorkBacklog{}, err
	}
	if out.Validate() != nil {
		return WorkBacklog{}, ErrUnavailable
	}
	return out, nil
}

// persistedWorkRevision is the private comparison copy of a work item plus its
// source, lease and decision fields used to detect concurrent mutation between
// reads.
type persistedWorkRevision struct {
	Item                                                           WorkItem
	Source, Fingerprint, Lease, DecisionActor, DecisionFingerprint string
	InitialDueAt                                                   time.Time
}

// decisionActor returns the decision's actor ID, or "" when the item has no
// decision.
func decisionActor(item WorkItem) string {
	if item.Decision != nil {
		return item.Decision.ActorID
	}
	return ""
}

// decisionFingerprint returns the decision's fingerprint, or "" when the item
// has no decision.
func decisionFingerprint(item WorkItem) string {
	if item.Decision != nil {
		return item.Decision.Fingerprint
	}
	return ""
}

// validBacklogCounts enforces non-negative counts within report capacity,
// disjoint-state sums equal to Pending, subset constraints, and
// presence/consistency of oldest timestamps relative to the classification
// time.
func validBacklogCounts(c WorkBacklogCounts, at time.Time) bool {
	if c.Pending < 0 || c.Pending > WorkReportCapacity || c.Ready < 0 || c.Delayed < 0 || c.Backoff < 0 || c.Leased < 0 || c.AttemptedPending < 0 || c.DecisionAwaitingCompletion < 0 || c.Ready > c.Pending || c.Delayed > c.Pending || c.Backoff > c.Pending || c.Leased > c.Pending || c.AttemptedPending > c.Pending || c.Delayed > c.Pending-c.AttemptedPending || c.Backoff+c.Leased > c.AttemptedPending || c.DecisionAwaitingCompletion > c.AttemptedPending || c.Ready+c.Delayed+c.Backoff+c.Leased != c.Pending {
		return false
	}
	for _, pair := range []struct {
		count int64
		at    *time.Time
	}{{c.Pending, c.OldestCreatedAt}, {c.Ready, c.OldestReadyAt}, {c.DecisionAwaitingCompletion, c.OldestDecisionAt}} {
		if (pair.count == 0) != (pair.at == nil) || (pair.at != nil && (pair.at.IsZero() || pair.at.After(at))) {
			return false
		}
	}
	if c.OldestReadyAt != nil && c.OldestReadyAt.Before(*c.OldestCreatedAt) {
		return false
	}
	if c.OldestDecisionAt != nil && c.OldestDecisionAt.Before(*c.OldestCreatedAt) {
		return false
	}
	return true
}

// Validate rejects malformed service projections at manager/transport boundaries.
func (r WorkBacklog) Validate() error {
	if !validWorkText(r.ProgramID, 128) || r.AsOf.IsZero() || len(r.Revision) != 64 || r.Coverage != "discovered_pending_work" || len(r.Kinds) != 4 || !validBacklogCounts(r.Counts, r.AsOf) {
		return ErrUnavailable
	}
	for _, ch := range r.Revision {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return ErrUnavailable
		}
	}
	var sum WorkBacklogCounts
	seen := map[string]bool{}
	for _, row := range r.Kinds {
		if !validWorkKind(row.Kind) || seen[row.Kind] || !validBacklogCounts(row.Counts, r.AsOf) {
			return ErrUnavailable
		}
		seen[row.Kind] = true
		sum.Pending += row.Counts.Pending
		sum.Ready += row.Counts.Ready
		sum.Delayed += row.Counts.Delayed
		sum.Backoff += row.Counts.Backoff
		sum.Leased += row.Counts.Leased
		sum.AttemptedPending += row.Counts.AttemptedPending
		sum.DecisionAwaitingCompletion += row.Counts.DecisionAwaitingCompletion
		if row.Counts.OldestCreatedAt != nil {
			sum.OldestCreatedAt = earliest(sum.OldestCreatedAt, *row.Counts.OldestCreatedAt)
		}
		if row.Counts.OldestReadyAt != nil {
			sum.OldestReadyAt = earliest(sum.OldestReadyAt, *row.Counts.OldestReadyAt)
		}
		if row.Counts.OldestDecisionAt != nil {
			sum.OldestDecisionAt = earliest(sum.OldestDecisionAt, *row.Counts.OldestDecisionAt)
		}
	}
	if !sameBacklogCounts(sum, r.Counts) {
		return ErrUnavailable
	}
	return nil
}

// sameBacklogCounts compares all counts and oldest timestamps, treating nil and
// non-nil times as unequal.
func sameBacklogCounts(a, b WorkBacklogCounts) bool {
	return a.Pending == b.Pending && a.Ready == b.Ready && a.Delayed == b.Delayed && a.Backoff == b.Backoff && a.Leased == b.Leased && a.AttemptedPending == b.AttemptedPending && a.DecisionAwaitingCompletion == b.DecisionAwaitingCompletion && sameTime(a.OldestCreatedAt, b.OldestCreatedAt) && sameTime(a.OldestReadyAt, b.OldestReadyAt) && sameTime(a.OldestDecisionAt, b.OldestDecisionAt)
}

// sameTime compares two optional times, equal only when both are present and
// equal or both absent.
func sameTime(a, b *time.Time) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && a.Equal(*b))
}
