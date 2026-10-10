package partnerstore

import (
	"context"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/referral"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// Standalone audit exception: one physical TTL lifecycle must observe the same
// encrypted database before and after Mongo's asynchronous monitor runs. The
// retained financial/anonymous records and delayed signup are part of that
// transition, not independent fixtures or a timer-based admission policy.
func TestMongoRawVisitExpirationPreservesCountsAttributionAndMoney(t *testing.T) {
	earnings, _, store, db, clock, setupCtx := mongoEarnings(t)
	accrue(t, earnings, setupCtx, clock, "retained-financial-payment", 10000)
	beforeMoney, err := earnings.Balances(setupCtx, "partner")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	t.Cleanup(cancel)
	indexes, err := db.Collection("ghatd_owned_records").Indexes().List(ctx)
	require.NoError(t, err)
	var definitions []bson.M
	require.NoError(t, indexes.All(ctx, &definitions))
	found := false
	for _, index := range definitions {
		if index["name"] != "owned_record_expiration" {
			continue
		}
		found = true
		key, ok := index["key"].(bson.D)
		require.True(t, ok)
		require.Len(t, key, 1)
		require.Equal(t, "expires_at", key[0].Key)
		require.EqualValues(t, 0, index["expireAfterSeconds"])
		require.NotNil(t, index["partialFilterExpression"])
	}
	require.True(t, found, "additive single-field absolute TTL index must exist")
	repo, err := NewReferralRepository(store)
	require.NoError(t, err)
	svc, err := referral.NewService(repo, clock, randomIDs{}, 30*24*time.Hour)
	require.NoError(t, err)
	svc, err = svc.WithAnalytics(referral.AnalyticsConfig{ObservationRetention: 24 * time.Hour})
	require.NoError(t, err)
	clock.now = time.Now().UTC().Add(-25 * time.Hour)
	partner := referral.PartnerState{PartnerID: "partner", CustomerID: "owner", CanAcquireReferrals: true}
	link, err := svc.IssueLink(ctx, partner)
	require.NoError(t, err)
	signer := mongoVisitSigner(t, clock)
	_, identity, err := signer.IssueVisit(link)
	require.NoError(t, err)
	visit, err := svc.ObserveVisit(ctx, referral.VisitRequest{Code: link.Code, Identity: identity, Consented: true})
	require.NoError(t, err)
	token, issued, err := signer.IssueMeasured(link, visit.Eligible)
	require.NoError(t, err)
	signupAt := clock.Now().Add(time.Minute)
	verified, err := signer.Verify(token, signupAt)
	require.NoError(t, err)
	require.Equal(t, issued, verified)
	clock.now = time.Now().UTC()
	kept, err := svc.ObserveVisit(ctx, referral.VisitRequest{Code: link.Code, Consented: true, KnownBot: true})
	require.NoError(t, err)
	q := referral.AnalyticsQuery{Limit: 100}
	before, err := svc.GetAnalytics(ctx, "partner", q)
	require.NoError(t, err)
	require.EqualValues(t, 2, before.Visits.Observations)
	// TTL is eventual. The expired timestamp never substitutes for a consent,
	// nonce-validity or account-creation authority decision in business code.
	require.Eventually(t, func() bool {
		count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"$or": bson.A{bson.M{"kind": kindClick, "id": visit.Click.ID}, bson.M{"kind": kindVisitReceipt}}})
		return err == nil && count == 0
	}, 90*time.Second, 250*time.Millisecond, "Mongo TTL must physically remove expired raw observations and receipts")
	_, err = repo.GetClick(ctx, link.ID, visit.Click.ID)
	require.ErrorIs(t, err, referral.ErrNotFound)
	_, err = repo.GetVisitReceipt(ctx, link.ID, identity.Digest)
	require.ErrorIs(t, err, referral.ErrNotFound)
	still, err := repo.GetClick(ctx, link.ID, kept.Click.ID)
	require.NoError(t, err)
	require.Equal(t, kept.Click, still)
	after, err := svc.GetAnalytics(ctx, "partner", q)
	require.NoError(t, err)
	require.Equal(t, before, after, "raw cleanup must not alter persistent totals or revision")
	// The trusted signup occurred within the signed window. Delayed processing
	// never looks up an already-expired analytics observation to admit it.
	r, err := svc.LockAttribution(ctx, partner, link, referral.Eligibility{ReferredCustomer: "new-customer", SignupID: "retained-signup", IsIndividual: true, At: signupAt, Evidence: verified, Terms: frozenTerms()})
	require.NoError(t, err)
	require.Equal(t, visit.Click.ID, r.SourceMeasuredClickID)
	require.Equal(t, visit.Click.OccurredAt, *r.SourceMeasuredOccurredAt)
	report, err := svc.GetAnalytics(ctx, "partner", q)
	require.NoError(t, err)
	require.EqualValues(t, 1, report.Visits.ConvertedMeasuredVisits)
	require.EqualValues(t, 1, report.Signups.MeasuredSignups)
	require.Zero(t, report.Coverage.MissingMeasuredSignupOrigins)
	from, to := visit.Click.OccurredAt.Add(-time.Minute), visit.Click.OccurredAt.Add(time.Minute)
	partial, err := svc.GetAnalytics(ctx, "partner", referral.AnalyticsQuery{Limit: 100, From: &from, To: &to})
	require.ErrorIs(t, err, referral.ErrGranularity)
	require.Empty(t, partial)
	financial, err := earnings.Balances(ctx, "partner")
	require.NoError(t, err)
	require.Equal(t, beforeMoney, financial)
	accidental, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"expires_at": bson.M{"$exists": true}, "kind": bson.M{"$nin": bson.A{kindClick, kindVisitReceipt}}})
	require.NoError(t, err)
	require.Zero(t, accidental, "ownership, day counts and financial records must not receive expiry metadata")
}
