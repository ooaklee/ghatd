package billinglifecycle

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

var _ StatusSupersessionRepository = (*RecordExecutionRepository)(nil)

func TestStatusSupersessionEncryptedFences(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"acknowledged", nil}, {"stale_revision", recordstore.ErrConflict},
		{"wrong_actor", recordstore.ErrConflict}, {"wrong_token", recordstore.ErrConflict},
		{"wrong_fence", recordstore.ErrConflict}, {"wrong_lane", recordstore.ErrConflict},
		{"wrong_expiry", recordstore.ErrConflict}, {"expired", recordstore.ErrConflict},
		{"expires_before_job_cas", recordstore.ErrConflict}, {"clock_moves_backwards", recordstore.ErrConflict},
		{"missing_bound_original", recordstore.ErrUnavailable}, {"retained_evidence_changed", recordstore.ErrConflict},
		{"pending_is_not_supersession", recordstore.ErrInvalid}, {"native_head_wrong_owner", recordstore.ErrInvalid},
		{"lost_reply", recordstore.ErrUncertain}, {"canceled", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r, clock, s, ctx, j, input := nativeOriginalResolverFixture(t, billing.SubscriptionStatusSuperseded, true)
			resolution, err := s.owner.ResolveSubscriptionStatus(ctx, "worker-original", input.Preparation)
			require.NoError(t, err)
			q := StatusSupersessionRequest{j, input, resolution}
			switch tc.name {
			case "stale_revision":
				q.Job.Revision++
			case "wrong_actor":
				q.Job.LeaseActor = "other-worker"
			case "wrong_token":
				q.Job.LeaseToken = "other-token"
			case "wrong_fence":
				q.Job.Fence++
			case "wrong_lane":
				q.Job.Lane = RefreshLane
			case "wrong_expiry":
				q.Job.LeasedUntil = q.Job.LeasedUntil.Add(time.Second)
			case "expired":
				clock.set(j.LeasedUntil)
			case "expires_before_job_cas":
				clock.sequence = []time.Time{clock.at, j.LeasedUntil}
			case "clock_moves_backwards":
				clock.sequence = []time.Time{clock.at, clock.at.Add(-time.Nanosecond)}
			case "missing_bound_original":
				id, _ := statusIdentity(input.Preparation)
				_, err = f.base.db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": statusKind, "id": id})
				require.NoError(t, err)
			case "retained_evidence_changed":
				e := *input.Evidence
				e.Status = "active"
				q.Input.Evidence = &e
			case "pending_is_not_supersession":
				q.Resolution = billing.SubscriptionStatusResolution{Preparation: input.Preparation, State: billing.SubscriptionStatusPending}
			case "native_head_wrong_owner":
				c := *resolution.Current
				c.Preparation.PrincipalID = "other-owner"
				q.Resolution.Current = &c
			case "lost_reply":
				f.base.reply.lose = true
			case "canceled":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			}
			gets := f.provider.calls
			out, err := r.SupersedeStatus(ctx, q)
			f.base.reply.lose = false
			clock.sequence = nil
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, StatusSupersession{}, out)
			} else {
				require.NoError(t, err)
				require.True(t, supersessionShape(out, StatusSupersessionKey{j.Source, input.Preparation.CaptureID}))
			}
			inspect := context.WithoutCancel(ctx)
			stored, e := r.ReadJob(inspect, j.Source)
			require.NoError(t, e)
			count, e := f.base.db.Collection("ghatd_owned_records").CountDocuments(inspect, bson.M{"kind": statusSupersessionKind})
			require.NoError(t, e)
			if tc.want == nil || tc.name == "lost_reply" {
				require.EqualValues(t, 1, count)
				require.Equal(t, j.Revision+1, stored.Revision)
				require.Nil(t, stored.OriginalStatus)
				require.Equal(t, j.Attempts, stored.Attempts)
				require.Equal(t, j.Fence, stored.Fence)
				require.Equal(t, j.LeasedUntil, stored.LeasedUntil)
				history, e := r.FindLastStatusSupersession(inspect, j.Source)
				require.NoError(t, e)
				require.True(t, sameRetainedStatus(input, history.Input))
			} else {
				require.Zero(t, count, "failed transaction must not leave a history row")
				require.True(t, sameExecutionJob(j, stored), "failed transaction must conserve attached original")
			}
			require.Equal(t, gets, f.provider.calls)
		})
	}
}

func TestStatusSupersessionEncryptedCompetition(t *testing.T) {
	for _, evidence := range []bool{false, true} {
		name := "prepared"
		if evidence {
			name = "evidence"
		}
		t.Run(name, func(t *testing.T) {
			f, r, _, s, ctx, j, input := nativeOriginalResolverFixture(t, billing.SubscriptionStatusSuperseded, evidence)
			native, err := s.owner.ResolveSubscriptionStatus(ctx, "worker-original", input.Preparation)
			require.NoError(t, err)
			q := StatusSupersessionRequest{j, input, native}
			results := make(chan error, 2)
			var wg sync.WaitGroup
			for i := 0; i < 2; i++ {
				wg.Add(1)
				go func() { defer wg.Done(); _, e := r.SupersedeStatus(ctx, q); results <- e }()
			}
			wg.Wait()
			close(results)
			success, conflict := 0, 0
			for e := range results {
				if e == nil {
					success++
				} else {
					require.ErrorIs(t, e, recordstore.ErrConflict)
					conflict++
				}
			}
			require.Equal(t, 1, success)
			require.Equal(t, 1, conflict)
			count, e := f.base.db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": statusSupersessionKind})
			require.NoError(t, e)
			require.EqualValues(t, 1, count)
		})
	}
}

func TestStatusSupersessionHistoricalInspection(t *testing.T) {
	for _, scenario := range []string{"lease_replacement", "later_native_head", "missing_referenced_history"} {
		t.Run(scenario, func(t *testing.T) {
			f, r, clock, s, ctx, j, _ := nativeOriginalResolverFixture(t, billing.SubscriptionStatusSuperseded, false)
			out, err := s.Resolve(ctx, JobLease(j))
			require.NoError(t, err)
			switch scenario {
			case "lease_replacement":
				clock.set(j.LeasedUntil)
				next, e := r.Acquire(ctx, LeaseRequest{Source: j.Source, ExpectedRevision: out.Job.Revision, Actor: "worker-original", Token: "replacement", Until: clock.at.Add(time.Minute)})
				require.NoError(t, e)
				require.Equal(t, out.Job.LastSupersessionID, next.LastSupersessionID)
				require.Equal(t, out.Job.Attempts+1, next.Attempts)
				require.Equal(t, out.Job.Fence+1, next.Fence)
			case "later_native_head":
				p, e := s.execution.owner.PrepareSubscriptionStatusForCheckout(ctx, "worker-original", f.p.Scope, f.p.SubscriptionID)
				require.NoError(t, e)
				require.Equal(t, out.Supersession.Resolution.Current.Revision, p.ExpectedRevision)
				ev, e := s.execution.owner.LookupSubscriptionStatus(ctx, "worker-original", p)
				require.NoError(t, e)
				_, e = s.execution.owner.CaptureSubscriptionStatus(ctx, "worker-original", p, ev)
				require.NoError(t, e)
			case "missing_referenced_history":
				_, e := f.base.db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": statusSupersessionKind, "id": out.Job.LastSupersessionID})
				require.NoError(t, e)
			}
			gets := f.provider.calls
			history, e := s.InspectLast(ctx, j.Source)
			if scenario == "missing_referenced_history" {
				require.ErrorIs(t, e, recordstore.ErrUnavailable)
				require.Equal(t, StatusSupersession{}, history)
			} else {
				require.NoError(t, e)
				require.True(t, sameStatusSupersession(*out.Supersession, history))
			}
			require.Equal(t, gets, f.provider.calls)
		})
	}
}

func TestStatusSupersessionPrivateCodec(t *testing.T) {
	for _, scenario := range []string{"round_trip", "wrong_kind", "wrong_id", "wrong_partition", "mutable_revision", "unexpected_sequence", "expiring_history", "unknown_schema", "unknown_payload_field", "changed_native_owner", "missing_job_reference"} {
		t.Run(scenario, func(t *testing.T) {
			_, _, _, s, ctx, j, _ := nativeOriginalResolverFixture(t, billing.SubscriptionStatusSuperseded, true)
			out, err := s.Resolve(ctx, JobLease(j))
			require.NoError(t, err)
			history := *out.Supersession
			row, err := encodeStatusSupersession(history)
			require.NoError(t, err)
			key := StatusSupersessionKey{j.Source, history.Input.Preparation.CaptureID}
			switch scenario {
			case "wrong_kind":
				row.Kind = "other"
			case "wrong_id":
				row.ID = "other"
			case "wrong_partition":
				row.Partition = "other"
			case "mutable_revision":
				row.Revision++
			case "unexpected_sequence":
				row.Sequence = 1
			case "expiring_history":
				at := history.RecordedAt.Add(time.Hour)
				row.ExpiresAt = &at
			case "unknown_schema", "unknown_payload_field", "changed_native_owner", "missing_job_reference":
				var payload map[string]any
				require.NoError(t, json.Unmarshal(row.Data, &payload))
				switch scenario {
				case "unknown_schema":
					payload["Schema"] = 2
				case "unknown_payload_field":
					payload["UnexpectedAuthority"] = true
				case "changed_native_owner":
					payload["CurrentPreparation"].(map[string]any)["PrincipalID"] = "other"
				case "missing_job_reference":
					payload["Job"].(map[string]any)["LastSupersessionID"] = ""
				}
				row.Data, err = json.Marshal(payload)
				require.NoError(t, err)
			}
			decoded, err := decodeStatusSupersession(row, key)
			if scenario == "round_trip" {
				require.NoError(t, err)
				require.True(t, sameStatusSupersession(history, decoded))
			} else {
				require.ErrorIs(t, err, recordstore.ErrUnavailable)
				require.Equal(t, StatusSupersession{}, decoded)
			}
		})
	}
}

func TestStatusSupersessionNextConfirmedCycle(t *testing.T) {
	for _, retry := range []bool{false, true} {
		name := "same_lease"
		if retry {
			name = "retry_new_lease"
		}
		t.Run(name, func(t *testing.T) {
			f, r, clock, s, ctx, j, _ := nativeOriginalResolverFixture(t, billing.SubscriptionStatusSuperseded, true)
			out, err := s.Resolve(ctx, JobLease(j))
			require.NoError(t, err)
			current := out.Job
			if retry {
				release, e := r.Release(ctx, LeaseDisposition{Handle: JobLease(current), NextAttemptAt: clock.at.Add(time.Second), Lane: current.Lane})
				require.NoError(t, e)
				require.Equal(t, current.LastSupersessionID, release.LastSupersessionID)
				clock.set(release.NextAttemptAt)
				current, e = r.Acquire(ctx, LeaseRequest{Source: release.Source, ExpectedRevision: release.Revision, Actor: "worker-original", Token: "after-supersession", Until: clock.at.Add(time.Minute)})
				require.NoError(t, e)
				require.Equal(t, out.Job.Attempts+1, current.Attempts)
				require.Equal(t, out.Job.LastSupersessionID, current.LastSupersessionID)
			}
			observation, err := s.execution.Observe(ctx, JobLease(current))
			require.NoError(t, err)
			require.Equal(t, out.Job.LastSupersessionID, observation.Job.LastSupersessionID)
			require.NotEqual(t, j.OriginalStatus.CaptureID, observation.Input.Preparation.CaptureID)
			require.Equal(t, current.Attempts, observation.Job.Attempts)
			complete, err := NewStatusCompletion(s.execution, r, 30*time.Second)
			require.NoError(t, err)
			finished, err := complete.Complete(ctx, observation)
			require.NoError(t, err)
			require.Equal(t, out.Job.LastSupersessionID, finished.Job.LastSupersessionID)
			require.Equal(t, RefreshLane, finished.Job.Lane)
			require.Nil(t, finished.Job.OriginalStatus)
			require.Zero(t, finished.Job.Attempts, "only confirmed completion resets attempt credit")
			require.Equal(t, observation.Input.Preparation.RequestedAt, finished.Job.CadenceAnchor)
			gets := f.provider.calls
			historical, err := s.InspectLast(ctx, j.Source)
			require.NoError(t, err)
			require.True(t, sameStatusSupersession(*out.Supersession, historical))
			require.Equal(t, gets, f.provider.calls)
		})
	}
}
