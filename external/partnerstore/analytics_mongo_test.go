package partnerstore

import (
	"context"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/referral"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

type analyticsReadStore struct {
	recordstore.Store
	reads    int
	retry    bool
	failKind string
}
type analyticsReadTx struct {
	recordstore.Tx
	failKind string
}

func (t analyticsReadTx) Find(ctx context.Context, q recordstore.Query) ([]recordstore.Record, error) {
	if q.Kind == t.failKind {
		return nil, recordstore.ErrUnavailable
	}
	return t.Tx.Find(ctx, q)
}
func (s *analyticsReadStore) Read(ctx context.Context, fn func(recordstore.Tx) error) error {
	s.reads++
	return s.Store.Read(ctx, func(tx recordstore.Tx) error {
		bound := analyticsReadTx{tx, s.failKind}
		if s.retry {
			if err := fn(bound); err != nil {
				return err
			}
		}
		return fn(bound)
	})
}

// Audit disposition: named actual encrypted-datastore journeys verify retained
// original conversion, full global counts, retry reset and all-or-nothing reads.
func TestMongoReferralAnalyticsSnapshot(t *testing.T) {
	for _, tc := range []struct {
		name     string
		retry    bool
		failKind string
	}{{"one_complete_owning_snapshot", false, ""}, {"reentered_read_callback_resets_result", true, ""}, {"membership_query_failure_discards_preceding_clicks", false, kindReferralRelationship}, {"history_query_failure_discards_preceding_members", false, kindReferralRevision}} {
		t.Run(tc.name, func(t *testing.T) {
			f := owningWorkerFixture(t)
			f.signer = mongoVisitSigner(t, f.clock)
			link, err := f.referrals.IssueLink(f.ctx, referral.PartnerState{PartnerID: f.partner.ID, CustomerID: f.partner.CustomerID, CanAcquireReferrals: true})
			require.NoError(t, err)
			m := f.manager(t, false)
			first, err := m.PrepareVisit(f.ctx, partnermanager.PrepareVisitRequest{Code: link.Code, Consented: true})
			require.NoError(t, err)
			repeat, err := m.PrepareVisit(f.ctx, partnermanager.PrepareVisitRequest{Code: link.Code, Consented: true, PriorEvidence: first.Evidence, PriorVisit: first.VisitCookie})
			require.NoError(t, err)
			require.Equal(t, referral.VisitDuplicate, repeat.Measurement)
			customer := f.signup(t, "analytics-member@example.test", first.Evidence)
			original, err := m.ConsumeSignup(f.ctx, "worker", customer)
			require.NoError(t, err)
			f.clock.now = f.clock.now.Add(time.Hour)
			_, err = f.referrals.AssignAttribution(f.ctx, reviewedCorrection(t, f.referrals, referral.CorrectionRequest{Partner: referral.PartnerState{PartnerID: "other-partner", CustomerID: "other-owner", CanAcquireReferrals: true}, ReferredCustomer: customer, ActorID: "operator", Reason: "reviewed prospective move", ExpectedRevision: 1, ExpectedReferralID: original.ID, Terms: frozenTerms()}))
			require.NoError(t, err)
			to := original.LockedAt.Add(time.Minute)
			q := referral.AnalyticsQuery{Limit: 1, To: &to}
			storage := &analyticsReadStore{Store: f.store, retry: tc.retry, failKind: tc.failKind}
			repo, err := NewReferralRepository(storage)
			require.NoError(t, err)
			s, err := referral.NewService(repo, f.clock, randomIDs{}, 30*24*time.Hour)
			require.NoError(t, err)
			s, err = s.WithAnalytics(referral.AnalyticsConfig{ObservationRetention: 24 * time.Hour})
			require.NoError(t, err)
			out, err := s.GetAnalytics(f.ctx, f.partner.ID, q)
			require.Equal(t, 1, storage.reads)
			if tc.failKind != "" {
				require.ErrorIs(t, err, referral.ErrUnavailable)
				require.Empty(t, out)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, 2, out.Visits.Observations)
			require.EqualValues(t, 1, out.Visits.EligibleMeasuredVisits)
			require.EqualValues(t, 1, out.Visits.ConvertedMeasuredVisits)
			require.EqualValues(t, 1, out.Visits.SignupsFromVisitCohort)
			require.EqualValues(t, 1, out.Signups.MeasuredSignups)
			require.Equal(t, 1, out.LifetimeRelationships)
			require.Zero(t, out.CurrentRelationships)
			require.Equal(t, 1, out.RetainedRelationships)
			require.Zero(t, out.Coverage.MissingMeasuredSignupOrigins)
			// Empty new-owner click cohort is honest: a correction does not relocate
			// the earlier owner's measured signup or manufacture another conversion.
			other, err := s.GetAnalytics(f.ctx, "other-partner", referral.AnalyticsQuery{Limit: 1})
			require.NoError(t, err)
			require.Zero(t, other.Visits.EligibleMeasuredVisits)
			require.Zero(t, other.Signups.MeasuredSignups)
			require.EqualValues(t, 1, other.Signups.CorrectionAcquisitionEvents)
			require.Equal(t, 1, other.CurrentRelationships)
		})
	}
}

// Standalone audit exception: a single retained datastore grows by one record
// across its complete-report boundary. Rebuilding two 10,000-row encrypted
// fixtures would duplicate the expensive preparation rather than test another
// scenario; the persisted before/after transition is the behaviour under test.
func TestMongoAnalyticsExactCapacityThenOverflow(t *testing.T) {
	_, _, store, _, clock, fixtureCtx := mongoEarnings(t)
	repo, err := NewReferralRepository(store)
	require.NoError(t, err)
	svc, err := referral.NewService(repo, clock, randomIDs{}, 30*24*time.Hour)
	require.NoError(t, err)
	svc, err = svc.WithAnalytics(referral.AnalyticsConfig{ObservationRetention: 24 * time.Hour})
	require.NoError(t, err)
	reportAt := clock.now
	clock.now = clock.now.Add(-10001 * 24 * time.Hour)
	link, err := svc.IssueLink(fixtureCtx, referral.PartnerState{PartnerID: "partner", CustomerID: "owner", CanAcquireReferrals: true})
	require.NoError(t, err)
	clock.now = reportAt
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	// Bucket capacity is independent of traffic volume and raw-row expiry.
	require.NoError(t, store.Transact(ctx, clicksPartition(link.ID), func(tx recordstore.Tx) error {
		for i := 0; i < referral.AnalyticsCapacity; i++ {
			day := reportAt.UTC().Truncate(24 * time.Hour).Add(time.Duration(i-referral.AnalyticsCapacity) * 24 * time.Hour)
			v := referral.VisitDay{ProgramID: referral.ProgramID, LinkID: link.ID, Day: day, Revision: 1, Counts: referral.VisitMetrics{Observations: 1, UnmeasuredObservations: 1}}
			row, err := recordstore.NewRecord(kindVisitDay, identity(referral.ProgramID, link.ID, visitDayKey(day)), clicksPartition(link.ID), 1, v)
			if err != nil {
				return err
			}
			row.State = visitDayKey(day)
			if err := tx.Insert(ctx, row); err != nil {
				return err
			}
		}
		return nil
	}))
	exact, err := svc.GetAnalytics(ctx, "partner", referral.AnalyticsQuery{Limit: 1})
	require.NoError(t, err)
	require.EqualValues(t, referral.AnalyticsCapacity, exact.Visits.Observations)
	require.EqualValues(t, referral.AnalyticsCapacity, exact.Coverage.LegacyUnmeasuredObservations)
	require.Zero(t, exact.Visits.EligibleMeasuredVisits)
	_, err = svc.ObserveClick(ctx, link.Code, "")
	require.NoError(t, err)
	overflow, err := svc.GetAnalytics(ctx, "partner", referral.AnalyticsQuery{Limit: 1})
	require.ErrorIs(t, err, referral.ErrCapacity)
	require.Empty(t, overflow)
	// Refusing a report never removes ownership/link state or changes financial
	// admission; the same link remains available from the owning service.
	current, err := repo.GetLinkByCode(ctx, link.Code)
	require.NoError(t, err)
	require.Equal(t, link, current)
}
