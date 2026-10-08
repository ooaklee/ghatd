package billingmanager

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/encryption"
	"github.com/ooaklee/ghatd/external/paymentprovider"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// The reply loss occurs after a real successful transaction. Server commit
// labels, provider authentication and full host runtime remain separate proof.
type checkoutStatusLostReply struct {
	recordstore.Store
	lose bool
}

func (s *checkoutStatusLostReply) Transact(ctx context.Context, key string, fn func(recordstore.Tx) error) error {
	if err := s.Store.Transact(ctx, key, fn); err != nil {
		return err
	}
	if s.lose {
		return recordstore.ErrUncertain
	}
	return nil
}

func TestCheckoutSubscriptionStatusManagerEncryptedMongo(t *testing.T) {
	for _, tc := range []struct{ name, state string }{
		{"prepayment_trial_is_distinct_from_paid_revenue", "trial"},
		{"later_paid_head_is_read_through_original_checkout", "paid"},
		{"lost_native_capture_reply_recovers_original_no_get", "lost"},
		{"original_capture_replay_after_later_head", "later"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uri := os.Getenv("GHATD_TEST_MONGO_URI")
			if uri == "" {
				t.Skip("set GHATD_TEST_MONGO_URI to an isolated replica set")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			t.Cleanup(cancel)
			client, err := mongo.Connect(options.Client().ApplyURI(uri))
			require.NoError(t, err)
			db := client.Database(fmt.Sprintf("ghatd_checkout_status_manager_%d", time.Now().UnixNano()))
			t.Cleanup(func() {
				cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
				defer done()
				require.NoError(t, db.Drop(cleanup))
				require.NoError(t, client.Disconnect(cleanup))
			})
			cipher, err := encryption.NewPayloadCipher(bytes.Repeat([]byte{0x71}, 32))
			require.NoError(t, err)
			store, err := recordstore.NewMongoStoreFromDatabase(db, cipher)
			require.NoError(t, err)
			require.NoError(t, store.EnsureIndexes(ctx))
			require.NoError(t, store.Probe(ctx))
			reply := &checkoutStatusLostReply{Store: store}
			repo, err := revenuestore.NewRepository(reply)
			require.NoError(t, err)
			clock := &checkoutStatusManagerClock{time.Unix(1700000000, 123456789).UTC()}
			scope := billing.RevenueScope{Provider: "stripe", AccountID: "acct_native"}
			i := nativeDiscoveryCheckout(t, ctx, repo, fixtureBillingStatusClock{clock.at}, &discoveryNativeCheckoutProvider{}, scope, "original", "payer", "cs_original", "sub_trial", true)
			owner, err := billing.NewRevenueService(repo, clock)
			require.NoError(t, err)
			provider := &statusManagerProvider{revenueBoundaryProvider: &revenueBoundaryProvider{}, evidence: paymentprovider.RevenueSubscriptionEvidence{Scope: paymentprovider.RevenueScope{Provider: scope.Provider, AccountID: scope.AccountID}, SubscriptionID: "sub_trial", CustomerID: "cus_payer", Status: "trialing"}}
			manager, err := (&Service{}).WithRevenueServices(revenueBoundaryRegistry{p: provider}, owner, &revenueBoundaryAssociation{})
			require.NoError(t, err)
			_, err = manager.WithSubscriptionStatusAuthority(&statusManagerAuthority{})
			require.NoError(t, err)
			p, err := manager.PrepareSubscriptionStatusForCheckout(ctx, "original-author", scope, "sub_trial")
			require.NoError(t, err)
			require.Equal(t, i.ID, p.CheckoutIntentID)
			require.Empty(t, p.FactID)
			require.NoError(t, manager.ValidateSubscriptionStatusPreparation(ctx, "replacement-worker", p))
			e, err := manager.LookupSubscriptionStatus(ctx, "replacement-worker", p)
			require.NoError(t, err)
			reply.lose = tc.state == "lost"
			first, err := manager.CaptureSubscriptionStatus(ctx, "replacement-worker", p, e)
			if reply.lose {
				require.ErrorIs(t, err, billing.ErrRevenueUncertain)
				require.Zero(t, first)
				reply.lose = false
				first, err = manager.CaptureSubscriptionStatus(ctx, "replacement-worker", p, e)
			}
			require.NoError(t, err)
			require.Equal(t, "trialing", first.Status)
			require.Equal(t, "original-author", first.Preparation.ActorID)
			clock.at = clock.at.Add(time.Minute)
			if tc.state == "paid" {
				observation, err := owner.AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: scope, EnvelopeID: "evt_first", Facts: []billing.RevenueFact{{Scope: scope, Kind: billing.RevenuePayment, PaymentID: "pi_first", InvoiceID: "in_first", AllocationID: "il_first", PrincipalID: "payer", ProviderCustomerID: "cus_payer", SubscriptionID: "sub_trial", PlanID: "plan", CostID: "cost", Currency: "GBP", CurrencyExponent: 2, PaidMinor: 1000, EffectiveAt: clock.at}}})
				require.NoError(t, err)
				paid, err := manager.PrepareSubscriptionStatus(ctx, "paid-author", observation.FactIDs[0])
				require.NoError(t, err)
				require.Empty(t, paid.Source)
				active := e
				active.Status = "active"
				_, err = manager.CaptureSubscriptionStatus(ctx, "replacement-worker", paid, active)
				require.NoError(t, err)
			}
			if tc.state == "later" {
				next, err := manager.PrepareSubscriptionStatusForCheckout(ctx, "later-author", scope, "sub_trial")
				require.NoError(t, err)
				active := e
				active.Status = "active"
				_, err = manager.CaptureSubscriptionStatus(ctx, "replacement-worker", next, active)
				require.NoError(t, err)
				replayed, err := manager.CaptureSubscriptionStatus(ctx, "replacement-worker", p, e)
				require.NoError(t, err)
				require.Equal(t, first, replayed)
			}
			current, err := manager.GetSubscriptionStatusForCheckout(ctx, "current-reader", scope, "sub_trial", 2*time.Minute)
			require.NoError(t, err)
			if tc.state == "paid" || tc.state == "later" {
				require.Equal(t, "active", current.Status)
				require.EqualValues(t, 2, current.Revision)
			} else {
				require.Equal(t, first, current)
			}
			require.Equal(t, 1, provider.calls)
			if tc.state != "paid" {
				count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": "billing_revenue_fact"})
				require.NoError(t, err)
				require.Zero(t, count)
			}
			var raw bson.Raw
			require.NoError(t, db.Collection("ghatd_owned_records").FindOne(ctx, bson.M{"kind": "billing_subscription_status_head"}).Decode(&raw))
			require.Equal(t, bson.TypeBinary, raw.Lookup("payload").Type)
			require.NotContains(t, string(raw), "sub_trial")
			require.NotContains(t, string(raw), "cus_payer")
			require.NotContains(t, string(raw), "payer@example.test")
		})
	}
}
