package revenuestore

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Named native encrypted snapshots prove original joins and no owning-record
// writes; transaction-guard bookkeeping is not financial/lifecycle evidence.
func TestMongoCheckoutLifecyclePreparationAndReceipt(t *testing.T) {
	for _, tc := range []struct {
		name, operation, damage string
		want                    error
	}{
		{"prepare_original", "prepare", "", nil}, {"receipt_recovers_original", "receipt", "", nil},
		{"receipt_recovery_ignores_new_clock", "receipt", "backwards", nil},
		{"preparation_keeps_original_creation", "prepare", "backwards", nil},
		{"later_receipt_requires_first_receipt", "receipt", "first-receipt", billing.ErrRevenueUnavailable},
		{"receipt_absence_conclusive_only", "receipt", "receipt", billing.ErrRevenueNotFound},
		{"prepare_missing_reverse", "prepare", "reverse", billing.ErrRevenueUnavailable},
		{"lookup_missing_reverse_before_provider", "lookup", "reverse", billing.ErrRevenueUnavailable},
		{"capture_missing_reverse_no_replay_success", "capture", "reverse", billing.ErrRevenueUnavailable},
		{"receipt_missing_reverse_no_success", "receipt", "reverse", billing.ErrRevenueUnavailable},
		{"prepare_wrong_reverse_owner", "prepare", "owner", billing.ErrRevenueConflict},
		{"receipt_wrong_reverse_session", "receipt", "session", billing.ErrRevenueConflict},
		{"prepare_missing_forward_ack", "prepare", "ack", billing.ErrRevenueUnavailable},
		{"receipt_missing_forward_ack", "receipt", "ack", billing.ErrRevenueUnavailable},
		{"prepare_missing_original", "prepare", "intent", billing.ErrRevenueUnavailable},
		{"receipt_missing_original", "receipt", "intent", billing.ErrRevenueUnavailable},
		{"receipt_missing_first_anchor", "receipt", "anchor", billing.ErrRevenueUnavailable},
		{"prepare_changed_created_at", "prepare", "created", billing.ErrRevenueConflict},
		{"receipt_changed_original_input", "receipt", "created", billing.ErrRevenueConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, store, db, ctx := revenueFixture(t)
			p := &lifecycleEvidenceFixture{}
			s := lifecycleService(t, repo, p, time.Unix(1700000000, 0).UTC())
			i := lifecycleIntent(t, s, ctx)
			e := checkoutEvidence(i)
			p.evidence = e
			first, err := s.CaptureCheckoutLifecycleEvidence(ctx, i, e)
			require.NoError(t, err)
			if tc.damage == "backwards" {
				s = lifecycleService(t, repo, p, i.CreatedAt.Add(-time.Hour))
			}
			if tc.damage == "first-receipt" {
				originalID := i.ID
				q := historicalCheckoutRequest()
				q.IdempotencyKey = "later-intent"
				j, err := s.PrepareCheckout(ctx, i.Scope, q)
				require.NoError(t, err)
				require.NoError(t, s.AcknowledgeCheckout(ctx, j, "cs_later"))
				j, err = s.FindCheckoutIntent(ctx, j.Scope, q.IdempotencyKey)
				require.NoError(t, err)
				e2 := checkoutEvidence(j)
				e2.SessionID = j.SessionID
				_, err = s.CaptureCheckoutLifecycleEvidence(ctx, j, e2)
				require.NoError(t, err)
				_, err = db.Collection("ghatd_owned_records").DeleteOne(ctx, bson.M{"kind": kindCheckoutLifecycleReceipt, "id": originalID})
				require.NoError(t, err)
				i = j
			}
			kinds := map[string]string{"reverse": kindCheckoutSession, "ack": kindCheckoutAck, "intent": kindCheckoutIntent, "receipt": kindCheckoutLifecycleReceipt, "anchor": kindCheckoutLifecycleAnchor}
			if kind := kinds[tc.damage]; kind != "" {
				_, err = db.Collection("ghatd_owned_records").DeleteMany(ctx, bson.M{"kind": kind})
				require.NoError(t, err)
			}
			if tc.damage == "owner" || tc.damage == "session" {
				err = store.Transact(ctx, "corrupt-test-fixture", func(tx recordstore.Tx) error {
					row, err := tx.Get(ctx, kindCheckoutSession, associationKey(i.Scope, i.SessionID, ""))
					if err != nil {
						return err
					}
					var v checkoutAcknowledgement
					require.NoError(t, json.Unmarshal(row.Data, &v))
					if tc.damage == "owner" {
						v.IntentID = "other_intent"
					} else {
						v.SessionID = "other_session"
					}
					row.Data, err = json.Marshal(v)
					if err != nil {
						return err
					}
					expected := row.Revision
					row.Revision++
					return tx.Replace(ctx, row, expected)
				})
				require.NoError(t, err)
			}
			if tc.damage == "created" {
				i.CreatedAt = i.CreatedAt.Add(time.Second)
			}
			snapshot := func() []bson.Raw {
				cur, err := db.Collection("ghatd_owned_records").Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "kind", Value: 1}, {Key: "id", Value: 1}}))
				require.NoError(t, err)
				defer cur.Close(ctx)
				var out []bson.Raw
				require.NoError(t, cur.All(ctx, &out))
				return out
			}
			before := snapshot()
			switch tc.operation {
			case "prepare":
				out, err := s.PrepareCheckoutLifecycle(ctx, i)
				require.ErrorIs(t, err, tc.want)
				if tc.want == nil {
					require.Equal(t, i, out)
					out.Request.Metadata["mutated"] = "outside"
					fresh, err := s.PrepareCheckoutLifecycle(ctx, i)
					require.NoError(t, err)
					require.NotContains(t, fresh.Request.Metadata, "mutated")
				} else {
					require.Zero(t, out)
				}
			case "receipt":
				out, err := s.FindCheckoutLifecycleReceipt(ctx, i)
				require.ErrorIs(t, err, tc.want)
				if tc.want == nil {
					require.Equal(t, first, out)
				} else {
					require.Zero(t, out)
				}
			case "lookup":
				out, err := s.LookupCheckoutLifecycleEvidence(ctx, i)
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, out)
			case "capture":
				out, err := s.CaptureCheckoutLifecycleEvidence(ctx, i, e)
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, out)
			}
			require.Equal(t, before, snapshot())
			require.Zero(t, p.calls)
		})
	}
}
