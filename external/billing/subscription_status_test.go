package billing

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Audit disposition: named lifecycle/boundary cases with a fresh mutable owning
// fixture per case. Native transaction, encryption and CAS tests live in revenuestore.
type statusTestClock struct{ at time.Time }

func (c *statusTestClock) Now() time.Time { return c.at }

type statusTestRepo struct {
	*revenueTestRepo
	statusMu sync.Mutex
	heads    map[string]SubscriptionStatus
	captures map[string]SubscriptionStatus
	readErr  error
}
type statusTestTx struct {
	heads, captures map[string]SubscriptionStatus
	scope           RevenueScope
	sub, key        string
}

func (r *statusTestRepo) ReadSubscriptionStatus(ctx context.Context, scope RevenueScope, sub string) (SubscriptionStatusSnapshot, error) {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	if r.readErr != nil {
		return SubscriptionStatusSnapshot{}, r.readErr
	}
	return (&statusTestTx{heads: r.heads, captures: r.captures, scope: scope, sub: sub, key: SubscriptionStatusIdentity(scope, sub)}).GetCurrent(ctx)
}
func (r *statusTestRepo) WithSubscriptionStatusTransaction(ctx context.Context, scope RevenueScope, sub string, fn func(SubscriptionStatusTx) error) error {
	r.statusMu.Lock()
	defer r.statusMu.Unlock()
	tx := &statusTestTx{heads: maps.Clone(r.heads), captures: maps.Clone(r.captures), scope: scope, sub: sub, key: SubscriptionStatusIdentity(scope, sub)}
	if err := fn(tx); err != nil {
		return err
	}
	r.heads, r.captures = tx.heads, tx.captures
	return nil
}
func (t *statusTestTx) GetCapture(_ context.Context, id string) (SubscriptionStatus, error) {
	v, ok := t.captures[id]
	if !ok {
		return SubscriptionStatus{}, ErrRevenueNotFound
	}
	return v, nil
}
func (t *statusTestTx) GetCurrent(ctx context.Context) (SubscriptionStatusSnapshot, error) {
	v, ok := t.heads[t.key]
	if !ok {
		return SubscriptionStatusSnapshot{}, ErrRevenueNotFound
	}
	r, err := t.GetCapture(ctx, v.Preparation.CaptureID)
	if err != nil {
		return SubscriptionStatusSnapshot{}, ErrRevenueUnavailable
	}
	return SubscriptionStatusSnapshot{Current: v, Receipt: r}, nil
}
func (t *statusTestTx) InsertCapture(_ context.Context, v SubscriptionStatus) error {
	if _, ok := t.captures[v.Preparation.CaptureID]; ok {
		return ErrRevenueConflict
	}
	t.captures[v.Preparation.CaptureID] = v
	return nil
}
func (t *statusTestTx) PutCurrent(_ context.Context, v SubscriptionStatus, expected int64) error {
	if t.heads[t.key].Revision != expected {
		return ErrRevenueConflict
	}
	t.heads[t.key] = v
	return nil
}
func statusFixture(t *testing.T) (*RevenueService, *statusTestRepo, *statusTestClock, RevenueFact) {
	t.Helper()
	_, base, request := revenueFixture(t)
	request.Facts[0].ProviderCustomerID = "customer_fixture"
	r := &statusTestRepo{revenueTestRepo: base, heads: map[string]SubscriptionStatus{}, captures: map[string]SubscriptionStatus{}}
	c := &statusTestClock{request.Facts[0].EffectiveAt.Add(time.Minute)}
	s, err := NewRevenueService(r, c)
	require.NoError(t, err)
	o, err := s.AcceptVerified(context.Background(), request)
	require.NoError(t, err)
	f, err := s.GetRevenueFact(context.Background(), o.FactIDs[0])
	require.NoError(t, err)
	return s, r, c, f
}
func statusEvidence(p SubscriptionStatusPreparation, state string) VerifiedSubscriptionStatusEvidence {
	return VerifiedSubscriptionStatusEvidence{Scope: p.Scope, SubscriptionID: p.SubscriptionID, ProviderCustomerID: p.ProviderCustomerID, Status: state}
}
func TestSubscriptionStatusStates(t *testing.T) {
	type testCase struct {
		name, state string
		scheduled   bool
		want        error
	}
	cases := []testCase{
		{name: "active", state: "active"},
		{name: "active_scheduled_cancellation", state: "active", scheduled: true},
		{name: "trialing", state: "trialing"},
		{name: "incomplete", state: "incomplete"},
		{name: "incomplete_expired", state: "incomplete_expired"},
		{name: "past_due", state: "past_due"},
		{name: "unpaid", state: "unpaid"},
		{name: "canceled", state: "canceled"},
		{name: "paused", state: "paused"},
		{name: "unknown_is_not_inactive", state: "unknown", want: ErrRevenueConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, r, _, f := statusFixture(t)
			p, err := s.PrepareSubscriptionStatus(context.Background(), "operator", f.ID)
			require.NoError(t, err)
			e := statusEvidence(p, tc.state)
			e.CancellationScheduled = tc.scheduled
			v, err := s.CaptureVerifiedSubscriptionStatus(context.Background(), p, e)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, v)
				require.Empty(t, r.captures)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.state, v.Status)
			require.Equal(t, tc.scheduled, v.CancellationScheduled)
			current, err := s.GetSubscriptionStatusForFact(context.Background(), f.ID, time.Minute)
			require.NoError(t, err)
			require.Equal(t, v, current)
			for _, value := range []any{p, e, v, SubscriptionStatusSnapshot{Current: v, Receipt: v}} {
				data, err := json.Marshal(value)
				require.NoError(t, err)
				require.JSONEq(t, `{}`, string(data))
			}
			require.EqualValues(t, 1, r.sequence, "status must not create another economic fact")
		})
	}
}
func TestSubscriptionStatusOriginalReceiptRecovery(t *testing.T) {
	type testCase struct {
		name                                               string
		changeState, changeCancellation, changePreparation bool
		want                                               error
	}
	cases := []testCase{
		{name: "original_receipt_survives_later_head_and_zero_clock"},
		{name: "changed_state_conflicts", changeState: true, want: ErrRevenueConflict},
		{name: "changed_cancellation_conflicts", changeCancellation: true, want: ErrRevenueConflict},
		{name: "changed_authorship_is_not_same_capture", changePreparation: true, want: ErrRevenueConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, r, c, f := statusFixture(t)
			ctx := context.Background()
			p, err := s.PrepareSubscriptionStatus(ctx, "original-author", f.ID)
			require.NoError(t, err)
			e := statusEvidence(p, "active")
			first, err := s.CaptureVerifiedSubscriptionStatus(ctx, p, e)
			require.NoError(t, err)
			c.at = c.at.Add(time.Second)
			next, err := s.PrepareSubscriptionStatus(ctx, "another-author", f.ID)
			require.NoError(t, err)
			require.Equal(t, first.Fingerprint, next.ExpectedFingerprint)
			later, err := s.CaptureVerifiedSubscriptionStatus(ctx, next, statusEvidence(next, "canceled"))
			require.NoError(t, err)
			c.at = time.Time{}
			if tc.changeState {
				e.Status = "paused"
			}
			if tc.changeCancellation {
				e.CancellationScheduled = true
			}
			if tc.changePreparation {
				p.ActorID = "replacement-author"
			}
			recovered, err := s.CaptureVerifiedSubscriptionStatus(ctx, p, e)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, recovered)
			} else {
				require.NoError(t, err)
				require.Equal(t, first, recovered)
			}
			require.Equal(t, later, r.heads[SubscriptionStatusIdentity(f.Scope, f.SubscriptionID)])
			require.Len(t, r.captures, 2)
		})
	}
}
func TestSubscriptionStatusFreshness(t *testing.T) {
	type testCase struct {
		name                       string
		age, limit, lookupDuration time.Duration
		rollback                   bool
		want                       error
	}
	cases := []testCase{
		{name: "fresh_at_exact_boundary", age: time.Minute, limit: time.Minute},
		{name: "stale_one_nanosecond_after_boundary", age: time.Minute + time.Nanosecond, limit: time.Minute, want: ErrSubscriptionStatusStale},
		{name: "slow_lookup_age_starts_before_provider_io", age: time.Minute, lookupDuration: 30 * time.Second, limit: time.Minute},
		{name: "lookup_itself_can_consume_coverage", age: time.Minute + time.Nanosecond, lookupDuration: time.Minute, limit: time.Minute, want: ErrSubscriptionStatusStale},
		{name: "zero_limit_rejected", want: ErrRevenueInvalid},
		{name: "minimum_limit", age: time.Second, limit: time.Second},
		{name: "maximum_limit_exact_boundary", age: 24 * time.Hour, limit: 24 * time.Hour},
		{name: "over_ceiling", limit: 24*time.Hour + time.Nanosecond, want: ErrRevenueInvalid},
		{name: "clock_rollback_is_unavailable", rollback: true, limit: time.Minute, want: ErrRevenueUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, c, f := statusFixture(t)
			ctx := context.Background()
			start := c.at
			p, err := s.PrepareSubscriptionStatus(ctx, "operator", f.ID)
			require.NoError(t, err)
			c.at = start.Add(tc.lookupDuration)
			_, err = s.CaptureVerifiedSubscriptionStatus(ctx, p, statusEvidence(p, "active"))
			require.NoError(t, err)
			c.at = start.Add(tc.age)
			if tc.rollback {
				c.at = start.Add(-time.Nanosecond)
			}
			v, err := s.GetSubscriptionStatusForFact(ctx, f.ID, tc.limit)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, v)
			} else {
				require.NoError(t, err)
				require.Equal(t, "active", v.Status)
			}
		})
	}
}
func TestSubscriptionStatusOwningProvenance(t *testing.T) {
	type testCase struct {
		name               string
		kind, customer     string
		corruptFingerprint bool
		readErr, want      error
	}
	cases := []testCase{
		{name: "legacy_customer_missing", want: ErrRevenueUnassessable},
		{name: "refund_cannot_establish_status", kind: RevenueRefund, customer: "customer_fixture", want: ErrRevenueUnassessable},
		{name: "dispute_cannot_establish_status", kind: RevenueDisputeHold, customer: "customer_fixture", want: ErrRevenueUnassessable},
		{name: "changed_source_fingerprint", customer: "customer_fixture", corruptFingerprint: true, want: ErrRevenueUnassessable},
		{name: "joined_absence_and_outage_not_first_head", customer: "customer_fixture", readErr: errors.Join(ErrRevenueNotFound, ErrRevenueUnavailable), want: ErrRevenueUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, r, _, f := statusFixture(t)
			f.ProviderCustomerID = tc.customer
			if tc.kind != "" {
				f.Kind = tc.kind
			}
			if tc.corruptFingerprint {
				f.Fingerprint = "changed"
			}
			r.facts[f.ID] = f
			r.readErr = tc.readErr
			p, err := s.PrepareSubscriptionStatus(context.Background(), "operator", f.ID)
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, p)
			require.Empty(t, r.captures)
		})
	}
}
func TestSubscriptionStatusBindingAndCAS(t *testing.T) {
	type testCase struct {
		name                          string
		otherPrincipal, otherCustomer bool
		lateResponse                  bool
	}
	cases := []testCase{
		{name: "principal_cannot_be_rebound", otherPrincipal: true},
		{name: "customer_cannot_be_rebound", otherCustomer: true},
		{name: "late_first_response_cannot_overwrite_new_head", lateResponse: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, r, c, f := statusFixture(t)
			ctx := context.Background()
			old, err := s.PrepareSubscriptionStatus(ctx, "slow-worker", f.ID)
			require.NoError(t, err)
			p, err := s.PrepareSubscriptionStatus(ctx, "fast-worker", f.ID)
			require.NoError(t, err)
			c.at = c.at.Add(time.Second)
			first, err := s.CaptureVerifiedSubscriptionStatus(ctx, p, statusEvidence(p, "active"))
			require.NoError(t, err)
			if tc.lateResponse {
				v, err := s.CaptureVerifiedSubscriptionStatus(ctx, old, statusEvidence(old, "canceled"))
				require.ErrorIs(t, err, ErrRevenueConflict)
				require.Zero(t, v)
			} else {
				f.ID = ""
				f.Sequence = 0
				f.AcceptedAt = time.Time{}
				f.Fingerprint = ""
				f.PaymentID = "renewal"
				if tc.otherPrincipal {
					f.PrincipalID = "different-principal"
				}
				if tc.otherCustomer {
					f.ProviderCustomerID = "different-customer"
				}
				o, err := s.AcceptVerified(ctx, VerifiedRevenueRequest{Scope: f.Scope, EnvelopeID: "renewal", Facts: []RevenueFact{f}})
				require.NoError(t, err)
				p, err := s.PrepareSubscriptionStatus(ctx, "operator", o.FactIDs[0])
				require.ErrorIs(t, err, ErrRevenueConflict)
				require.Zero(t, p)
			}
			require.Equal(t, first, r.heads[SubscriptionStatusIdentity(f.Scope, f.SubscriptionID)])
			require.Len(t, r.captures, 1)
		})
	}
}

func TestSubscriptionStatusInvalidPreparationAndClock(t *testing.T) {
	type testCase struct {
		name                 string
		field                string
		capture, unsupported bool
		want                 error
	}
	cases := []testCase{
		{name: "optional_repository_missing", unsupported: true, want: ErrRevenueUnavailable},
		{name: "zero_preparation_clock", field: "clock", want: ErrRevenueInvalid},
		{name: "preparation_before_accepted_fact", field: "before_fact", want: ErrRevenueInvalid},
		{name: "changed_capture_identity", field: "capture", capture: true, want: ErrRevenueConflict},
		{name: "changed_original_scope", field: "scope", capture: true, want: ErrRevenueConflict},
		{name: "changed_original_fingerprint", field: "fingerprint", capture: true, want: ErrRevenueConflict},
		{name: "missing_prior_head_fingerprint", field: "head", capture: true, want: ErrRevenueConflict},
		{name: "zero_capture_clock", field: "clock", capture: true, want: ErrRevenueInvalid},
		{name: "capture_before_request", field: "before_fact", capture: true, want: ErrRevenueInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, r, c, f := statusFixture(t)
			ctx := context.Background()
			if tc.unsupported {
				s.repo = r.revenueTestRepo
				p, err := s.PrepareSubscriptionStatus(ctx, "operator", f.ID)
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, p)
				return
			}
			if !tc.capture {
				if tc.field == "clock" {
					c.at = time.Time{}
				} else {
					c.at = f.AcceptedAt.Add(-time.Nanosecond)
				}
				p, err := s.PrepareSubscriptionStatus(ctx, "operator", f.ID)
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, p)
				return
			}
			p, err := s.PrepareSubscriptionStatus(ctx, "operator", f.ID)
			require.NoError(t, err)
			e := statusEvidence(p, "active")
			switch tc.field {
			case "capture":
				p.CaptureID = "changed"
			case "scope":
				p.Scope.AccountID = "other"
			case "fingerprint":
				p.FactFingerprint = "changed"
			case "head":
				p.ExpectedRevision = 1
			case "clock":
				c.at = time.Time{}
			case "before_fact":
				c.at = p.RequestedAt.Add(-time.Nanosecond)
			}
			v, err := s.CaptureVerifiedSubscriptionStatus(ctx, p, e)
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, v)
			require.Empty(t, r.captures)
		})
	}
}
