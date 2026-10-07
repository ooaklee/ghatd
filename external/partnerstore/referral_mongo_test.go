package partnerstore

import (
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/referral"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func frozenTerms() referral.TermsSnapshot {
	return referral.TermsSnapshot{RateBasisPoints: 2000, HoldDurationDays: 7, Currency: "EUR", CurrencyExponent: 2, TermsVersion: "fixture-terms", PolicyVersionIDs: []string{"fixture-policy"}, EligiblePlanIDs: []string{"fixture-plan"}}
}
func TestMongoReferralConcurrentStableLinks(t *testing.T) {
	// Different generated candidate IDs and codes contend for one active link.
	_, _, store, db, clock, ctx := mongoEarnings(t)
	r, err := NewReferralRepository(store)
	require.NoError(t, err)
	s, err := referral.NewService(r, clock, randomIDs{}, 30*24*time.Hour)
	require.NoError(t, err)
	s, err = s.WithAnalytics(referral.AnalyticsConfig{ObservationRetention: 24 * time.Hour})
	require.NoError(t, err)
	type result struct {
		l   referral.Link
		err error
	}
	out := make(chan result, 12)
	start := make(chan struct{})
	p := referral.PartnerState{PartnerID: "partner", CustomerID: "owner", CanAcquireReferrals: true}
	for i := 0; i < 12; i++ {
		go func() { <-start; l, err := s.IssueLink(ctx, p); out <- result{l, err} }()
	}
	close(start)
	winner := ""
	var link referral.Link
	for i := 0; i < 12; i++ {
		res := <-out
		require.NoError(t, res.err)
		if winner == "" {
			winner = res.l.ID
			link = res.l
		}
		require.Equal(t, winner, res.l.ID)
	}
	require.NoError(t, s.RetireLink(ctx, winner, "rotate after review", "operator"))
	replacement, err := s.IssueLink(ctx, p)
	require.NoError(t, err)
	require.NotEqual(t, winner, replacement.ID)
	retired, err := r.GetLinkByCode(ctx, link.Code)
	require.NoError(t, err)
	require.NotNil(t, retired.RetiredAt)
	require.Equal(t, "operator", retired.RetiredBy)
	collision := referral.Link{ID: "different-id", ProgramID: referral.ProgramID, PartnerID: "different-partner", Code: link.Code, CreatedAt: clock.Now()}
	require.ErrorIs(t, r.InsertLink(ctx, collision), referral.ErrCodeTaken)
	count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindLink})
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
	count, err = db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindLinkRevision})
	require.NoError(t, err)
	require.EqualValues(t, 3, count)
}
func TestMongoReferralAtomicRevisionAndFrozenBinding(t *testing.T) {
	// One stateful ownership correction proves that original payment binding and
	// the referral's audit history remain immutable after current ownership moves.
	_, _, store, db, clock, ctx := mongoEarnings(t)
	r, err := NewReferralRepository(store)
	require.NoError(t, err)
	s, err := referral.NewService(r, clock, randomIDs{}, 30*24*time.Hour)
	require.NoError(t, err)
	s, err = s.WithAnalytics(referral.AnalyticsConfig{ObservationRetention: 24 * time.Hour})
	require.NoError(t, err)
	first, err := s.AssignAttribution(ctx, reviewedCorrection(t, s, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: "original", CustomerID: "original-owner", CanAcquireReferrals: true}, ReferredCustomer: "customer", ActorID: "operator", Reason: "audited initial assignment", Terms: frozenTerms()}))
	require.NoError(t, err)
	binding, err := s.BindPayment(ctx, "customer", "original-payment", clock.Now())
	require.NoError(t, err)
	clock.now = clock.now.Add(time.Hour)
	req := reviewedCorrection(t, s, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: "new", CustomerID: "new-owner", CanAcquireReferrals: true}, ReferredCustomer: "customer", ActorID: "operator", Reason: "correct prospectively", ExpectedRevision: 1, ExpectedReferralID: first.ID, Terms: frozenTerms()})
	broken, err := NewReferralRepository(injectedStore{Store: store, failAt: 1})
	require.NoError(t, err)
	bs, err := referral.NewService(broken, clock, randomIDs{}, 30*24*time.Hour)
	require.NoError(t, err)
	bs, err = bs.WithAnalytics(referral.AnalyticsConfig{ObservationRetention: 24 * time.Hour})
	require.NoError(t, err)
	_, err = bs.AssignAttribution(ctx, req)
	require.ErrorIs(t, err, injectedFailure)
	head, err := r.GetReferralByCustomer(ctx, referral.ProgramID, "customer")
	require.NoError(t, err)
	require.Equal(t, first, head)
	updated, err := s.AssignAttribution(ctx, req)
	require.NoError(t, err)
	require.Equal(t, first.ID, updated.CorrectionOf)
	recovered, err := s.AssignAttribution(ctx, req)
	require.NoError(t, err)
	require.Equal(t, updated, recovered)
	replay, err := s.BindPayment(ctx, "customer", "original-payment", binding.EffectiveAt)
	require.NoError(t, err)
	require.Equal(t, binding, replay)
	future, err := s.BindPayment(ctx, "customer", "renewal-payment", clock.Now())
	require.NoError(t, err)
	require.Equal(t, "new", future.PartnerID)
	history, err := s.History(ctx, "customer")
	require.NoError(t, err)
	require.Len(t, history, 2)
	require.Equal(t, first, history[0])
	oldList, err := s.ListByPartner(ctx, "original", 100, "")
	require.NoError(t, err)
	require.Empty(t, oldList)
	newList, err := s.ListByPartner(ctx, "new", 100, "")
	require.NoError(t, err)
	require.Len(t, newList, 1)
	count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindReferralRevision})
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
}
func TestMongoReferralInsertRollback(t *testing.T) {
	cases := []struct {
		name   string
		failAt int
	}{{"reserved code", 1}, {"link", 2}, {"immutable link audit", 3}, {"active head", 4}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, db, clock, ctx := mongoEarnings(t)
			r, err := NewReferralRepository(injectedStore{Store: store, failAt: tc.failAt})
			require.NoError(t, err)
			s, err := referral.NewService(r, clock, randomIDs{}, 30*24*time.Hour)
			require.NoError(t, err)
			s, err = s.WithAnalytics(referral.AnalyticsConfig{ObservationRetention: 24 * time.Hour})
			require.NoError(t, err)
			_, err = s.IssueLink(ctx, referral.PartnerState{PartnerID: "partner", CustomerID: "owner", CanAcquireReferrals: true})
			require.ErrorIs(t, err, injectedFailure)
			count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{})
			require.NoError(t, err)
			require.Zero(t, count)
		})
	}
}
