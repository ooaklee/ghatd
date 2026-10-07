package revenuestore

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Audit disposition: new isolated replica-set coverage for source quarantine
// recovery, separate from downstream consumer acknowledgements.
func TestMongoRevenueSourceResolution(t *testing.T) {
	type testCase struct {
		name      string
		noFacts   bool
		uncertain bool
		failAt    int
	}
	cases := []testCase{{name: "recovered_facts"}, {name: "reasoned_no_revenue", noFacts: true}, {name: "lost_resolution_acknowledgement", uncertain: true}, {name: "head_rollback", failAt: 1}, {name: "fact_rollback", failAt: 2}, {name: "resolution_rollback", failAt: 3}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, store, db, ctx := revenueFixture(t)
			svc := revenueService(t, repo)
			f := revenueFact()
			original, err := svc.AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: f.Scope, EnvelopeID: "source-awaiting-association", QuarantineReason: "historical_payer_plan_pending", SourceFingerprint: "private-native-source-digest"})
			require.NoError(t, err)
			pending, err := svc.UnresolvedRevenueObservations(ctx, 200)
			require.NoError(t, err)
			require.Equal(t, []billing.RevenueObservation{original}, pending)
			req := billing.ResolveRevenueRequest{ObservationID: original.ID, ExpectedFingerprint: original.Fingerprint, Reason: "authenticated_source_recovered", ActorID: "private-authorized-operator", Facts: []billing.RevenueFact{f}}
			if tc.noFacts {
				req.Facts = nil
				req.Reason = "no_subscription_revenue"
			}
			if tc.failAt > 0 || tc.uncertain {
				faultRepo, err := NewRepository(failingStore{Store: store, at: tc.failAt, uncertain: tc.uncertain})
				require.NoError(t, err)
				svc = revenueService(t, faultRepo)
			}
			resolution, err := svc.ResolveQuarantinedRevenue(ctx, req)
			if tc.failAt > 0 {
				require.ErrorIs(t, err, failWrite)
				require.Empty(t, resolution.ID)
				count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{})
				require.NoError(t, err)
				require.EqualValues(t, 1, count)
				pending, err := repo.UnresolvedRevenueObservations(ctx, 200)
				require.NoError(t, err)
				require.Len(t, pending, 1)
				return
			}
			if tc.uncertain {
				require.ErrorIs(t, err, billing.ErrRevenueUncertain)
				require.Empty(t, resolution.ID)
			} else {
				require.NoError(t, err)
			}
			recovered := revenueService(t, repo)
			replay, err := recovered.ResolveQuarantinedRevenue(ctx, req)
			require.NoError(t, err)
			require.Equal(t, req.ActorID, replay.ResolutionBy)
			require.Equal(t, "private-native-source-digest", replay.SourceFingerprint)
			read, err := recovered.GetRevenueObservation(ctx, replay.ID)
			require.NoError(t, err)
			require.Equal(t, replay, read)
			originalAgain, err := recovered.GetRevenueObservation(ctx, original.ID)
			require.NoError(t, err)
			require.Equal(t, original, originalAgain)
			delivery, err := recovered.GetRevenueDelivery(ctx, f.Scope, original.EnvelopeID)
			require.NoError(t, err)
			require.Equal(t, original, delivery)
			pending, err = recovered.UnresolvedRevenueObservations(ctx, 200)
			require.NoError(t, err)
			require.Empty(t, pending)
			facts, err := recovered.FindPaymentRevenueFacts(ctx, f.Scope, f.PaymentID)
			require.NoError(t, err)
			if tc.noFacts {
				require.Empty(t, facts)
			} else {
				require.Len(t, facts, 1)
			}
			encoded, err := json.Marshal(replay)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), req.ActorID)
			require.NotContains(t, string(encoded), replay.SourceFingerprint)
			var raw bson.M
			require.NoError(t, db.Collection("ghatd_owned_records").FindOne(ctx, bson.M{"kind": kindObservation, "id": replay.ID}).Decode(&raw))
			require.NotContains(t, raw, "resolution_by")
			req.ActorID = "different-operator"
			_, err = recovered.ResolveQuarantinedRevenue(ctx, req)
			require.ErrorIs(t, err, billing.ErrRevenueConflict)
		})
	}
}

func TestMongoRevenueConcurrentResolution(t *testing.T) {
	// Concurrent identical workers must share one immutable resolution and one
	// economic fact; this single overlapping race is impractical as a case table.
	repo, _, db, ctx := revenueFixture(t)
	svc := revenueService(t, repo)
	f := revenueFact()
	original, err := svc.AcceptVerified(ctx, billing.VerifiedRevenueRequest{Scope: f.Scope, EnvelopeID: "source", QuarantineReason: "historical_payer_plan_pending", SourceFingerprint: "private-native-source-digest"})
	require.NoError(t, err)
	req := billing.ResolveRevenueRequest{ObservationID: original.ID, ExpectedFingerprint: original.Fingerprint, Reason: "authenticated_source_recovered", ActorID: "worker", Facts: []billing.RevenueFact{f}}
	start := make(chan struct{})
	results := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; _, err := svc.ResolveQuarantinedRevenue(ctx, req); results <- err }()
	}
	close(start)
	wg.Wait()
	close(results)
	for err := range results {
		require.NoError(t, err)
	}
	for _, kind := range []string{kindFact, kindObservation} {
		count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kind})
		require.NoError(t, err)
		if kind == kindFact {
			require.EqualValues(t, 1, count)
		} else {
			require.EqualValues(t, 2, count)
		}
	}
}
