package billingmanager

import (
	"bytes"
	"context"
	"errors"
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

// Loss and authority changes happen AFTER the real transaction commits. This
// proves native receipt recovery, not server commit labels or host identity.
type checkoutCompletionLostReply struct {
	recordstore.Store
	lose  bool
	after func()
}

func (s *checkoutCompletionLostReply) Transact(ctx context.Context, key string, fn func(recordstore.Tx) error) error {
	if err := s.Store.Transact(ctx, key, fn); err != nil {
		return err
	}
	if s.after != nil {
		s.after()
	}
	if s.lose {
		return fmt.Errorf("controlled lost commit reply: %w", recordstore.ErrUncertain)
	}
	return nil
}

func TestCheckoutLifecycleManagerEncryptedRecovery(t *testing.T) {
	for _, tc := range []struct {
		name, late string
		lose       bool
	}{
		{"lost_reply_current_authority", "", true},
		{"lost_reply_then_revoked", "denied", true},
		{"lost_reply_then_canceled", "canceled", true},
		{"acknowledged_commit_then_revoked_is_known", "denied", false},
		{"acknowledged_commit_then_canceled_is_known", "canceled", false},
		{"acknowledged_commit_returns_receipt", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uri := os.Getenv("GHATD_TEST_MONGO_URI")
			if uri == "" {
				t.Skip("set GHATD_TEST_MONGO_URI to an isolated replica set")
			}
			setup, done := context.WithTimeout(t.Context(), 30*time.Second)
			t.Cleanup(done)
			client, err := mongo.Connect(options.Client().ApplyURI(uri))
			require.NoError(t, err)
			db := client.Database(fmt.Sprintf("ghatd_checkout_completion_%d", time.Now().UnixNano()))
			t.Cleanup(func() {
				cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				require.NoError(t, db.Drop(cleanup))
				require.NoError(t, client.Disconnect(cleanup))
			})
			cipher, err := encryption.NewPayloadCipher(bytes.Repeat([]byte{0x72}, 32))
			require.NoError(t, err)
			store, err := recordstore.NewMongoStoreFromDatabase(db, cipher)
			require.NoError(t, err)
			require.NoError(t, store.EnsureIndexes(setup))
			require.NoError(t, store.Probe(setup))
			reply := &checkoutCompletionLostReply{Store: store}
			repo, err := revenuestore.NewRepository(reply)
			require.NoError(t, err)
			clock := fixtureBillingStatusClock{time.Unix(1700000000, 123456789).UTC()}
			provider := &completionProvider{}
			owner, err := billing.NewCheckoutService(repo, clock, provider)
			require.NoError(t, err)
			scope := billing.RevenueScope{Provider: "stripe", AccountID: "merchant-a"}
			q := paymentprovider.CheckoutSessionRequest{IdempotencyKey: "original", PriceID: "price_original", PlanID: "plan", CostID: "cost", UserID: "original-payer", UserReference: "original-payer", CustomerEmail: "payer@example.test", ReturnURL: "https://example.test/checkout", Mode: paymentprovider.CheckoutModeSubscription, ExpectedCurrency: "GBP", ExpectedAmount: 1000, ExpectedBillingCadence: "month", TrialPeriodDays: 14}
			i, err := owner.PrepareCheckout(setup, scope, q)
			require.NoError(t, err)
			require.NoError(t, owner.AcknowledgeCheckout(setup, i, "cs_original"))
			i, err = owner.FindCheckoutIntent(setup, scope, q.IdempotencyKey)
			require.NoError(t, err)
			provider.e = paymentprovider.RevenueCheckoutEvidence{Scope: paymentprovider.RevenueScope{Provider: scope.Provider, AccountID: scope.AccountID}, SessionID: i.SessionID, IntentID: i.ID, ClientReferenceID: q.UserID, CustomerID: "cus_original", SubscriptionID: "sub_trial", PriceID: q.PriceID, Currency: "GBP", Mode: q.Mode, Status: "complete", UnitAmountMinor: 1000, IntervalCount: 1, BillingCadence: "month", CreatedAt: i.CreatedAt}
			revenue, err := billing.NewRevenueService(repo, clock)
			require.NoError(t, err)
			manager, err := (&Service{}).WithRevenueServices(revenueBoundaryRegistry{p: &revenueBoundaryProvider{}}, revenue, owner)
			require.NoError(t, err)
			authority := &completionAuthority{denied: errors.New("revoked checkout authority")}
			_, err = manager.WithCheckoutLifecycleAuthority(authority)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(setup)
			t.Cleanup(cancel)
			reply.lose = tc.lose
			reply.after = func() {
				if tc.late == "denied" {
					authority.denyAt = authority.calls + 1
				}
				if tc.late == "canceled" {
					cancel()
				}
			}
			out, err := manager.CaptureCheckoutLifecycleEvidence(ctx, "current-worker", i, provider.e)
			if tc.lose {
				require.ErrorIs(t, err, billing.ErrRevenueUncertain)
			} else {
				require.NotErrorIs(t, err, billing.ErrRevenueUncertain)
			}
			if tc.late == "denied" {
				require.ErrorIs(t, err, authority.denied)
			}
			if tc.late == "canceled" {
				require.ErrorIs(t, err, context.Canceled)
			}
			if tc.lose || tc.late != "" {
				require.Zero(t, out)
			} else {
				require.NoError(t, err)
				require.NoError(t, out.ValidateCapturedEvidence(i, provider.e))
			}
			reply.lose = false
			reply.after = nil
			authority.denyAt = 0
			receipt, err := manager.FindCheckoutLifecycleReceipt(setup, "replacement-worker", i)
			require.NoError(t, err)
			require.NoError(t, receipt.ValidateCapturedEvidence(i, provider.e))
			if !tc.lose && tc.late == "" {
				require.Equal(t, out, receipt)
			}
			again, err := manager.CaptureCheckoutLifecycleEvidence(setup, "replacement-worker", i, provider.e)
			require.NoError(t, err)
			require.Equal(t, receipt, again)
			// A newly fetched/changed response cannot replace retained capture evidence.
			changed := provider.e
			changed.CreatedAt = changed.CreatedAt.Add(time.Second)
			bad, err := manager.CaptureCheckoutLifecycleEvidence(setup, "replacement-worker", i, changed)
			require.Error(t, err)
			require.Zero(t, bad)
			original, err := manager.FindCheckoutLifecycleReceipt(setup, "replacement-worker", i)
			require.NoError(t, err)
			require.Equal(t, receipt, original)
			require.Zero(t, provider.calls)
			require.Equal(t, "replacement-worker", authority.actor)
			for _, target := range authority.targets {
				require.Equal(t, q.UserID, target.PrincipalID)
				require.Equal(t, i.ID, target.IntentID)
				require.Equal(t, scope, target.Scope)
			}
			collection := db.Collection("ghatd_owned_records")
			count, err := collection.CountDocuments(setup, bson.M{"kind": "billing_revenue_fact"})
			require.NoError(t, err)
			require.Zero(t, count)
			count, err = collection.CountDocuments(setup, bson.M{"kind": "billing_checkout_lifecycle_receipt"})
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
			var raw bson.Raw
			require.NoError(t, collection.FindOne(setup, bson.M{"kind": "billing_checkout_lifecycle_receipt"}).Decode(&raw))
			require.Equal(t, bson.TypeBinary, raw.Lookup("payload").Type)
			require.NotContains(t, string(raw), "cus_original")
			require.NotContains(t, string(raw), "payer@example.test")
		})
	}
}
