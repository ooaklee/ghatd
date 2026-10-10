package revenuestore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Audit disposition: native named history, capacity, corruption and snapshot
// cases own isolated databases. Capacity fixtures exercise the adapter budget;
// they do not claim synthetic rows are valid economic reception evidence.
func TestMongoRevenueHistoryConfirmedSources(t *testing.T) {
	type testCase struct {
		name                              string
		refund, loss, quarantine, resolve bool
	}
	cases := []testCase{{name: "immutable_paid_source"}, {name: "cumulative_refund", refund: true}, {name: "full_confirmed_loss", loss: true}, {name: "unassociated_source_coverage", quarantine: true}, {name: "resolution_retains_original_reception", quarantine: true, resolve: true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _, _, ctx := revenueFixture(t)
			s := revenueService(t, r)
			f := revenueFact()
			f.ProviderCustomerID = "private-customer"
			accept(t, s, ctx, "original", f)
			if tc.refund {
				f.Kind = billing.RevenueRefund
				f.AdjustmentID = "refund"
				f.CumulativeRefundedMinor = 2000
				accept(t, s, ctx, "refund", f)
			}
			if tc.loss {
				f.Kind = billing.RevenueDisputeLost
				f.AdjustmentID = "dispute"
				accept(t, s, ctx, "loss", f)
			}
			if tc.quarantine {
				o, err := s.AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: f.Scope, EnvelopeID: "unknown", QuarantineReason: "historical_payer_pending"})
				require.NoError(t, err)
				if tc.resolve {
					_, err = s.ResolveQuarantinedRevenue(ctx, billing.ResolveRevenueRequest{ObservationID: o.ID, ExpectedFingerprint: o.Fingerprint, Reason: "verified_no_subscription", ActorID: "current-worker"})
					require.NoError(t, err)
				}
			}
			out, err := s.GetPaymentRevenueHistory(ctx, billing.RevenueHistoryQuery{Scopes: []billing.RevenueScope{f.Scope}, Principals: []string{f.PrincipalID}})
			require.NoError(t, err)
			require.Len(t, out.Items, 1)
			wantNet := int64(10000)
			if tc.refund {
				wantNet = 8000
			}
			if tc.loss {
				wantNet = 0
			}
			require.Equal(t, wantNet, out.Items[0].NetMinor)
			unknown := 0
			if tc.quarantine && !tc.resolve {
				unknown = 1
			}
			require.Equal(t, unknown, out.ScopedUnresolvedSources)
			restarted, err := NewRepository(r.store)
			require.NoError(t, err)
			again := revenueService(t, restarted)
			recovered, err := again.GetPaymentRevenueHistory(ctx, billing.RevenueHistoryQuery{Scopes: []billing.RevenueScope{f.Scope}, Principals: []string{f.PrincipalID}})
			require.NoError(t, err)
			require.Equal(t, out, recovered)
		})
	}
}
func TestMongoRevenueHistoryMissingProof(t *testing.T) {
	type testCase struct{ name, kind string }
	cases := []testCase{{name: "fact_missing_from_retained_sequence", kind: kindFact}, {name: "global_sequence_head_missing", kind: kindHead}, {name: "original_reception_receipt_missing", kind: kindObservation}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, _, db, ctx := revenueFixture(t)
			s := revenueService(t, r)
			f := revenueFact()
			accept(t, s, ctx, "original", f)
			_, err := db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": tc.kind})
			require.NoError(t, err)
			out, err := s.GetPaymentRevenueHistory(ctx, billing.RevenueHistoryQuery{Scopes: []billing.RevenueScope{f.Scope}, Principals: []string{f.PrincipalID}})
			require.ErrorIs(t, err, billing.ErrRevenueUnassessable)
			require.Zero(t, out)
		})
	}
}
func TestMongoRevenueHistoryCompleteCapacity(t *testing.T) {
	type testCase struct {
		name                string
		facts, observations int
		want                error
	}
	cases := []testCase{{name: "exact_complete_capacity", facts: billing.RevenueHistoryCapacity}, {name: "next_fact_exceeds_complete_capacity", facts: billing.RevenueHistoryCapacity + 1, want: billing.ErrRevenueHistoryTooLarge}, {name: "observation_counts_toward_combined_capacity", facts: billing.RevenueHistoryCapacity, observations: 1, want: billing.ErrRevenueHistoryTooLarge}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, store, _, fixtureCtx := revenueFixture(t)
			ctx, cancel := context.WithTimeout(context.WithoutCancel(fixtureCtx), 90*time.Second)
			defer cancel()
			err := store.Transact(ctx, "capacity-fixture", func(tx recordstore.Tx) error {
				for i := 0; i < tc.facts; i++ {
					f := revenueFact()
					f.ID = fmt.Sprintf("synthetic-%05d", i)
					f.Sequence = int64(i + 1)
					f.AcceptedAt = f.EffectiveAt
					f.Fingerprint = "synthetic-adapter-only"
					row, err := recordstore.NewRecord(kindFact, f.ID, partition, 1, persistedFact{f, f.Fingerprint})
					if err != nil {
						return err
					}
					row.Sequence = f.Sequence
					row.State = f.Kind
					if err := tx.Insert(ctx, row); err != nil {
						return err
					}
				}
				for i := 0; i < tc.observations; i++ {
					o := billing.RevenueObservation{ID: fmt.Sprintf("synthetic-observation-%d", i), AcceptedAt: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), Fingerprint: "synthetic-adapter-only"}
					if err := (&bound{tx}).InsertObservation(ctx, o); err != nil {
						return err
					}
				}
				row, err := recordstore.NewRecord(kindHead, partition, partition, int64(tc.facts), sequenceHead{Sequence: int64(tc.facts)})
				if err != nil {
					return err
				}
				return tx.Insert(ctx, row)
			})
			require.NoError(t, err)
			out, err := r.ReadRevenueHistory(ctx)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, out)
			} else {
				require.NoError(t, err)
				require.Len(t, out.Facts, tc.facts)
				require.Empty(t, out.Observations)
				require.EqualValues(t, tc.facts, out.Sequence)
			}
		})
	}
}

type historyInterleavedStore struct {
	recordstore.Store
	seen             chan struct{}
	committed        chan error
	headRead         bool
	failObservations bool
}
type historyInterleavedTx struct {
	recordstore.Tx
	store *historyInterleavedStore
}

func (s *historyInterleavedStore) Read(ctx context.Context, fn func(recordstore.Tx) error) error {
	return s.Store.Read(ctx, func(tx recordstore.Tx) error { return fn(&historyInterleavedTx{tx, s}) })
}
func (t *historyInterleavedTx) Get(ctx context.Context, kind, id string) (recordstore.Record, error) {
	row, err := t.Tx.Get(ctx, kind, id)
	if err == nil && kind == kindHead && !t.store.headRead {
		t.store.headRead = true
		close(t.store.seen)
		if err := <-t.store.committed; err != nil {
			return recordstore.Record{}, err
		}
	}
	return row, err
}
func (t *historyInterleavedTx) Find(ctx context.Context, q recordstore.Query) ([]recordstore.Record, error) {
	if q.Kind == kindObservation && t.store.failObservations {
		return nil, recordstore.ErrUnavailable
	}
	return t.Tx.Find(ctx, q)
}
func TestMongoRevenueHistorySnapshotAndLateFailure(t *testing.T) {
	type testCase struct {
		name string
		fail bool
	}
	cases := []testCase{{name: "new_acceptance_after_pinned_head_does_not_tear_history"}, {name: "late_observation_outage_discards_loaded_facts", fail: true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, store, _, ctx := revenueFixture(t)
			s := revenueService(t, r)
			f := revenueFact()
			accept(t, s, ctx, "first", f)
			interleaved := &historyInterleavedStore{Store: store, seen: make(chan struct{}), committed: make(chan error, 1), failObservations: tc.fail}
			readerRepo, err := NewRepository(interleaved)
			require.NoError(t, err)
			reader := revenueService(t, readerRepo)
			go func() {
				<-interleaved.seen
				next := f
				next.PaymentID = "renewal"
				_, err := s.AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: next.Scope, EnvelopeID: "renewal", Facts: []billing.RevenueFact{next}})
				interleaved.committed <- err
			}()
			out, err := reader.GetPaymentRevenueHistory(ctx, billing.RevenueHistoryQuery{Scopes: []billing.RevenueScope{f.Scope}, Principals: []string{f.PrincipalID}})
			if tc.fail {
				require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
				require.Zero(t, out)
			} else {
				require.NoError(t, err)
				require.Len(t, out.Items, 1)
				require.EqualValues(t, 1, out.AcceptanceSequence)
			}
			current, err := s.GetPaymentRevenueHistory(ctx, billing.RevenueHistoryQuery{Scopes: []billing.RevenueScope{f.Scope}, Principals: []string{f.PrincipalID}})
			require.NoError(t, err)
			require.Len(t, current.Items, 2)
			require.EqualValues(t, 2, current.AcceptanceSequence)
		})
	}
}

type historyNoCallbackStore struct{ recordstore.Store }

func (s *historyNoCallbackStore) Read(context.Context, func(recordstore.Tx) error) error { return nil }

func TestMongoRevenueHistoryReadBoundaries(t *testing.T) {
	type testCase struct {
		name, mode, kind, field string
		want                    error
	}
	cases := []testCase{
		{name: "nil_context", mode: "nil", want: billing.ErrRevenueInvalid},
		{name: "already_canceled_read", mode: "canceled", want: context.Canceled},
		{name: "nonexecuted_read_callback_is_unavailable", mode: "callback", want: billing.ErrRevenueUnavailable},
		{name: "facts_never_gain_analytics_expiry", kind: kindFact, field: "expires_at", want: billing.ErrRevenueUnavailable},
		{name: "original_receipts_never_gain_analytics_expiry", kind: kindObservation, field: "expires_at", want: billing.ErrRevenueUnavailable},
		{name: "global_head_never_gains_analytics_expiry", kind: kindHead, field: "expires_at", want: billing.ErrRevenueUnavailable},
		{name: "fact_state_tampering_is_unavailable", kind: kindFact, field: "state", want: billing.ErrRevenueUnavailable},
		{name: "receipt_state_tampering_is_unavailable", kind: kindObservation, field: "state", want: billing.ErrRevenueUnavailable},
		{name: "head_revision_tampering_is_unavailable", kind: kindHead, field: "revision", want: billing.ErrRevenueUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, store, db, fixtureCtx := revenueFixture(t)
			s := revenueService(t, r)
			accept(t, s, fixtureCtx, "original", revenueFact())
			ctx := fixtureCtx
			switch tc.mode {
			case "nil":
				ctx = nil
			case "canceled":
				canceled, cancel := context.WithCancel(fixtureCtx)
				cancel()
				ctx = canceled
			case "callback":
				var err error
				r, err = NewRepository(&historyNoCallbackStore{Store: store})
				require.NoError(t, err)
			default:
				var value any = "tampered"
				if tc.field == "expires_at" {
					value = time.Now().UTC().Add(24 * time.Hour)
				}
				if tc.field == "revision" {
					value = int64(99)
				}
				changed, err := db.Collection("ghatd_owned_records").UpdateOne(ctx, bson.M{"kind": tc.kind}, bson.M{"$set": bson.M{tc.field: value}})
				require.NoError(t, err)
				require.EqualValues(t, 1, changed.ModifiedCount)
			}
			out, err := r.ReadRevenueHistory(ctx)
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, out)
		})
	}
}
