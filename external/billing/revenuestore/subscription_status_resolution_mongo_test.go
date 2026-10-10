package revenuestore

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Read faults are injected around actual encrypted snapshot operations, not
// converted into fabricated absence. Resolution must never perform writes.
type statusResolutionReadStore struct {
	recordstore.Store
	kind, id string
	failure  error
	writes   int
}
type statusResolutionReadTx struct {
	recordstore.Tx
	kind, id string
	failure  error
}

func (s *statusResolutionReadStore) Read(ctx context.Context, fn func(recordstore.Tx) error) error {
	return s.Store.Read(ctx, func(tx recordstore.Tx) error { return fn(statusResolutionReadTx{tx, s.kind, s.id, s.failure}) })
}
func (t statusResolutionReadTx) Get(ctx context.Context, kind, id string) (recordstore.Record, error) {
	if kind == t.kind && (t.id == "" || id == t.id) && t.failure != nil {
		return recordstore.Record{}, t.failure
	}
	return t.Tx.Get(ctx, kind, id)
}
func (s *statusResolutionReadStore) Transact(ctx context.Context, key string, fn func(recordstore.Tx) error) error {
	s.writes++
	return s.Store.Transact(ctx, key, fn)
}

func TestMongoSubscriptionStatusOriginalResolution(t *testing.T) {
	for _, source := range []string{"payment", "checkout"} {
		for _, tc := range []struct {
			name, state string
			want        error
		}{
			{"pending_first", billing.SubscriptionStatusPending, nil},
			{"pending_current", billing.SubscriptionStatusPending, nil},
			{"superseded", billing.SubscriptionStatusSuperseded, nil},
			{"captured_after_later_head", billing.SubscriptionStatusCaptured, nil},
			{"captured_ignores_corrupt_later_head", billing.SubscriptionStatusCaptured, nil},
			{"lost_capture_reply", billing.SubscriptionStatusCaptured, nil},
			{"missing_expected_head", "", billing.ErrRevenueUnavailable},
			{"missing_joined_head_receipt", "", billing.ErrRevenueUnavailable},
			{"joined_capture_absence_outage", "", billing.ErrRevenueUnavailable},
			{"joined_capture_absence_uncertain", "", billing.ErrRevenueUncertain},
			{"joined_head_absence_outage", "", billing.ErrRevenueUnavailable},
			{"canceled", "", context.Canceled},
		} {
			t.Run(source+"/"+tc.name, func(t *testing.T) {
				r, store, db, ctx := revenueFixture(t)
				var owner *billing.RevenueService
				var clock *subscriptionClock
				var prepare func(string) (billing.SubscriptionStatusPreparation, error)
				if source == "payment" {
					var fact string
					owner, clock, fact = nativeStatus(t, r, ctx)
					prepare = func(actor string) (billing.SubscriptionStatusPreparation, error) {
						return owner.PrepareSubscriptionStatus(ctx, actor, fact)
					}
				} else {
					checkout := lifecycleService(t, r, &lifecycleEvidenceFixture{}, time.Unix(1700000000, 0).UTC())
					i := lifecycleIntent(t, checkout, ctx)
					e := checkoutEvidence(i)
					_, err := checkout.CaptureCheckoutLifecycleEvidence(ctx, i, e)
					require.NoError(t, err)
					clock = &subscriptionClock{i.CreatedAt.Add(time.Second)}
					owner, err = billing.NewRevenueService(r, clock)
					require.NoError(t, err)
					prepare = func(actor string) (billing.SubscriptionStatusPreparation, error) {
						return owner.PrepareSubscriptionStatusForCheckout(ctx, actor, i.Scope, e.SubscriptionID)
					}
				}
				p, err := prepare("original")
				require.NoError(t, err)
				coll := db.Collection("ghatd_owned_records")
				switch tc.name {
				case "pending_current", "missing_expected_head", "missing_joined_head_receipt":
					first, e := owner.CaptureVerifiedSubscriptionStatus(ctx, p, nativeStatusEvidence(p, "trialing"))
					require.NoError(t, e)
					clock.at = clock.at.Add(time.Second)
					p, e = prepare("original")
					require.NoError(t, e)
					filter := bson.M{"kind": kindSubscriptionStatusHead, "id": billing.SubscriptionStatusIdentity(p.Scope, p.SubscriptionID)}
					if tc.name == "missing_joined_head_receipt" {
						filter = bson.M{"kind": kindSubscriptionStatusCapture, "id": first.Preparation.CaptureID}
					}
					if tc.name != "pending_current" {
						_, e = coll.DeleteOne(ctx, filter)
						require.NoError(t, e)
					}
				case "superseded":
					clock.at = clock.at.Add(time.Second)
					other, e := prepare("other")
					require.NoError(t, e)
					_, e = owner.CaptureVerifiedSubscriptionStatus(ctx, other, nativeStatusEvidence(other, "active"))
					require.NoError(t, e)
				case "captured_after_later_head", "captured_ignores_corrupt_later_head":
					_, e := owner.CaptureVerifiedSubscriptionStatus(ctx, p, nativeStatusEvidence(p, "trialing"))
					require.NoError(t, e)
					clock.at = clock.at.Add(time.Second)
					other, e := prepare("other")
					require.NoError(t, e)
					_, e = owner.CaptureVerifiedSubscriptionStatus(ctx, other, nativeStatusEvidence(other, "active"))
					require.NoError(t, e)
					if tc.name == "captured_ignores_corrupt_later_head" {
						_, e = coll.UpdateOne(ctx, bson.M{"kind": kindSubscriptionStatusHead}, bson.M{"$set": bson.M{"state": "corrupt"}})
						require.NoError(t, e)
					}
				case "lost_capture_reply":
					broken, e := NewRepository(failingStore{Store: store, uncertain: true})
					require.NoError(t, e)
					attempt, e := billing.NewRevenueService(broken, clock)
					require.NoError(t, e)
					v, e := attempt.CaptureVerifiedSubscriptionStatus(ctx, p, nativeStatusEvidence(p, "trialing"))
					require.ErrorIs(t, e, billing.ErrRevenueUncertain)
					require.Zero(t, v)
				}
				spy := &statusResolutionReadStore{Store: store}
				switch tc.name {
				case "joined_capture_absence_outage":
					spy.kind, spy.id, spy.failure = kindSubscriptionStatusCapture, p.CaptureID, errors.Join(recordstore.ErrNotFound, recordstore.ErrUnavailable)
				case "joined_capture_absence_uncertain":
					spy.kind, spy.id, spy.failure = kindSubscriptionStatusCapture, p.CaptureID, errors.Join(recordstore.ErrNotFound, recordstore.ErrUncertain)
				case "joined_head_absence_outage":
					spy.kind, spy.failure = kindSubscriptionStatusHead, errors.Join(recordstore.ErrNotFound, recordstore.ErrUnavailable)
				}
				adapter, e := NewRepository(spy)
				require.NoError(t, e)
				resolver, e := billing.NewRevenueService(adapter, clock)
				require.NoError(t, e)
				callctx := ctx
				if tc.name == "canceled" {
					var cancel context.CancelFunc
					callctx, cancel = context.WithCancel(ctx)
					cancel()
				}
				before, e := coll.CountDocuments(ctx, bson.M{})
				require.NoError(t, e)
				out, e := resolver.ResolveSubscriptionStatus(callctx, p)
				if tc.want != nil {
					require.ErrorIs(t, e, tc.want)
					require.Zero(t, out)
				} else {
					require.NoError(t, e)
					require.Equal(t, tc.state, out.State)
					require.NoError(t, out.Validate())
					if tc.state == billing.SubscriptionStatusSuperseded {
						v, e := owner.CaptureVerifiedSubscriptionStatus(ctx, p, nativeStatusEvidence(p, "trialing"))
						require.ErrorIs(t, e, billing.ErrRevenueConflict)
						require.Zero(t, v)
					}
				}
				after, e := coll.CountDocuments(ctx, bson.M{})
				require.NoError(t, e)
				require.Equal(t, before, after)
				require.Zero(t, spy.writes)
			})
		}
	}
}
