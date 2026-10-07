package partnermanager

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/referral"
	"github.com/stretchr/testify/require"
)

type conversionReportRepo struct {
	referral.Repository
	snapshot referral.ConversionSnapshot
	calls    int
	mode     string
	revoke   *authorityStub
}

func (r *conversionReportRepo) ReadConversionSnapshot(ctx context.Context, _, _ string, _ referral.AnalyticsQuery) (referral.ConversionSnapshot, error) {
	r.calls++
	if err := ctx.Err(); err != nil {
		return referral.ConversionSnapshot{}, err
	}
	if r.calls == 2 {
		if r.mode == "changed_binding" {
			for customer := range r.snapshot.Bindings {
				r.snapshot.Bindings[customer] = []referral.PaymentAttribution{}
			}
		}
		if r.mode == "changed_day" {
			r.snapshot.Analytics.Days[0].Revision++
			r.snapshot.Analytics.Days[0].Counts.Observations++
			r.snapshot.Analytics.Days[0].Counts.UnmeasuredObservations++
		}
		if r.revoke != nil {
			r.revoke.deny = true
		}
	}
	return r.snapshot, nil
}

func newConversionReportFixture(t *testing.T) (*paidReportFixture, *conversionReportRepo) {
	t.Helper()
	f := newPaidReportFixture(t)
	at := f.manager.deps.Clock.Now()
	visitAt := at.Add(-10 * time.Minute)
	link := referral.Link{ID: "own-link", Code: "own-code", ProgramID: referral.ProgramID, PartnerID: "partner", CreatedAt: at.Add(-time.Hour)}
	head := referral.Referral{ID: "private-revision", ProgramID: referral.ProgramID, PartnerID: "partner", ReferredCustomer: f.original.PrincipalID, SignupID: "private-signup", Revision: 1, SourceKind: "click", SourceCode: link.Code, SourceMeasuredClickID: "private-origin", SourceMeasuredOccurredAt: &visitAt, EvidenceDigest: strings.Repeat("a", 64), LockedAt: at, PostedAt: at, TermsSnapshot: f.referral.page.Items[0].Periods[0].Terms}
	member := referral.RelationshipSnapshot{ID: referral.RelationshipReferenceID(referral.ProgramID, "partner", head.ReferredCustomer), ProgramID: referral.ProgramID, PartnerID: "partner", ReferredCustomer: head.ReferredCustomer, FirstReferralID: head.ID, Head: head, History: []referral.Referral{head}}
	click := referral.Click{ID: head.SourceMeasuredClickID, LinkID: link.ID, Code: link.Code, OccurredAt: visitAt, Classification: referral.VisitEligible, MeasuredClickID: head.SourceMeasuredClickID, VisitDigest: strings.Repeat("b", 64)}
	r := &conversionReportRepo{snapshot: referral.ConversionSnapshot{Analytics: referral.AnalyticsSnapshot{Links: []referral.Link{link}, Clicks: []referral.Click{click}, Days: []referral.VisitDay{{ProgramID: referral.ProgramID, LinkID: link.ID, Day: at.Truncate(24 * time.Hour), Revision: 1, Counts: referral.VisitMetrics{Observations: 1, EligibleMeasuredVisits: 1}}}, Relationships: []referral.RelationshipSnapshot{member}}, Bindings: map[string][]referral.PaymentAttribution{head.ReferredCustomer: append([]referral.PaymentAttribution(nil), f.referral.snapshot.Bindings...)}}}
	s, err := referral.NewService(r, f.clock, &visitIDs{}, time.Hour)
	require.NoError(t, err)
	f.manager.deps.Referral = s
	return f, r
}

func conversionPayment(t *testing.T, f *paidReportFixture, r *conversionReportRepo, id, customer, plan, referralID, partner string, at time.Time) {
	t.Helper()
	raw := f.original
	raw.PaymentID, raw.InvoiceID, raw.AllocationID = "pi_"+id, "in_"+id, "il_"+id
	raw.PrincipalID, raw.ProviderCustomerID, raw.SubscriptionID = customer, "cus_"+id, "sub_"+id
	raw.PlanID, raw.EffectiveAt = plan, at
	o, err := f.source.AcceptVerified(context.Background(), billing.VerifiedRevenueRequest{Scope: raw.Scope, EnvelopeID: "evt_" + id, Facts: []billing.RevenueFact{raw}})
	require.NoError(t, err)
	row, err := f.source.GetRevenueFact(context.Background(), o.FactIDs[0])
	require.NoError(t, err)
	var terms referral.TermsSnapshot
	for _, member := range r.snapshot.Analytics.Relationships {
		for _, revision := range member.History {
			if revision.ID == referralID {
				terms = revision.TermsSnapshot
			}
		}
	}
	r.snapshot.Bindings[customer] = append(r.snapshot.Bindings[customer], referral.PaymentAttribution{ID: "binding_" + id, ProgramID: referral.ProgramID, PaymentID: row.ID, ReferredCustomer: customer, ReferralID: referralID, PartnerID: partner, EffectiveAt: at, BoundAt: f.clock.Now(), Terms: terms})
}

// Audit disposition: named cohort journeys use the ACTUAL referral validator
// and ACTUAL billing owner/adapter, with isolated typed complete-read fixtures.
// Native coupling and snapshot races are separately exercised in partnerstore.
func TestOriginalConversionCohorts(t *testing.T) {
	for _, tc := range []struct {
		name, mode                                             string
		measured, unmeasured, manual, corrections, visits, net int64
		visitRate                                              bool
	}{
		{"one_paid_original_signup_and_visit", "", 1, 0, 0, 0, 1, 1, true},
		{"payment_after_to_converts_signup_cohort", "later_payment", 1, 0, 0, 0, 0, 1, false},
		{"signup_after_to_converts_visit_cohort", "later_signup", 0, 0, 0, 0, 1, 0, true},
		{"two_accounts_one_origin_convert_one_visit", "two_people", 2, 0, 0, 0, 1, 2, true},
		{"renewals_do_not_add_conversions", "renewal", 1, 0, 0, 0, 1, 1, true},
		{"full_refund_keeps_historical_conversion", "refund", 1, 0, 0, 0, 1, 0, true},
		{"former_owner_keeps_original_conversion", "former", 1, 0, 0, 0, 1, 1, true},
		{"unmeasured_signup_never_creates_visit_conversion", "unmeasured", 0, 1, 0, 0, 0, 0, true},
		{"manual_initial_is_separate_from_click_conversion", "manual", 0, 0, 1, 0, 0, 0, true},
		{"new_owner_converts_correction_event_only", "correction", 0, 0, 0, 1, 0, 0, true},
		{"missing_origin_withholds_visit_rate", "missing", 1, 0, 0, 0, 0, 1, false},
		{"unbound_source_payment_does_not_convert", "unbound", 0, 0, 0, 0, 0, 0, true},
		{"frozen_ineligible_plan_does_not_convert", "plan", 0, 0, 0, 0, 0, 0, true},
		{"payment_at_recurrence_end_does_not_convert", "recurrence", 0, 0, 0, 0, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r := newConversionReportFixture(t)
			at := f.manager.deps.Clock.Now()
			q := referral.AnalyticsQuery{Limit: 1}
			member := &r.snapshot.Analytics.Relationships[0]
			switch tc.mode {
			case "later_payment":
				to := at.Add(30 * time.Second)
				q.From, q.To = &at, &to
			case "later_signup":
				to := at.Add(-5 * time.Minute)
				q.To = &to
			case "two_people":
				second := *member
				second.ReferredCustomer = "second-private-customer"
				second.ID = referral.RelationshipReferenceID(referral.ProgramID, "partner", second.ReferredCustomer)
				head := member.Head
				head.ID, head.SignupID, head.ReferredCustomer = "second-private-revision", "second-private-signup", second.ReferredCustomer
				second.Head, second.FirstReferralID, second.History = head, head.ID, []referral.Referral{head}
				r.snapshot.Analytics.Relationships = append(r.snapshot.Analytics.Relationships, second)
				r.snapshot.Bindings[second.ReferredCustomer] = []referral.PaymentAttribution{}
				conversionPayment(t, f, r, "second", second.ReferredCustomer, f.original.PlanID, head.ID, "partner", f.original.EffectiveAt)
			case "renewal":
				conversionPayment(t, f, r, "renewal", member.ReferredCustomer, f.original.PlanID, member.Head.ID, "partner", f.original.EffectiveAt)
			case "refund":
				f.adjust(t, billing.RevenueRefund, 10000, "refund-full")
			case "former":
				head := member.Head
				head.ID, head.Revision, head.PartnerID, head.CorrectionOf, head.PriorPartnerID = "other-revision", 2, "other-owner", member.Head.ID, "partner"
				head.SourceKind, head.SourceCode, head.SourceMeasuredClickID, head.SourceMeasuredOccurredAt = "admin", "", "", nil
				head.LockedAt, head.PostedAt = at.Add(time.Hour), at.Add(time.Hour)
				member.History, member.Head = append(member.History, head), head
			case "unmeasured", "manual":
				member.History[0].SourceMeasuredClickID, member.History[0].SourceMeasuredOccurredAt = "", nil
				if tc.mode == "manual" {
					member.History[0].SourceKind, member.History[0].SourceCode = "admin", ""
				}
				member.Head = member.History[0]
			case "correction":
				original := member.History[0]
				original.PartnerID, original.SourceCode = "other-owner", "other-code"
				head := original
				head.ID, head.Revision, head.PartnerID, head.CorrectionOf, head.PriorPartnerID = "corrected-revision", 2, "partner", original.ID, original.PartnerID
				head.SourceKind, head.SourceCode, head.SourceMeasuredClickID, head.SourceMeasuredOccurredAt = "admin", "", "", nil
				head.LockedAt, head.PostedAt = at.Add(time.Hour), at.Add(time.Hour)
				member.Head, member.FirstReferralID, member.History = head, head.ID, []referral.Referral{original, head}
				r.snapshot.Bindings[member.ReferredCustomer][0].PartnerID = "other-owner"
				f.clock.at = at.Add(2 * time.Hour)
				conversionPayment(t, f, r, "corrected", member.ReferredCustomer, f.original.PlanID, head.ID, "partner", at.Add(time.Hour+time.Minute))
			case "missing":
				member.History[0].SourceMeasuredOccurredAt = nil
				member.Head = member.History[0]
			case "unbound":
				r.snapshot.Bindings[member.ReferredCustomer] = []referral.PaymentAttribution{}
			case "plan", "recurrence":
				terms := member.History[0].TermsSnapshot
				if tc.mode == "plan" {
					terms.EligiblePlanIDs = []string{"different-approved-plan"}
				} else {
					terms.RecurrenceEndsAt = &f.original.EffectiveAt
				}
				member.History[0].TermsSnapshot = terms
				member.Head = member.History[0]
				r.snapshot.Bindings[member.ReferredCustomer][0].Terms = terms
			}
			beforeWrites := f.records.writes
			f.records.readOnly = true
			out, err := f.manager.ConversionMetrics(context.Background(), "owner", q)
			require.NoError(t, err)
			c := out.Cohorts
			require.Equal(t, tc.measured, c.MeasuredSignups.ConfirmedConverted)
			require.Equal(t, tc.unmeasured, c.UnmeasuredSignups.ConfirmedConverted)
			require.Equal(t, tc.manual, c.ManualInitial.ConfirmedConverted)
			require.Equal(t, tc.corrections, c.CorrectionEvents.ConfirmedConverted)
			require.Equal(t, tc.visits, c.MeasuredVisits.ConfirmedConverted)
			require.Equal(t, tc.net, c.MeasuredSignups.NetPositive)
			if tc.visitRate {
				require.NotNil(t, c.MeasuredVisits.ObservedRate)
			} else {
				require.Nil(t, c.MeasuredVisits.ObservedRate)
			}
			require.Equal(t, 2, r.calls)
			require.Equal(t, 1, f.source.historyCalls)
			require.Zero(t, f.source.statusCalls, "conversion is not current subscription activity")
			require.Equal(t, beforeWrites, f.records.writes)
			bytes, err := json.Marshal(out)
			require.NoError(t, err)
			for _, private := range []string{"private-customer", "private-signup", "private-revision", "private-origin", "private-plan", "acct_private", "cus_private", "sub_private", "other-owner"} {
				require.NotContains(t, string(bytes), private)
			}
		})
	}
}

func TestConversionFailuresAndCurrentAuthority(t *testing.T) {
	for _, admin := range []bool{false, true} {
		for _, tc := range []struct {
			name, mode string
			want       error
		}{
			{"binding_added_or_changed_during_billing", "changed_binding", referral.ErrStaleWrite},
			{"day_changed_during_billing", "changed_day", referral.ErrStaleWrite},
			{"revoked_before_return", "revoke", ErrDenied},
			{"denied_before_read", "deny", ErrDenied},
			{"source_capacity_is_not_zero_conversion", "capacity", billing.ErrRevenueHistoryTooLarge},
			{"source_outage_is_not_zero_conversion", "source_outage", billing.ErrRevenueUnavailable},
			{"partial_day_raw_expired", "expired", referral.ErrGranularity},
			{"missing_revenue_opt_in", "config", ErrUnavailable},
			{"invalid_period_before_read", "query", ErrInvalid},
		} {
			t.Run(strings.Join([]string{map[bool]string{false: "self", true: "admin"}[admin], tc.name}, "/"), func(t *testing.T) {
				f, r := newConversionReportFixture(t)
				r.mode = tc.mode
				q := referral.AnalyticsQuery{Limit: 1}
				switch tc.mode {
				case "revoke":
					r.revoke = f.authority
				case "deny":
					f.authority.deny = true
				case "capacity", "source_outage":
					f.source.mode = tc.mode
				case "expired":
					to := f.manager.deps.Clock.Now()
					q.To = &to
					r.snapshot.Analytics.Clicks = nil
				case "config":
					f.manager.revenueReporting = nil
				case "query":
					q.Limit = 0
				}
				f.records.readOnly = true
				if admin {
					out, err := f.manager.AdminConversionMetrics(context.Background(), "operator", "partner", AdminConversionQuery{AnalyticsQuery: q, PlanLimit: 1})
					require.ErrorIs(t, err, tc.want)
					require.Zero(t, out)
				} else {
					out, err := f.manager.ConversionMetrics(context.Background(), "owner", q)
					require.ErrorIs(t, err, tc.want)
					require.Zero(t, out)
				}
				if tc.mode == "deny" || tc.mode == "query" {
					require.Zero(t, r.calls)
				}
			})
		}
	}
}

func TestConversionLinkAndPlanPagination(t *testing.T) {
	for _, tc := range []struct{ name, mode string }{
		{"first_plan_page_keeps_global_denominator", ""},
		{"second_plan_page_keeps_global_denominator", "plan_page"},
		{"second_link_page_keeps_global_conversions", "link_page"},
		{"foreign_plan_cursor_is_rejected", "foreign"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r := newConversionReportFixture(t)
			member := &r.snapshot.Analytics.Relationships[0]
			terms := member.History[0].TermsSnapshot
			terms.EligiblePlanIDs = []string{f.original.PlanID, "private-plan-two"}
			member.History[0].TermsSnapshot = terms
			member.Head = member.History[0]
			r.snapshot.Bindings[member.ReferredCustomer][0].Terms = terms
			conversionPayment(t, f, r, "plan-two", member.ReferredCustomer, "private-plan-two", member.Head.ID, "partner", f.original.EffectiveAt)
			second := r.snapshot.Analytics.Links[0]
			second.ID, second.Code = "second-link", "second-code"
			r.snapshot.Analytics.Links = append(r.snapshot.Analytics.Links, second)
			q := AdminConversionQuery{AnalyticsQuery: referral.AnalyticsQuery{Limit: 1}, PlanLimit: 1}
			switch tc.mode {
			case "plan_page":
				q.AfterPlanID = f.original.PlanID
			case "link_page":
				q.AfterLinkID = "own-link"
			case "foreign":
				q.AfterPlanID = "other-partner-plan"
			}
			out, err := f.manager.AdminConversionMetrics(context.Background(), "operator", "partner", q)
			if tc.mode == "foreign" {
				require.ErrorIs(t, err, ErrInvalid)
				require.Zero(t, out)
				return
			}
			require.NoError(t, err)
			require.EqualValues(t, 1, out.Summary.Cohorts.MeasuredSignups.ConfirmedConverted)
			require.EqualValues(t, 1, out.MultiPlanSignups, "overlapping plan people cannot be summed")
			require.Len(t, out.Plans, 1)
			require.EqualValues(t, 1, out.Plans[0].AutomaticSignups.Denominator)
			require.EqualValues(t, 1, out.Plans[0].AutomaticSignups.ConfirmedConverted)
			if tc.mode == "plan_page" {
				require.Equal(t, "private-plan-two", out.Plans[0].PlanID)
				require.False(t, out.HasMorePlans)
			} else {
				require.Equal(t, f.original.PlanID, out.Plans[0].PlanID)
				require.True(t, out.HasMorePlans)
			}
			if tc.mode == "link_page" {
				require.Equal(t, "second-link", out.Summary.Links[0].LinkID)
				require.Zero(t, out.Summary.Links[0].Cohorts.MeasuredSignups.ConfirmedConverted)
				require.Nil(t, out.Summary.Links[0].Cohorts.MeasuredSignups.ObservedRate)
			}
			bytes, err := json.Marshal(out)
			require.NoError(t, err)
			require.JSONEq(t, "{}", string(bytes), "private operator domain report requires host projection")
		})
	}
}

func TestConversionEmptyCohorts(t *testing.T) {
	for _, tc := range []struct {
		name                string
		noRelationships     bool
		expectSourceQueries int
	}{
		{"no_relationships_has_no_billing_clock", true, 0},
		{"no_events_in_original_cohort_has_no_rate", false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r := newConversionReportFixture(t)
			q := referral.AnalyticsQuery{Limit: 1}
			if tc.noRelationships {
				r.snapshot.Analytics.Relationships = []referral.RelationshipSnapshot{}
				r.snapshot.Bindings = map[string][]referral.PaymentAttribution{}
			} else {
				from := f.clock.Now().Truncate(24 * time.Hour).Add(24 * time.Hour)
				to := from.Add(24 * time.Hour)
				q.From, q.To = &from, &to
			}
			f.records.readOnly = true
			out, err := f.manager.ConversionMetrics(context.Background(), "owner", q)
			require.NoError(t, err)
			for _, fraction := range []ConversionFraction{out.Cohorts.MeasuredSignups, out.Cohorts.UnmeasuredSignups, out.Cohorts.ManualInitial, out.Cohorts.CorrectionEvents} {
				require.Zero(t, fraction.Denominator)
				require.Zero(t, fraction.ConfirmedConverted)
				require.Nil(t, fraction.ObservedRate, "empty cohort is not an observed zero rate")
			}
			require.Equal(t, tc.expectSourceQueries, f.source.historyCalls)
			if tc.noRelationships {
				require.Nil(t, out.SourceAsOf)
				require.Equal(t, "no_partner_relationships", out.SourceCoverage)
				require.NotNil(t, out.Cohorts.MeasuredVisits.ObservedRate)
				require.Zero(t, *out.Cohorts.MeasuredVisits.ObservedRate)
			} else {
				require.NotNil(t, out.SourceAsOf)
				require.Nil(t, out.Cohorts.MeasuredVisits.ObservedRate)
			}
		})
	}
}
