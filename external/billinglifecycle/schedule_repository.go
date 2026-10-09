package billinglifecycle

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

const (
	ColdLane           = "cold"
	RefreshLane        = "refresh"
	RetiredLane        = "retired"
	scheduleJobKind    = "partners_lifecycle_job"
	scheduleCursorKind = "partners_lifecycle_scan"
)

// ScheduledSource is private immutable owning-discovery input. The scheduling
// service must validate the native page and current authority before admission.
// This descriptor neither authenticates a caller nor proves billing ownership.
type ScheduledSource struct {
	Scope                                               billing.RevenueScope    `json:"-"`
	Kind, SourceID, PrincipalID, SubscriptionID, FactID string                  `json:"-"`
	Checkout                                            *billing.CheckoutIntent `json:"-"`
}

// ScheduledJob owns host execution metadata only. Status work remains recurring
// independently of financial completion. A retained preparation is an original
// input pointer, not a capture receipt or permission for a replacement worker.
type ScheduledJob struct {
	LastSupersessionID                    string                                 `json:"-"`
	LastCompletionID                      string                                 `json:"-"`
	CadenceAnchor                         time.Time                              `json:"-"`
	Source                                ScheduledSource                        `json:"-"`
	Revision                              int64                                  `json:"-"`
	Lane                                  string                                 `json:"-"`
	CreatedAt, NextAttemptAt, LeasedUntil time.Time                              `json:"-"`
	Attempts, Fence                       int64                                  `json:"-"`
	LeaseActor, LeaseToken                string                                 `json:"-"`
	OriginalStatus                        *billing.SubscriptionStatusPreparation `json:"-"`
	CheckoutPrepared                      bool                                   `json:"-"`
}

type ScheduleCursor struct {
	Revision int64  `json:"-"`
	AfterID  string `json:"-"`
}
type ScheduleScan struct {
	Scope billing.RevenueScope `json:"-"`
	Lane  string               `json:"-"`
	Limit int                  `json:"-"`
}
type ScheduleSnapshot struct {
	Cursor ScheduleCursor `json:"-"`
	Jobs   []ScheduledJob `json:"-"`
}
type ScheduleWrite struct {
	ExpectedRevision int64        `json:"-"`
	Job              ScheduledJob `json:"-"`
}
type ScheduleCommit struct {
	Scope       billing.RevenueScope `json:"-"`
	Lane        string               `json:"-"`
	Expected    ScheduleCursor       `json:"-"`
	NextAfterID string               `json:"-"`
	Writes      []ScheduleWrite      `json:"-"`
}

// ScheduleRepository provides one snapshot and one atomic cursor/bootstrap CAS.
// Job writes are confined to never-executed records; acquisition, disposition
// and input-stage writes require their execution-specific fenced operations.
// The owning scheduling service selects due work, leases, retries and cursor
// progress, and checks current authority outside retryable storage callbacks.
// It must never interpret a storage error as an empty or completed sweep.
type ScheduleRepository interface {
	ReadScan(context.Context, ScheduleScan) (ScheduleSnapshot, error)
	CommitScan(context.Context, ScheduleCommit) error
}

// RecordScheduleRepository borrows the host's prepared encrypted native store.
// Multi-kind job/cursor changes use ONE native transaction. The guard names the
// scan, not a restriction on which record kinds its transaction may access.
// No provider I/O, authority, due-time policy or automatic startup is supplied.
type RecordScheduleRepository struct{ store recordstore.Store }

func NewRecordScheduleRepository(store recordstore.Store) (*RecordScheduleRepository, error) {
	if nilPort(store) {
		return nil, recordstore.ErrUnavailable
	}
	return &RecordScheduleRepository{store}, nil
}

type scheduleSourcePayload struct {
	Scope                                               billing.RevenueScope
	Kind, SourceID, PrincipalID, SubscriptionID, FactID string
	Checkout                                            *checkoutPayload
}
type scheduleJobPayload struct {
	LastSupersessionID                    string
	LastCompletionID                      string
	CadenceAnchor                         time.Time
	Schema                                int
	Source                                scheduleSourcePayload
	Lane                                  string
	CreatedAt, NextAttemptAt, LeasedUntil time.Time
	Attempts, Fence                       int64
	LeaseActor, LeaseToken                string
	OriginalStatus                        *statusPreparationPayload
	CheckoutPrepared                      bool
}
type scheduleCursorPayload struct {
	Schema        int
	Scope         billing.RevenueScope
	Lane, AfterID string
}

func scheduleSource(s ScheduledSource) scheduleSourcePayload {
	p := scheduleSourcePayload{Scope: s.Scope, Kind: s.Kind, SourceID: s.SourceID, PrincipalID: s.PrincipalID, SubscriptionID: s.SubscriptionID, FactID: s.FactID}
	if s.Checkout != nil {
		v := payload(CheckoutInput{Intent: *s.Checkout})
		p.Checkout = &v
	}
	return p
}
func (p scheduleSourcePayload) source() ScheduledSource {
	s := ScheduledSource{Scope: p.Scope, Kind: p.Kind, SourceID: p.SourceID, PrincipalID: p.PrincipalID, SubscriptionID: p.SubscriptionID, FactID: p.FactID}
	if p.Checkout != nil {
		v := p.Checkout.input().Intent
		s.Checkout = &v
	}
	return s
}
func scheduleJob(j ScheduledJob) scheduleJobPayload {
	p := scheduleJobPayload{LastSupersessionID: j.LastSupersessionID, LastCompletionID: j.LastCompletionID, CadenceAnchor: j.CadenceAnchor, CheckoutPrepared: j.CheckoutPrepared, Schema: 1, Source: scheduleSource(j.Source), Lane: j.Lane, CreatedAt: j.CreatedAt, NextAttemptAt: j.NextAttemptAt, LeasedUntil: j.LeasedUntil, Attempts: j.Attempts, Fence: j.Fence, LeaseActor: j.LeaseActor, LeaseToken: j.LeaseToken}
	if j.OriginalStatus != nil {
		v := statusPreparation(*j.OriginalStatus)
		p.OriginalStatus = &v
	}
	return p
}
func (p scheduleJobPayload) job(revision int64) ScheduledJob {
	j := ScheduledJob{LastSupersessionID: p.LastSupersessionID, LastCompletionID: p.LastCompletionID, CadenceAnchor: p.CadenceAnchor, CheckoutPrepared: p.CheckoutPrepared, Source: p.Source.source(), Revision: revision, Lane: p.Lane, CreatedAt: p.CreatedAt, NextAttemptAt: p.NextAttemptAt, LeasedUntil: p.LeasedUntil, Attempts: p.Attempts, Fence: p.Fence, LeaseActor: p.LeaseActor, LeaseToken: p.LeaseToken}
	if p.OriginalStatus != nil {
		v := p.OriginalStatus.input()
		j.OriginalStatus = &v
	}
	return j
}
func scheduleLane(lane string) bool {
	return lane == ColdLane || lane == RefreshLane || lane == RetiredLane
}
func scheduleText(v string) bool {
	return v != "" && len(v) <= 256 && strings.TrimSpace(v) == v && !strings.ContainsAny(v, "\r\n\x00")
}
func scheduleScope(s billing.RevenueScope) bool {
	return billing.ValidateRevenueHistoryScopes([]billing.RevenueScope{s}) == nil
}
func schedulePosition(id string) bool {
	if id == "" {
		return true
	}
	if len(id) != 64 {
		return false
	}
	for _, c := range id {
		allowed := c >= '0' && c <= '9' || c >= 'a' && c <= 'f'
		if !allowed {
			return false
		}
	}
	return true
}
func scheduledIdentity(s ScheduledSource) string {
	return digest([]any{"partners.lifecycle.job.v1", s.Scope, s.Kind, s.SourceID})
}
func schedulePartition(s billing.RevenueScope, lane string) string {
	return digest([]any{"partners.lifecycle.schedule.v1", s, lane})
}
func scheduleCursorID(s billing.RevenueScope, lane string) string {
	return digest([]any{"partners.lifecycle.scan.v1", s, lane})
}

// Validate codec shape and immutable native references, never admission policy.
func scheduleSourceShape(s ScheduledSource) bool {
	if !scheduleScope(s.Scope) || !scheduleText(s.SourceID) || !scheduleText(s.PrincipalID) {
		return false
	}
	if s.Kind == billing.LifecycleCheckoutSources {
		return s.Checkout != nil && s.Checkout.ValidateAcknowledgedSubscription() == nil && s.Checkout.Scope == s.Scope && s.Checkout.Request.UserID == s.PrincipalID && s.SubscriptionID == "" && s.FactID == "" && s.SourceID == billing.LifecycleDiscoverySourceID(s.Scope, s.Kind, s.Checkout.ID)
	}
	return s.Kind == billing.LifecycleSubscriptionSources && s.Checkout == nil && scheduleText(s.SubscriptionID) && (s.FactID == "" || scheduleText(s.FactID)) && s.SourceID == billing.LifecycleDiscoverySourceID(s.Scope, s.Kind, s.SubscriptionID)
}
func validateScheduledJob(j ScheduledJob) error {
	if !scheduleSourceShape(j.Source) || !scheduleLane(j.Lane) || j.Revision < 1 || j.CreatedAt.IsZero() || j.NextAttemptAt.IsZero() || j.Attempts < 0 || j.Fence < 0 {
		return recordstore.ErrInvalid
	}
	if j.LeasedUntil.IsZero() {
		if j.LeaseActor != "" || j.LeaseToken != "" {
			return recordstore.ErrInvalid
		}
	} else if j.Fence < 1 || !scheduleText(j.LeaseActor) || !scheduleText(j.LeaseToken) {
		return recordstore.ErrInvalid
	}
	if j.LastSupersessionID != "" && (!schedulePosition(j.LastSupersessionID) || j.Source.Kind != billing.LifecycleSubscriptionSources || j.Fence < 1) {
		return recordstore.ErrInvalid
	}
	if j.LastCompletionID != "" && (!schedulePosition(j.LastCompletionID) || j.Fence < 1) {
		return recordstore.ErrInvalid
	}
	if !j.CadenceAnchor.IsZero() && (j.Source.Kind != billing.LifecycleSubscriptionSources || j.Fence < 1 || (j.OriginalStatus != nil && j.OriginalStatus.RequestedAt.Before(j.CadenceAnchor))) {
		return recordstore.ErrInvalid
	}
	if j.CheckoutPrepared && (j.Source.Kind != billing.LifecycleCheckoutSources || j.Fence < 1 || j.Attempts < 1) {
		return recordstore.ErrInvalid
	}
	if j.OriginalStatus != nil && (j.OriginalStatus.Validate() != nil || j.OriginalStatus.Scope != j.Source.Scope || j.OriginalStatus.PrincipalID != j.Source.PrincipalID || j.OriginalStatus.SubscriptionID != j.Source.SubscriptionID) {
		return recordstore.ErrInvalid
	}
	return nil
}
func jobRecord(j ScheduledJob) (recordstore.Record, error) {
	if err := validateScheduledJob(j); err != nil {
		return recordstore.Record{}, err
	}
	r, err := recordstore.NewRecord(scheduleJobKind, scheduledIdentity(j.Source), schedulePartition(j.Source.Scope, j.Lane), j.Revision, scheduleJob(j))
	r.State = j.Lane
	return r, err
}
func strictScheduleDecode(raw []byte, p any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(p) != nil || d.Decode(new(any)) != io.EOF {
		return recordstore.ErrUnavailable
	}
	return nil
}
func readJob(r recordstore.Record) (ScheduledJob, error) {
	var p scheduleJobPayload
	if strictScheduleDecode(r.Data, &p) != nil || p.Schema != 1 {
		return ScheduledJob{}, recordstore.ErrUnavailable
	}
	if p.Source.Checkout != nil && (p.Source.Checkout.Schema != 1 || p.Source.Checkout.Evidence != nil) {
		return ScheduledJob{}, recordstore.ErrUnavailable
	}
	j := p.job(r.Revision)
	want, err := jobRecord(j)
	if err != nil || r.Kind != want.Kind || r.ID != want.ID || r.Partition != want.Partition || r.State != want.State || r.Sequence != 0 || r.ExpiresAt != nil {
		return ScheduledJob{}, recordstore.ErrUnavailable
	}
	return j, nil
}
func readCursor(ctx context.Context, tx recordstore.Tx, scope billing.RevenueScope, lane string) (ScheduleCursor, error) {
	r, err := tx.Get(ctx, scheduleCursorKind, scheduleCursorID(scope, lane))
	if soleNotFound(err) {
		return ScheduleCursor{}, nil
	}
	if err != nil {
		return ScheduleCursor{}, err
	}
	var p scheduleCursorPayload
	if strictScheduleDecode(r.Data, &p) != nil || p.Schema != 1 || p.Scope != scope || p.Lane != lane || !schedulePosition(p.AfterID) || r.Kind != scheduleCursorKind || r.ID != scheduleCursorID(scope, lane) || r.Partition != schedulePartition(scope, lane) || r.Revision < 1 || r.Sequence != 0 || r.State != "" || r.ExpiresAt != nil {
		return ScheduleCursor{}, recordstore.ErrUnavailable
	}
	return ScheduleCursor{r.Revision, p.AfterID}, nil
}
func (r *RecordScheduleRepository) ready(ctx context.Context) error {
	if ctx == nil {
		return recordstore.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || nilPort(r.store) {
		return recordstore.ErrUnavailable
	}
	return nil
}
func (r *RecordScheduleRepository) ReadScan(ctx context.Context, q ScheduleScan) (ScheduleSnapshot, error) {
	if err := r.ready(ctx); err != nil {
		return ScheduleSnapshot{}, err
	}
	if !scheduleScope(q.Scope) || !scheduleLane(q.Lane) || q.Limit < 1 || q.Limit > 200 {
		return ScheduleSnapshot{}, recordstore.ErrInvalid
	}
	var out ScheduleSnapshot
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out = ScheduleSnapshot{}
		c, err := readCursor(ctx, tx, q.Scope, q.Lane)
		if err != nil {
			return err
		}
		rows, err := tx.Find(ctx, recordstore.Query{Kind: scheduleJobKind, Partition: schedulePartition(q.Scope, q.Lane), AfterID: c.AfterID, Limit: q.Limit})
		if err != nil {
			return err
		}
		if len(rows) > q.Limit {
			return recordstore.ErrUnavailable
		}
		after := c.AfterID
		for _, row := range rows {
			j, err := readJob(row)
			if err != nil {
				return err
			}
			if j.Source.Scope != q.Scope || j.Lane != q.Lane || row.ID <= after {
				return recordstore.ErrUnavailable
			}
			out.Jobs = append(out.Jobs, j)
			after = row.ID
		}
		out.Cursor = c
		return nil
	})
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		return ScheduleSnapshot{}, err
	}
	return out, nil
}
func (r *RecordScheduleRepository) CommitScan(ctx context.Context, c ScheduleCommit) error {
	if err := r.ready(ctx); err != nil {
		return err
	}
	if !scheduleScope(c.Scope) || !scheduleLane(c.Lane) || c.Expected.Revision < 0 || c.Expected.Revision == math.MaxInt64 || !schedulePosition(c.Expected.AfterID) || !schedulePosition(c.NextAfterID) || len(c.Writes) > 200 || (c.Expected.Revision == 0 && c.Expected.AfterID != "") {
		return recordstore.ErrInvalid
	}
	rows := make([]recordstore.Record, 0, len(c.Writes))
	expected := make([]int64, 0, len(c.Writes))
	seen := map[string]bool{}
	for _, w := range c.Writes {
		row, err := jobRecord(w.Job)
		if err != nil {
			return err
		}
		if w.ExpectedRevision < 0 || w.ExpectedRevision == math.MaxInt64 || row.Revision != w.ExpectedRevision+1 || w.Job.Source.Scope != c.Scope || (w.ExpectedRevision == 0 && w.Job.Lane != c.Lane) || seen[row.ID] {
			return recordstore.ErrInvalid
		}
		// This bootstrap/scan primitive is not an execution fence. Once any
		// execution has started, writes must use the exact lease predicates.
		if w.Job.LastSupersessionID != "" || w.Job.LastCompletionID != "" || !w.Job.CadenceAnchor.IsZero() || w.Job.Fence != 0 || w.Job.Attempts != 0 || !w.Job.LeasedUntil.IsZero() || w.Job.LeaseActor != "" || w.Job.LeaseToken != "" {
			return recordstore.ErrInvalid
		}
		seen[row.ID] = true
		rows = append(rows, row)
		expected = append(expected, w.ExpectedRevision)
	}
	return r.store.Transact(ctx, scheduleCursorKind+":"+scheduleCursorID(c.Scope, c.Lane), func(tx recordstore.Tx) error {
		old, err := readCursor(ctx, tx, c.Scope, c.Lane)
		if err != nil {
			return err
		}
		if old != c.Expected {
			return recordstore.ErrConflict
		}
		for n, row := range rows {
			previousRevision := expected[n]
			if previousRevision == 0 {
				if err := tx.Insert(ctx, row); err != nil {
					return err
				}
				continue
			}
			stored, err := tx.Get(ctx, scheduleJobKind, row.ID)
			if err != nil {
				return err
			}
			prior, err := readJob(stored)
			if err != nil {
				return err
			}
			var next scheduleJobPayload
			if strictScheduleDecode(row.Data, &next) != nil {
				return recordstore.ErrUnavailable
			}
			if prior.Revision != previousRevision || prior.Lane != c.Lane || digest(scheduleSource(prior.Source)) != digest(next.Source) || !prior.CreatedAt.Equal(next.CreatedAt) {
				return recordstore.ErrConflict
			}
			if prior.LastSupersessionID != "" || prior.LastCompletionID != "" || !prior.CadenceAnchor.IsZero() || prior.Fence != 0 || prior.Attempts != 0 || !prior.LeasedUntil.IsZero() || prior.LeaseActor != "" || prior.LeaseToken != "" {
				return recordstore.ErrConflict
			}
			if err := tx.Replace(ctx, row, previousRevision); err != nil {
				return err
			}
		}
		cursor, err := recordstore.NewRecord(scheduleCursorKind, scheduleCursorID(c.Scope, c.Lane), schedulePartition(c.Scope, c.Lane), old.Revision+1, scheduleCursorPayload{1, c.Scope, c.Lane, c.NextAfterID})
		if err != nil {
			return err
		}
		if old.Revision == 0 {
			return tx.Insert(ctx, cursor)
		}
		return tx.Replace(ctx, cursor, old.Revision)
	})
}
