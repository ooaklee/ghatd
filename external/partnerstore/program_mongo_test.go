package partnerstore

import (
	"errors"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func programConfig() partnerprogram.Config {
	return partnerprogram.Config{DefaultRateBasisPoints: 2000, DefaultHoldDays: 7, Currency: "EUR", CurrencyExponent: 2, TermsVersion: "fixture-terms", AttributionWindow: 30 * 24 * time.Hour, EligiblePlanIDs: []string{"fixture-plan"}}
}
func TestMongoProgramConcurrentEnrollment(t *testing.T) {
	// The competing insert/unique reference race is the complete scenario. Each
	// command generates its own participant ID; exactly one durable owner wins.
	_, _, store, db, clock, ctx := mongoEarnings(t)
	repo, err := NewProgramRepository(store)
	require.NoError(t, err)
	svc, err := partnerprogram.NewService(repo, clock, randomIDs{}, programConfig())
	require.NoError(t, err)
	type result struct {
		p   partnerprogram.Partner
		err error
	}
	out := make(chan result, 12)
	start := make(chan struct{})
	for i := 0; i < 12; i++ {
		go func() {
			<-start
			p, err := svc.Enroll(ctx, partnerprogram.EnrollRequest{CustomerID: "owner", AcceptedTermsVersion: "fixture-terms"})
			out <- result{p, err}
		}()
	}
	close(start)
	winner := ""
	for i := 0; i < 12; i++ {
		res := <-out
		require.NoError(t, res.err)
		if winner == "" {
			winner = res.p.ID
		}
		require.Equal(t, winner, res.p.ID)
	}
	p, err := repo.GetPartnerByCustomer(ctx, partnerprogram.ProgramID, "owner")
	require.NoError(t, err)
	require.Equal(t, winner, p.ID)
	for _, kind := range []string{kindPartner, kindPartnerCustomer, kindPartnerRevision} {
		count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kind})
		require.NoError(t, err)
		require.EqualValues(t, 1, count, kind)
	}
}
func TestMongoProgramEnrollmentRollback(t *testing.T) {
	cases := []struct {
		name   string
		failAt int
	}{{"participant", 1}, {"immutable audit", 2}, {"customer reference", 3}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, db, clock, ctx := mongoEarnings(t)
			r, err := NewProgramRepository(injectedStore{Store: store, failAt: tc.failAt})
			require.NoError(t, err)
			s, err := partnerprogram.NewService(r, clock, randomIDs{}, programConfig())
			require.NoError(t, err)
			_, err = s.Enroll(ctx, partnerprogram.EnrollRequest{CustomerID: "owner", AcceptedTermsVersion: "fixture-terms"})
			require.ErrorIs(t, err, injectedFailure)
			count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{})
			require.NoError(t, err)
			require.Zero(t, count)
			_, err = r.GetPartnerByCustomer(ctx, partnerprogram.ProgramID, "owner")
			require.ErrorIs(t, err, partnerprogram.ErrNotFound)
		})
	}
}
func TestMongoProgramPolicyAndDestinationCAS(t *testing.T) {
	cases := []struct {
		name        string
		destination bool
	}{{"immutable policy scope", false}, {"immutable payout destination", true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, db, clock, ctx := mongoEarnings(t)
			r, err := NewProgramRepository(store)
			require.NoError(t, err)
			s, err := partnerprogram.NewService(r, clock, randomIDs{}, programConfig())
			require.NoError(t, err)
			start := make(chan struct{})
			out := make(chan error, 2)
			for i := 0; i < 2; i++ {
				go func() {
					<-start
					var err error
					if tc.destination {
						_, err = s.UpdatePayoutDestination(ctx, partnerprogram.DestinationRequest{CustomerID: "owner", Method: "paypal", PayPalEmail: "fixture@example.test", ExpectedVersion: 0})
					} else {
						_, err = s.PublishPolicy(ctx, partnerprogram.PublishPolicyRequest{ActorID: "operator", Draft: partnerprogram.PolicyDraft{Scope: "global", RateBasisPoints: 2000, HoldDays: 7, Currency: "EUR", EligiblePlanIDs: []string{"fixture-plan"}, TermsVersion: "fixture-terms", EffectiveFrom: clock.Now().Add(-time.Hour)}})
					}
					out <- err
				}()
			}
			close(start)
			a, b := <-out, <-out
			require.True(t, (a == nil && errors.Is(b, partnerprogram.ErrStaleWrite)) || (b == nil && errors.Is(a, partnerprogram.ErrStaleWrite)), "%v / %v", a, b)
			kind := kindPolicy
			if tc.destination {
				kind = kindDestination
				d, err := r.GetDestination(ctx, "owner")
				require.NoError(t, err)
				require.EqualValues(t, 1, d.Version)
				require.Equal(t, "fixture@example.test", d.Email)
			}
			count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kind})
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
		})
	}
}
func TestMongoProgramStatusAuditRollsBack(t *testing.T) {
	// The existing participant must survive an audit insert failure, then a new
	// attempt may commit the head and immutable revision together.
	_, _, store, db, clock, ctx := mongoEarnings(t)
	r, err := NewProgramRepository(store)
	require.NoError(t, err)
	s, err := partnerprogram.NewService(r, clock, randomIDs{}, programConfig())
	require.NoError(t, err)
	p, err := s.Enroll(ctx, partnerprogram.EnrollRequest{CustomerID: "owner", AcceptedTermsVersion: "fixture-terms"})
	require.NoError(t, err)
	broken, err := NewProgramRepository(injectedStore{Store: store, failAt: 1})
	require.NoError(t, err)
	bs, err := partnerprogram.NewService(broken, clock, randomIDs{}, programConfig())
	require.NoError(t, err)
	req := partnerprogram.StatusChangeRequest{ActorID: "operator", PartnerID: p.ID, ExpectedRevision: p.Revision, NewStatus: partnerprogram.StatusSuspended, Reason: "review"}
	_, err = bs.ChangeStatus(ctx, req)
	require.ErrorIs(t, err, injectedFailure)
	unchanged, err := r.GetPartnerByID(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, p, unchanged)
	updated, err := s.ChangeStatus(ctx, req)
	require.NoError(t, err)
	require.EqualValues(t, 2, updated.Revision)
	_, err = s.ChangeStatus(ctx, req)
	require.ErrorIs(t, err, partnerprogram.ErrStaleWrite)
	count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindPartnerRevision})
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
}
