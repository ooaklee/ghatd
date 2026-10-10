package billinglifecycle

import (
	"context"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Source admission, service identity and provider responses are controlled.
// Billing, manager authority and encrypted Mongo transactions are real. These
// package flows do not certify installed runtime or authenticated platform E2E.
func nativeOriginalResolverFixture(t *testing.T, state string, evidence bool) (*statusNativeFixture, *RecordExecutionRepository, *executionTestClock, *StatusOriginalResolver, context.Context, ScheduledJob, StatusInput) {
	t.Helper()
	f, r, clock, j := nativeStatusBindingFixture(t)
	b, ctx := nativeStatusBinder(t, f, r, clock, "worker-original")
	input := StatusInput{Preparation: f.p}
	bound, err := b.Bind(ctx, JobLease(j), input)
	require.NoError(t, err)
	j = bound.Job
	if evidence {
		e := f.evidence(t)
		input.Evidence = &e
		bound, err = b.Bind(ctx, JobLease(j), input)
		require.NoError(t, err)
		j = bound.Job
	}
	if state == billing.SubscriptionStatusCaptured {
		require.NotNil(t, input.Evidence)
		_, err = b.validator.(*billingmanager.Service).CaptureSubscriptionStatus(ctx, "worker-original", f.p, *input.Evidence)
		require.NoError(t, err)
	}
	if state == billing.SubscriptionStatusSuperseded {
		f.owner, err = newExecutionStatusOwner(t, f)
		require.NoError(t, err)
		clock.set(f.base.clock.at.Add(time.Nanosecond))
		b, ctx = nativeStatusBinder(t, f, r, clock, "worker-original")
		m := b.validator.(*billingmanager.Service)
		next, e := m.PrepareSubscriptionStatusForCheckout(ctx, "worker-original", f.p.Scope, f.p.SubscriptionID)
		require.NoError(t, e)
		require.NotEqual(t, f.p.CaptureID, next.CaptureID)
		ev, e := m.LookupSubscriptionStatus(ctx, "worker-original", next)
		require.NoError(t, e)
		_, e = m.CaptureSubscriptionStatus(ctx, "worker-original", next, ev)
		require.NoError(t, e)
	}
	box, err := NewStatusOutbox(f.base.reply, b.validator)
	require.NoError(t, err)
	x, err := NewStatusExecution(b.scheduler, r, box)
	require.NoError(t, err)
	resolver, err := NewStatusOriginalResolver(x, r)
	require.NoError(t, err)
	return f, r, clock, resolver, ctx, j, input
}

type supersessionReplyBoundary struct {
	StatusSupersessionRepository
	store     *inputLostReplyStore
	lose      bool
	after     func()
	afterRead func()
}

func (r supersessionReplyBoundary) SupersedeStatus(ctx context.Context, q StatusSupersessionRequest) (StatusSupersession, error) {
	r.store.lose = r.lose
	out, err := r.StatusSupersessionRepository.SupersedeStatus(ctx, q)
	r.store.lose = false
	if r.after != nil {
		r.after()
	}
	return out, err
}
func (r supersessionReplyBoundary) FindStatusSupersession(ctx context.Context, k StatusSupersessionKey) (StatusSupersession, error) {
	out, err := r.StatusSupersessionRepository.FindStatusSupersession(ctx, k)
	if r.afterRead != nil {
		r.afterRead()
	}
	return out, err
}

func TestStatusOriginalResolverNativeOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		evidence    bool
	}{
		{"pending_preparation", billing.SubscriptionStatusPending, false},
		{"pending_evidence", billing.SubscriptionStatusPending, true},
		{"captured_evidence", billing.SubscriptionStatusCaptured, true},
		{"superseded_preparation", billing.SubscriptionStatusSuperseded, false},
		{"superseded_evidence", billing.SubscriptionStatusSuperseded, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r, _, s, ctx, j, input := nativeOriginalResolverFixture(t, tc.state, tc.evidence)
			gets := f.provider.calls
			out, err := s.Resolve(ctx, JobLease(j))
			require.NoError(t, err)
			require.Equal(t, tc.state, out.State)
			require.Equal(t, gets, f.provider.calls, "resolution must not look up the provider")
			stored, err := r.ReadJob(ctx, j.Source)
			require.NoError(t, err)
			require.True(t, sameExecutionJob(out.Job, stored))
			if tc.state != billing.SubscriptionStatusSuperseded {
				require.True(t, sameExecutionJob(j, stored))
				require.Nil(t, out.Supersession)
				if tc.state == billing.SubscriptionStatusCaptured {
					require.NotNil(t, out.Observation)
					require.True(t, statusObservationShape(*out.Observation))
				} else {
					require.Nil(t, out.Observation)
				}
				return
			}
			require.NotNil(t, out.Supersession)
			expected := j
			expected.Revision++
			expected.OriginalStatus = nil
			expected.LastSupersessionID, _ = statusSupersessionIdentity(StatusSupersessionKey{j.Source, input.Preparation.CaptureID})
			require.True(t, sameExecutionJob(expected, stored), "only pointer, history reference and revision change")
			old, err := s.execution.outbox.Find(ctx, "worker-original", input.Preparation)
			require.NoError(t, err)
			require.True(t, sameRetainedStatus(input, old))
			history, err := s.InspectLast(ctx, j.Source)
			require.NoError(t, err)
			require.True(t, sameStatusSupersession(*out.Supersession, history))
			_, err = s.Resolve(ctx, JobLease(j))
			require.ErrorIs(t, err, recordstore.ErrConflict, "consumed lease revision cannot be blindly replayed")
		})
	}
}

func TestStatusOriginalResolverPostCommitRecovery(t *testing.T) {
	for _, tc := range []struct {
		name string
		lose bool
		late string
		want error
	}{
		{"lost_reply", true, "", recordstore.ErrUncertain},
		{"known_then_denied", false, "denied", partnermanager.ErrDenied},
		{"unknown_then_denied", true, "denied", recordstore.ErrUncertain},
		{"known_then_canceled", false, "canceled", context.Canceled},
		{"unknown_then_canceled", true, "canceled", recordstore.ErrUncertain},
		{"known_then_expired", false, "expired", recordstore.ErrConflict},
		{"unknown_then_expired", true, "expired", recordstore.ErrUncertain},
		{"final_history_read_then_denied", false, "read_denied", partnermanager.ErrDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r, clock, s, ctx, j, _ := nativeOriginalResolverFixture(t, billing.SubscriptionStatusSuperseded, true)
			ctx, cancel := context.WithCancel(ctx)
			t.Cleanup(cancel)
			boundary := supersessionReplyBoundary{StatusSupersessionRepository: r, store: f.base.reply, lose: tc.lose}
			boundary.after = func() {
				switch tc.late {
				case "denied":
					f.base.revoke(t)
				case "canceled":
					cancel()
				case "expired":
					clock.set(j.LeasedUntil)
				}
			}
			if tc.late == "read_denied" {
				boundary.afterRead = func() { f.base.revoke(t) }
			}
			s.repo = boundary
			gets := f.provider.calls
			out, err := s.Resolve(ctx, JobLease(j))
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, StatusOriginalOutcome{}, out)
			if tc.late == "denied" {
				require.ErrorIs(t, err, partnermanager.ErrDenied)
			}
			if tc.late == "canceled" {
				require.ErrorIs(t, err, context.Canceled)
			}
			if !tc.lose {
				require.NotErrorIs(t, err, recordstore.ErrUncertain)
			}
			inspect := context.WithoutCancel(ctx)
			actual, e := r.FindLastStatusSupersession(inspect, j.Source)
			require.NoError(t, e, "failed disclosure does not undo committed history")
			require.Equal(t, j.Revision+1, actual.Job.Revision)
			if tc.late == "denied" || tc.late == "read_denied" {
				f.base.restore(t)
			}
			history, e := s.InspectLast(inspect, j.Source)
			require.NoError(t, e)
			require.True(t, sameStatusSupersession(actual, history))
			require.Equal(t, gets, f.provider.calls, "durable recovery performs no provider GET")
		})
	}
}

func TestStatusOriginalResolverMissingInputs(t *testing.T) {
	for _, tc := range []struct {
		name     string
		captured bool
	}{
		{"missing_bound_input", false}, {"captured_missing_bound_evidence", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := billing.SubscriptionStatusPending
			if tc.captured {
				state = billing.SubscriptionStatusCaptured
			}
			f, r, _, s, ctx, j, input := nativeOriginalResolverFixture(t, state, tc.captured)
			if tc.captured {
				// Replace only the private fixture input with its originally valid prepared
				// stage. Native capture survives; it must not repair missing evidence.
				row, err := encodeStatus(StatusInput{Preparation: input.Preparation})
				require.NoError(t, err)
				_, err = f.base.db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": row.Kind, "id": row.ID})
				require.NoError(t, err)
				require.NoError(t, f.base.reply.Transact(ctx, "fixture-missing-evidence", func(tx recordstore.Tx) error { return tx.Insert(ctx, row) }))
			} else {
				id, _ := statusIdentity(input.Preparation)
				_, err := f.base.db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": statusKind, "id": id})
				require.NoError(t, err)
			}
			gets := f.provider.calls
			out, err := s.Resolve(ctx, JobLease(j))
			require.Error(t, err)
			require.Equal(t, StatusOriginalOutcome{}, out)
			stored, err := r.ReadJob(ctx, j.Source)
			require.NoError(t, err)
			require.True(t, sameExecutionJob(j, stored))
			require.Equal(t, gets, f.provider.calls)
		})
	}
}
