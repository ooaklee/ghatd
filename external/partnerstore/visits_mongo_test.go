package partnerstore

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/referral"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

func mongoVisitSigner(t *testing.T, clock referral.Clock) *referral.EvidenceSigner {
	t.Helper()
	s, err := referral.NewEvidenceSigner(referral.EvidenceConfig{ProgramID: referral.ProgramID, ActiveKeyID: "fixture", Keys: map[string][]byte{"fixture": []byte("01234567890123456789012345678901")}, Window: 30 * 24 * time.Hour, VisitWindow: time.Hour, VisitIDs: randomIDs{}}, clock)
	require.NoError(t, err)
	return s
}

// Audit disposition: each named contention case has a fresh actual datastore.
// Concurrent callbacks/replay are the behaviour; one winning origin survives.
func TestMongoVisitConcurrentDeduplication(t *testing.T) {
	for _, tc := range []struct {
		name  string
		calls int
	}{{"two_competing_requests", 2}, {"many_competing_requests", 12}} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, db, clock, ctx := mongoEarnings(t)
			repo, err := NewReferralRepository(store)
			require.NoError(t, err)
			svc, err := referral.NewService(repo, clock, randomIDs{}, 30*24*time.Hour)
			require.NoError(t, err)
			svc, err = svc.WithAnalytics(referral.AnalyticsConfig{ObservationRetention: 24 * time.Hour})
			require.NoError(t, err)
			link, err := svc.IssueLink(ctx, referral.PartnerState{PartnerID: "partner", CustomerID: "owner", CanAcquireReferrals: true})
			require.NoError(t, err)
			signer := mongoVisitSigner(t, clock)
			token, identity, err := signer.IssueVisit(link)
			require.NoError(t, err)
			type result struct {
				visit referral.VisitObservation
				err   error
			}
			results := make(chan result, tc.calls)
			start := make(chan struct{})
			for i := 0; i < tc.calls; i++ {
				go func() {
					<-start
					v, e := svc.ObserveVisit(ctx, referral.VisitRequest{Code: link.Code, Identity: identity, Consented: true})
					results <- result{v, e}
				}()
			}
			close(start)
			eligible := 0
			first := ""
			for i := 0; i < tc.calls; i++ {
				v := <-results
				require.NoError(t, v.err)
				if first == "" {
					first = v.visit.Eligible.ID
				}
				require.Equal(t, first, v.visit.Eligible.ID)
				require.Equal(t, first, v.visit.Click.MeasuredClickID)
				if v.visit.Click.Classification == referral.VisitEligible {
					eligible++
				} else {
					require.Equal(t, referral.VisitDuplicate, v.visit.Click.Classification)
				}
			}
			require.Equal(t, 1, eligible)
			receipt, err := repo.GetVisitReceipt(ctx, link.ID, identity.Digest)
			require.NoError(t, err)
			require.Equal(t, first, receipt.MeasuredClickID)
			count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindVisitReceipt})
			require.NoError(t, err)
			require.EqualValues(t, 1, count)
			count, err = db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindClick})
			require.NoError(t, err)
			require.EqualValues(t, tc.calls, count)
			day, err := repo.GetVisitDay(ctx, link.ID, clock.Now().UTC().Truncate(24*time.Hour))
			require.NoError(t, err)
			require.EqualValues(t, tc.calls, day.Counts.Observations)
			require.EqualValues(t, 1, day.Counts.EligibleMeasuredVisits)
			require.EqualValues(t, tc.calls-1, day.Counts.DuplicateObservations)
			// Encrypted payload and opaque metadata retain neither signed token nor raw
			// nonce; inspecting the owning decoded receipt exposes only a keyed digest.
			cursor, err := db.Collection("ghatd_owned_records").Find(ctx, bson.M{"kind": bson.M{"$in": []string{kindVisitReceipt, kindClick, kindVisitDay}}})
			require.NoError(t, err)
			t.Cleanup(func() { _ = cursor.Close(context.Background()) })
			var raw []bson.M
			require.NoError(t, cursor.All(ctx, &raw))
			require.NotContains(t, fmt.Sprint(raw), token)
			require.NotContains(t, fmt.Sprint(raw), identity.Digest)
			frozen, err := repo.GetClick(ctx, link.ID, first)
			require.NoError(t, err)
			require.Empty(t, frozen.UserAgent)
			replay, err := svc.ObserveVisit(ctx, referral.VisitRequest{Code: link.Code, Identity: identity, Consented: true})
			require.NoError(t, err)
			require.Equal(t, frozen, replay.Eligible)
			require.Equal(t, referral.VisitDuplicate, replay.Click.Classification)
		})
	}
}

func TestMongoVisitRollbackAndUncertainRecovery(t *testing.T) {
	for _, tc := range []struct {
		name      string
		failAt    int
		uncertain bool
		want      error
		committed int64
	}{{"receipt_write_failure", 1, false, injectedFailure, 0}, {"daily_count_write_failure", 2, false, injectedFailure, 0}, {"observation_write_failure", 3, false, injectedFailure, 0}, {"lost_commit_ack_reuses_first_origin", 0, true, referral.ErrUncertain, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, store, db, clock, ctx := mongoEarnings(t)
			repo, err := NewReferralRepository(store)
			require.NoError(t, err)
			svc, err := referral.NewService(repo, clock, randomIDs{}, 30*24*time.Hour)
			require.NoError(t, err)
			svc, err = svc.WithAnalytics(referral.AnalyticsConfig{ObservationRetention: 24 * time.Hour})
			require.NoError(t, err)
			link, err := svc.IssueLink(ctx, referral.PartnerState{PartnerID: "partner", CustomerID: "owner", CanAcquireReferrals: true})
			require.NoError(t, err)
			_, identity, err := mongoVisitSigner(t, clock).IssueVisit(link)
			require.NoError(t, err)
			fault, err := NewReferralRepository(injectedStore{Store: store, failAt: tc.failAt, uncertain: tc.uncertain})
			require.NoError(t, err)
			failed, err := referral.NewService(fault, clock, randomIDs{}, 30*24*time.Hour)
			require.NoError(t, err)
			failed, err = failed.WithAnalytics(referral.AnalyticsConfig{ObservationRetention: 24 * time.Hour})
			require.NoError(t, err)
			req := referral.VisitRequest{Code: link.Code, Identity: identity, Consented: true}
			out, err := failed.ObserveVisit(ctx, req)
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, out)
			for _, kind := range []string{kindVisitReceipt, kindClick, kindVisitDay} {
				n, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kind})
				require.NoError(t, err)
				require.Equal(t, tc.committed, n)
			}
			recovered, err := svc.ObserveVisit(ctx, req)
			require.NoError(t, err)
			if tc.uncertain {
				require.Equal(t, referral.VisitDuplicate, recovered.Click.Classification)
			} else {
				require.Equal(t, referral.VisitEligible, recovered.Click.Classification)
			}
			receipt, err := repo.GetVisitReceipt(ctx, link.ID, identity.Digest)
			require.NoError(t, err)
			require.Equal(t, recovered.Eligible.ID, receipt.MeasuredClickID)
			day, err := repo.GetVisitDay(ctx, link.ID, clock.Now().UTC().Truncate(24*time.Hour))
			require.NoError(t, err)
			require.EqualValues(t, 1+tc.committed, day.Counts.Observations)
			require.EqualValues(t, 1, day.Counts.EligibleMeasuredVisits)
			require.EqualValues(t, tc.committed, day.Counts.DuplicateObservations)
			// A bound visit adapter cannot open another guarded transaction or write a
			// receipt into a different link partition.
			require.NoError(t, repo.WithVisitTransaction(ctx, link.ID, func(tx referral.Repository) error {
				require.ErrorIs(t, tx.WithVisitTransaction(ctx, link.ID, func(referral.Repository) error { return nil }), referral.ErrInvalid)
				require.ErrorIs(t, tx.WithAttributionTransaction(ctx, referral.ProgramID, "customer", func(referral.Repository) error { return nil }), referral.ErrInvalid)
				receipt.LinkID = "foreign-link"
				require.ErrorIs(t, tx.InsertVisitReceipt(ctx, receipt), referral.ErrInvalid)
				return nil
			}))
		})
	}
}

type visitOutageService struct{ *referral.Service }

func (s visitOutageService) ObserveVisit(context.Context, referral.VisitRequest) (referral.VisitObservation, error) {
	return referral.VisitObservation{}, referral.ErrUnavailable
}

// Audit disposition: named measured/unmeasured cases exercise actual owning
// user creation, encrypted signup capture, manager lock, revenue and replay.
// These service journeys are not provider HTTP or whole-platform browser E2E.
func TestMongoMeasuredVisitSignupAndRevenue(t *testing.T) {
	for _, tc := range []struct {
		name   string
		outage bool
	}{{"measured_signup_retains_origin_and_replays", false}, {"analytics_outage_does_not_block_signup_or_earnings", true}} {
		t.Run(tc.name, func(t *testing.T) {
			f := owningWorkerFixture(t)
			f.signer = mongoVisitSigner(t, f.clock)
			link, err := f.referrals.IssueLink(f.ctx, referral.PartnerState{PartnerID: f.partner.ID, CustomerID: f.partner.CustomerID, CanAcquireReferrals: true})
			require.NoError(t, err)
			m := f.manager(t, false)
			if tc.outage {
				m, err = partnermanager.NewManager(partnermanager.Dependencies{Program: f.program, Referral: visitOutageService{f.referrals}, Earnings: f.earnings, Identity: f.identity, Authority: f.authority, Groups: workerGroups{}, Revenue: f.revenue, Evidence: f.signer, Clock: f.clock, Controls: partnermanager.Controls{Attribution: true, Accrual: true}})
				require.NoError(t, err)
			}
			prepared, err := m.PrepareVisit(f.ctx, partnermanager.PrepareVisitRequest{Code: link.Code, Consented: true})
			require.NoError(t, err)
			evidence, err := f.signer.VerifyCurrent(prepared.Evidence)
			require.NoError(t, err)
			if tc.outage {
				require.Equal(t, "unavailable", prepared.Measurement)
				require.Empty(t, evidence.MeasuredClickID)
			} else {
				require.Equal(t, referral.VisitEligible, prepared.Measurement)
				require.NotEmpty(t, evidence.MeasuredClickID)
			}
			customer := f.signup(t, "visit-member@example.test", prepared.Evidence)
			accepted, err := m.ConsumeSignup(f.ctx, "fixture-worker", customer)
			require.NoError(t, err)
			require.Equal(t, evidence.MeasuredClickID, accepted.SourceMeasuredClickID)
			current, err := f.referrals.GetReferralForCustomer(f.ctx, customer)
			require.NoError(t, err)
			require.Equal(t, accepted, current)
			paid := f.paid(t, customer)
			_, err = m.ProcessRevenueFact(f.ctx, "fixture-worker", paid.ID)
			require.NoError(t, err)
			// Admission may close later. An accepted immutable signup and verified
			// payment still reconcile without observing the optional visit datastore.
			paused := f.manager(t, true)
			replay, err := paused.ConsumeSignup(f.ctx, "fixture-worker", customer)
			require.NoError(t, err)
			require.Equal(t, accepted, replay)
			_, err = paused.ProcessRevenueFact(f.ctx, "fixture-worker", paid.ID)
			require.NoError(t, err)
			var row recordstore.Record
			require.NoError(t, f.store.Read(f.ctx, func(tx recordstore.Tx) error {
				var e error
				row, e = tx.Get(f.ctx, kindReferralRevision, accepted.ID)
				return e
			}))
			var stored storedReferral
			require.NoError(t, row.Decode(&stored))
			require.Equal(t, accepted.SourceMeasuredClickID, stored.value().SourceMeasuredClickID)
		})
	}
}
