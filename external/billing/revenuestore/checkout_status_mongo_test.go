package revenuestore

import (
	"context"
	"testing"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Audit disposition: fresh encrypted Mongo databases per case prove native
// forward/reverse ownership. Corruption is injected only into disposable test
// records, never a host database, and lookup makes no provider calls or writes.
func TestMongoFindOriginalAcknowledgedCheckout(t *testing.T) {
	for _, tc := range []struct {
		name, removeKind                                         string
		wrongSession, wrongScope, wrongMode, paymentMode, cancel bool
	}{
		{name: "original_subscription"},
		{name: "original_one_time", paymentMode: true},
		{name: "missing_reverse", removeKind: kindCheckoutSession},
		{name: "missing_forward", removeKind: kindCheckoutAck},
		{name: "missing_original", removeKind: kindCheckoutIntent},
		{name: "unknown_session", wrongSession: true},
		{name: "another_merchant", wrongScope: true},
		{name: "another_live_mode", wrongMode: true},
		{name: "cancelled_read", cancel: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, _, db, ctx := revenueFixture(t)
			p := &checkoutEvidenceFixture{}
			s := checkoutServiceFixture(t, repo, p)
			q := historicalCheckoutRequest()
			if tc.paymentMode {
				q.Mode = "payment"
				q.ExpectedBillingCadence = "one_time"
			}
			i, err := s.PrepareCheckout(ctx, checkoutResolverRequest().Scope, q)
			require.NoError(t, err)
			require.NoError(t, s.AcknowledgeCheckout(ctx, i, "cs_original"))
			if tc.removeKind != "" {
				_, err = db.Collection("ghatd_owned_records").DeleteMany(ctx, bson.M{"kind": tc.removeKind})
				require.NoError(t, err)
			}
			before, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{})
			require.NoError(t, err)
			scope := i.Scope
			session := "cs_original"
			if tc.wrongSession {
				session = "cs_unknown"
			}
			if tc.wrongScope {
				scope.AccountID = "another-merchant"
			}
			if tc.wrongMode {
				scope.LiveMode = true
			}
			readCtx := ctx
			if tc.cancel {
				var cancel func()
				readCtx, cancel = context.WithCancel(ctx)
				cancel()
			}
			out, err := s.FindAcknowledgedCheckout(readCtx, scope, session)
			if tc.removeKind != "" || tc.wrongSession || tc.wrongScope || tc.wrongMode || tc.cancel {
				require.Error(t, err)
				require.Equal(t, billing.CheckoutIntent{}, out)
			} else {
				require.NoError(t, err)
				require.Equal(t, i.ID, out.ID)
				require.Equal(t, i.Fingerprint, out.Fingerprint)
				require.Equal(t, "cs_original", out.SessionID)
				require.Equal(t, q.Mode, out.Request.Mode)
			}
			after, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{})
			require.NoError(t, err)
			require.Equal(t, before, after)
			require.Zero(t, p.calls)
		})
	}
}
