package partnermanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sort"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billing/revenuestore"
	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/referral"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named per-case fixtures use the actual billing owner and
// adapter. Native snapshot/encryption races live in revenuestore's Mongo suite;
// this fixture asserts manager privacy, provenance and absence of read writes.
type paidReportRecords struct {
	rows     map[string]recordstore.Record
	readOnly bool
	writes   int
}
type paidReportTx struct{ rows map[string]recordstore.Record }

func paidReportKey(kind, id string) string { return kind + ":" + id }
func (s *paidReportRecords) Read(ctx context.Context, fn func(recordstore.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(&paidReportTx{s.rows})
}
func (s *paidReportRecords) Transact(ctx context.Context, _ string, fn func(recordstore.Tx) error) error {
	s.writes++
	if s.readOnly {
		return errors.New("unexpected financial write during report")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	tx := &paidReportTx{maps.Clone(s.rows)}
	if err := fn(tx); err != nil {
		return err
	}
	s.rows = tx.rows
	return nil
}
func (tx *paidReportTx) Get(ctx context.Context, kind, id string) (recordstore.Record, error) {
	if err := ctx.Err(); err != nil {
		return recordstore.Record{}, err
	}
	row, ok := tx.rows[paidReportKey(kind, id)]
	if !ok {
		return recordstore.Record{}, recordstore.ErrNotFound
	}
	return row, nil
}
func (tx *paidReportTx) Find(ctx context.Context, q recordstore.Query) ([]recordstore.Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := []recordstore.Record{}
	for _, row := range tx.rows {
		if row.Kind == q.Kind && row.Partition == q.Partition && row.ID > q.AfterID {
			out = append(out, row)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	if len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}
func (tx *paidReportTx) Insert(ctx context.Context, row recordstore.Record) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key := paidReportKey(row.Kind, row.ID)
	if _, exists := tx.rows[key]; exists {
		return recordstore.ErrConflict
	}
	tx.rows[key] = row
	return nil
}
func (tx *paidReportTx) Replace(ctx context.Context, row recordstore.Record, expected int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key := paidReportKey(row.Kind, row.ID)
	if tx.rows[key].Revision != expected {
		return recordstore.ErrConflict
	}
	tx.rows[key] = row
	return nil
}

type paidReportClock struct{ at time.Time }

func (c *paidReportClock) Now() time.Time { return c.at }

type paidReportReferral struct {
	*relationshipReadStub
	snapshot                 referral.AttributionSnapshot
	snapshots                map[string]referral.AttributionSnapshot
	snapshotCalls, bindCalls int
	changeOnConfirm          bool
}

func (r *paidReportReferral) GetAttributionSnapshot(_ context.Context, customer string) (referral.AttributionSnapshot, error) {
	r.snapshotCalls++
	out := r.snapshot
	if r.snapshots != nil {
		out = r.snapshots[customer]
	}
	if r.changeOnConfirm && r.snapshotCalls > 1 {
		out.Fingerprint = "changed-owning-attribution"
	}
	return out, nil
}
func (r *paidReportReferral) BindPayment(context.Context, string, string, time.Time) (referral.PaymentAttribution, error) {
	r.bindCalls++
	return referral.PaymentAttribution{}, errors.New("report must never create attribution")
}

type paidReportSource struct {
	*billing.RevenueService
	historyCalls, statusCalls int
	mode                      string
	revoke                    *authorityStub
	cancel                    context.CancelFunc
}

func (s *paidReportSource) GetPaymentRevenueHistory(ctx context.Context, q billing.RevenueHistoryQuery) (billing.PaymentRevenueHistory, error) {
	s.historyCalls++
	if s.mode == "source_outage" {
		return billing.PaymentRevenueHistory{}, billing.ErrRevenueUnavailable
	}
	if s.mode == "capacity" {
		return billing.PaymentRevenueHistory{}, billing.ErrRevenueHistoryTooLarge
	}
	out, err := s.RevenueService.GetPaymentRevenueHistory(ctx, q)
	if err != nil {
		return out, err
	}
	switch s.mode {
	case "nil_items":
		out.Items = nil
	case "duplicate":
		out.Items = append(out.Items, out.Items[0])
	case "net_corrupt":
		out.Items[0].NetMinor++
	case "wrong_principal":
		out.Items[0].Original.PrincipalID = "unrelated-payer"
	case "wrong_scope":
		out.Items[0].Original.Scope.AccountID = "unrelated-merchant"
	}
	return out, nil
}
func (s *paidReportSource) GetSubscriptionStatusForFact(ctx context.Context, id string, age time.Duration) (billing.SubscriptionStatus, error) {
	s.statusCalls++
	if s.revoke != nil {
		s.revoke.deny = true
	}
	if s.cancel != nil {
		s.cancel()
		return billing.SubscriptionStatus{}, ctx.Err()
	}
	if s.mode == "status_outage" {
		return billing.SubscriptionStatus{}, billing.ErrRevenueUnavailable
	}
	out, err := s.RevenueService.GetSubscriptionStatusForFact(ctx, id, age)
	if err == nil && s.mode == "status_corrupt" {
		out.Fingerprint = "invalid"
	}
	return out, err
}

type paidReportFixture struct {
	manager   *Manager
	referral  *paidReportReferral
	financial *summaryEarningsStub
	source    *paidReportSource
	records   *paidReportRecords
	clock     *paidReportClock
	authority *authorityStub
	original  billing.RevenueFact
}

func newPaidReportFixture(t *testing.T) *paidReportFixture {
	t.Helper()
	m, program, _, _, auth, _, _ := managerFixture(t)
	program.cfg.CurrencyExponent = 2
	at := m.deps.Clock.Now()
	page := managerRelationshipPage(at)
	page.Items[0].Current = true
	page.Items[0].Periods[0].Until = nil
	r := &paidReportReferral{relationshipReadStub: &relationshipReadStub{page: page}, snapshot: referral.AttributionSnapshot{ReferredCustomer: page.Items[0].ReferredCustomer, Head: referral.Referral{ID: page.Items[0].Periods[0].ReferralID, ProgramID: referral.ProgramID, PartnerID: "partner", ReferredCustomer: page.Items[0].ReferredCustomer}, Fingerprint: "owning-attribution-fixture", Bindings: []referral.PaymentAttribution{}}}
	e := &summaryEarningsStub{report: partnerearnings.ReferralAmountReport{ProgramID: referral.ProgramID, PartnerID: "partner", Currency: "EUR", Revision: "independent-financial-revision", AsOf: at, Items: []partnerearnings.ReferralAmounts{{ID: page.Items[0].ID}}}}
	records := &paidReportRecords{rows: map[string]recordstore.Record{}}
	repo, err := revenuestore.NewRepository(records)
	require.NoError(t, err)
	clock := &paidReportClock{at: at.Add(time.Hour)}
	owner, err := billing.NewRevenueService(repo, clock)
	require.NoError(t, err)
	source := &paidReportSource{RevenueService: owner}
	m.deps.Referral, m.deps.Earnings, m.deps.Revenue = r, e, source
	f := &paidReportFixture{manager: m, referral: r, financial: e, source: source, records: records, clock: clock, authority: auth}
	f.original = f.addPayment(t, "original", 10000, "sub_private", true)
	_, err = m.WithRevenueReporting(RevenueReportingConfig{Scopes: []billing.RevenueScope{f.original.Scope}, StatusMaxAge: time.Hour})
	require.NoError(t, err)
	return f
}
func (f *paidReportFixture) addPayment(t *testing.T, id string, paid int64, sub string, bound bool) billing.RevenueFact {
	t.Helper()
	raw := billing.RevenueFact{Scope: billing.RevenueScope{Provider: "stripe", AccountID: "acct_private"}, Kind: billing.RevenuePayment, PaymentID: "pi_" + id, InvoiceID: "in_" + id, AllocationID: "il_" + id, PrincipalID: "private-customer", ProviderCustomerID: "cus_private", SubscriptionID: sub, PlanID: "private-plan", CostID: "cost_private", Currency: "EUR", CurrencyExponent: 2, PaidMinor: paid, EffectiveAt: f.manager.deps.Clock.Now().Add(time.Minute)}
	o, err := f.source.AcceptVerified(context.Background(), billing.VerifiedRevenueRequest{Scope: raw.Scope, EnvelopeID: "evt_" + id, Facts: []billing.RevenueFact{raw}})
	require.NoError(t, err)
	row, err := f.source.GetRevenueFact(context.Background(), o.FactIDs[0])
	require.NoError(t, err)
	if bound {
		period := f.referral.page.Items[0].Periods[0]
		f.referral.snapshot.Bindings = append(f.referral.snapshot.Bindings, referral.PaymentAttribution{ID: "binding_" + id, ProgramID: referral.ProgramID, PartnerID: "partner", ReferralID: period.ReferralID, ReferredCustomer: row.PrincipalID, PaymentID: row.ID, EffectiveAt: row.EffectiveAt, BoundAt: f.clock.Now(), Terms: period.Terms})
	}
	return row
}
func (f *paidReportFixture) capture(t *testing.T, row billing.RevenueFact, status string) {
	t.Helper()
	p, err := f.source.PrepareSubscriptionStatus(context.Background(), "fixture-status-worker", row.ID)
	require.NoError(t, err)
	_, err = f.source.CaptureVerifiedSubscriptionStatus(context.Background(), p, billing.VerifiedSubscriptionStatusEvidence{Scope: row.Scope, SubscriptionID: row.SubscriptionID, ProviderCustomerID: row.ProviderCustomerID, Status: status})
	require.NoError(t, err)
}
func (f *paidReportFixture) adjust(t *testing.T, kind string, cumulative int64, id string) {
	t.Helper()
	raw := f.original
	raw.Kind = kind
	raw.AdjustmentID = id
	raw.CumulativeRefundedMinor = cumulative
	_, err := f.source.AcceptVerified(context.Background(), billing.VerifiedRevenueRequest{Scope: raw.Scope, EnvelopeID: "evt_adjust_" + id + kind, Facts: []billing.RevenueFact{raw}})
	require.NoError(t, err)
}

func TestPaidReferralSummaryEconomicsAndStatus(t *testing.T) {
	type testCase struct {
		name, mode, status                                            string
		paid, net                                                     bool
		allocations, awaiting, active, trialing, unknown, statusReads int
		exact                                                         bool
	}
	cases := []testCase{
		{name: "one_paid_person_with_active_subscription", status: "active", paid: true, net: true, allocations: 1, active: 1, exact: true, statusReads: 1},
		{name: "renewals_are_one_person_and_one_subscription", mode: "renewals", status: "active", paid: true, net: true, allocations: 3, active: 1, exact: true, statusReads: 1},
		{name: "distinct_subscriptions_are_not_distinct_people", mode: "subscriptions", status: "active", paid: true, net: true, allocations: 2, active: 2, exact: true, statusReads: 2},
		{name: "trialing_is_separate_from_active", status: "trialing", paid: true, net: true, allocations: 1, trialing: 1, exact: true, statusReads: 1},
		{name: "canceled_is_known_inactive", status: "canceled", paid: true, net: true, allocations: 1, exact: true, statusReads: 1},
		{name: "missing_status_is_unknown", paid: true, net: true, allocations: 1, unknown: 1, statusReads: 1},
		{name: "stale_status_is_unknown", mode: "stale", status: "active", paid: true, net: true, allocations: 1, unknown: 1, statusReads: 1},
		{name: "status_outage_preserves_source_history", mode: "status_outage", status: "active", paid: true, net: true, allocations: 1, unknown: 1, statusReads: 1},
		{name: "partial_unknown_withholds_exact_total", mode: "partial", status: "active", paid: true, net: true, allocations: 2, active: 1, unknown: 1, statusReads: 2},
		{name: "partial_refund_still_net_positive", mode: "partial_refund", status: "active", paid: true, net: true, allocations: 1, active: 1, exact: true, statusReads: 1},
		{name: "full_refund_preserves_paid_history_without_active_paid", mode: "full_refund", status: "active", paid: true, allocations: 1, exact: true},
		{name: "lost_dispute_consumes_remaining_after_refund", mode: "lost", status: "active", paid: true, allocations: 1, exact: true},
		{name: "dispute_hold_is_not_confirmed_loss", mode: "hold", status: "active", paid: true, net: true, allocations: 1, active: 1, exact: true, statusReads: 1},
		{name: "unbound_source_is_processing_not_entitlement", mode: "unbound", status: "active", awaiting: 1, exact: true},
		{name: "zero_payment_is_not_paid", mode: "zero", awaiting: 1, exact: true},
		{name: "former_owner_keeps_history_without_live_status", mode: "former", status: "active", paid: true, net: true, allocations: 1, exact: true},
		{name: "other_owner_frozen_binding_is_excluded", mode: "other_owner", status: "active", exact: true},
		{name: "unreceived_deliveries_are_never_certified", mode: "quarantine", status: "active", paid: true, net: true, allocations: 1, active: 1, exact: true, statusReads: 1},
		{name: "same_payment_id_in_other_merchant_does_not_join", mode: "other_scope", status: "active", paid: true, net: true, allocations: 1, active: 1, exact: true, statusReads: 1},
	}
	for _, admin := range []bool{false, true} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("admin_%t/%s", admin, tc.name), func(t *testing.T) {
				f := newPaidReportFixture(t)
				if tc.status != "" {
					f.capture(t, f.original, tc.status)
				}
				switch tc.mode {
				case "renewals":
					f.addPayment(t, "renewal_one", 10000, f.original.SubscriptionID, true)
					f.addPayment(t, "renewal_two", 10000, f.original.SubscriptionID, true)
				case "subscriptions", "partial":
					row := f.addPayment(t, "second_subscription", 10000, "sub_private_second", true)
					if tc.mode == "subscriptions" {
						f.capture(t, row, "active")
					}
				case "stale":
					f.clock.at = f.clock.at.Add(time.Hour + time.Nanosecond)
				case "status_outage":
					f.source.mode = tc.mode
				case "partial_refund":
					f.adjust(t, billing.RevenueRefund, 2000, "refund_one")
					f.adjust(t, billing.RevenueRefund, 6000, "refund_two")
				case "full_refund":
					f.adjust(t, billing.RevenueRefund, 10000, "refund_full")
				case "lost":
					f.adjust(t, billing.RevenueRefund, 2000, "refund_one")
					f.adjust(t, billing.RevenueDisputeLost, 0, "dispute_one")
				case "hold":
					f.adjust(t, billing.RevenueDisputeHold, 0, "dispute_one")
				case "unbound":
					f.referral.snapshot.Bindings = nil
				case "zero":
					f.referral.snapshot.Bindings = nil
					f.addPayment(t, "zero", 0, "sub_zero", true)
				case "former":
					until := f.manager.deps.Clock.Now().Add(time.Hour)
					f.referral.page.Items[0].Current = false
					f.referral.page.Items[0].Periods[0].Until = &until
					f.referral.snapshot.Head.PartnerID = "new-owner"
				case "other_owner":
					f.referral.snapshot.Bindings[0].PartnerID = "new-owner"
				case "quarantine":
					_, err := f.source.AcceptVerified(context.Background(), billing.VerifiedRevenueRequest{Scope: f.original.Scope, EnvelopeID: "evt_unresolved_other_payer", QuarantineReason: "historical-association-missing"})
					require.NoError(t, err)
				case "other_scope":
					raw := f.original
					raw.Scope.AccountID = "acct_unrelated"
					_, err := f.source.AcceptVerified(context.Background(), billing.VerifiedRevenueRequest{Scope: raw.Scope, EnvelopeID: "evt_other_merchant", Facts: []billing.RevenueFact{raw}})
					require.NoError(t, err)
				}
				f.records.readOnly = true
				writes := f.records.writes
				var out ReferralSummaryPage
				var err error
				if admin {
					out, err = f.manager.AdminReferralSummaries(context.Background(), "operator", "partner", ReferralSummaryQuery{Limit: 1})
				} else {
					out, err = f.manager.ReferralSummaries(context.Background(), "owner", ReferralSummaryQuery{Limit: 1})
				}
				require.NoError(t, err)
				require.Len(t, out.Items, 1)
				e := out.Items[0].PaidEvidence
				require.NotNil(t, e)
				require.Equal(t, tc.paid, e.Paid)
				require.Equal(t, tc.net, e.NetPositive)
				require.Equal(t, tc.allocations, e.ConfirmedAllocationRows)
				require.Equal(t, tc.awaiting, e.AwaitingAttributionRows)
				require.Equal(t, tc.active, e.ConfirmedActivePaidSubscriptions)
				require.Equal(t, tc.trialing, e.ConfirmedTrialingSubscriptions)
				require.Equal(t, tc.unknown, e.UnknownSubscriptions)
				require.Equal(t, tc.statusReads, f.source.statusCalls)
				require.Equal(t, tc.active, out.PaidCoverage.ConfirmedActivePaidSubscriptions)
				require.Equal(t, tc.trialing, out.PaidCoverage.ConfirmedTrialingSubscriptions)
				if tc.exact {
					require.NotNil(t, out.PaidCoverage.ActivePaidSubscriptions)
					require.Equal(t, tc.active, *out.PaidCoverage.ActivePaidSubscriptions)
				} else {
					require.Nil(t, out.PaidCoverage.ActivePaidSubscriptions)
					require.Nil(t, e.ActivePaidSubscriptions)
				}
				if tc.mode == "former" {
					require.Nil(t, e.ActivePaidSubscriptions)
					require.Equal(t, "retained_history_only", e.SubscriptionCoverage)
				}
				require.Equal(t, "visible_relationships", out.Scope)
				require.Equal(t, "confirmed_accepted_history", out.SourceCoverage)
				require.Equal(t, "unreceived_deliveries_not_assessed", out.PaidCoverage.UpstreamDeliveryCoverage)
				require.Equal(t, writes, f.records.writes)
				require.Zero(t, f.referral.bindCalls)
				require.Equal(t, 2, f.referral.snapshotCalls)
				data, err := json.Marshal(out)
				require.NoError(t, err)
				for _, private := range []string{"private-customer", "private-revision", "acct_private", "acct_unrelated", "cus_private", "sub_private", "pi_original", "in_original", "il_original", "cost_private", "private-plan", "binding_original", "new-owner", "evt_unresolved_other_payer"} {
					require.NotContains(t, string(data), private)
				}
			})
		}
	}
}

func TestPaidReferralSummaryFailureBoundaries(t *testing.T) {
	type testCase struct {
		name, mode  string
		want        error
		statusReads int
	}
	cases := []testCase{
		{name: "source_outage_never_returns_empty_success", mode: "source_outage", want: billing.ErrRevenueUnavailable},
		{name: "complete_history_capacity_is_not_invalid_input", mode: "capacity", want: billing.ErrRevenueHistoryTooLarge},
		{name: "nil_source_items_are_not_confirmed_zero", mode: "nil_items", want: ErrUnavailable},
		{name: "duplicate_allocation_cannot_inflate_people", mode: "duplicate", want: ErrUnavailable},
		{name: "malformed_net_does_not_leave_valid_commission", mode: "net_corrupt", want: ErrUnavailable},
		{name: "foreign_principal_is_not_returned", mode: "wrong_principal", want: ErrUnavailable},
		{name: "foreign_scope_is_not_returned", mode: "wrong_scope", want: ErrUnavailable},
		{name: "invalid_successful_status_fails_closed", mode: "status_corrupt", want: ErrUnavailable, statusReads: 1},
		{name: "frozen_binding_time_mismatch", mode: "binding_time", want: ErrUnavailable},
		{name: "frozen_binding_terms_mismatch", mode: "binding_terms", want: ErrUnavailable},
		{name: "missing_retained_period", mode: "binding_period", want: ErrUnavailable},
		{name: "attribution_changes_between_reads", mode: "attribution_changed", want: referral.ErrStaleWrite, statusReads: 1},
		{name: "revoked_authority_after_status_read", mode: "revoked", want: ErrDenied, statusReads: 1},
		{name: "canceled_during_optional_status_discards_everything", mode: "canceled", want: context.Canceled, statusReads: 1},
		{name: "changed_revenue_owner_cannot_use_stale_configuration", mode: "replaced_owner", want: ErrUnavailable},
	}
	for _, admin := range []bool{false, true} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("admin_%t/%s", admin, tc.name), func(t *testing.T) {
				f := newPaidReportFixture(t)
				f.capture(t, f.original, "active")
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				f.source.mode = tc.mode
				switch tc.mode {
				case "binding_time":
					f.referral.snapshot.Bindings[0].EffectiveAt = f.original.EffectiveAt.Add(time.Second)
				case "binding_terms":
					f.referral.snapshot.Bindings[0].Terms.RateBasisPoints++
				case "binding_period":
					f.referral.snapshot.Bindings[0].ReferralID = "absent-revision"
				case "attribution_changed":
					f.referral.changeOnConfirm = true
				case "revoked":
					f.source.revoke = f.authority
				case "canceled":
					f.source.cancel = cancel
				case "replaced_owner":
					f.manager.deps.Revenue = &revenueStub{}
				}
				f.records.readOnly = true
				writes := f.records.writes
				var out ReferralSummaryPage
				var err error
				if admin {
					out, err = f.manager.AdminReferralSummaries(ctx, "operator", "partner", ReferralSummaryQuery{Limit: 1})
				} else {
					out, err = f.manager.ReferralSummaries(ctx, "owner", ReferralSummaryQuery{Limit: 1})
				}
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, out)
				require.Equal(t, tc.statusReads, f.source.statusCalls)
				require.Zero(t, f.referral.bindCalls)
				require.Equal(t, writes, f.records.writes)
			})
		}
	}
}

func TestRevenueReportingConfiguration(t *testing.T) {
	type testCase struct {
		name, mode string
		age        time.Duration
		want       error
	}
	cases := []testCase{
		{name: "minimum_one_second", age: time.Second}, {name: "maximum_one_day", age: 24 * time.Hour},
		{name: "missing_freshness", want: ErrInvalid}, {name: "too_short", age: time.Second - time.Nanosecond, want: ErrInvalid}, {name: "too_long", age: 24*time.Hour + time.Nanosecond, want: ErrInvalid},
		{name: "missing_explicit_scopes", age: time.Hour, mode: "scopes", want: ErrInvalid},
		{name: "duplicate_scopes", age: time.Hour, mode: "duplicate", want: ErrInvalid},
		{name: "same_owner_capability_required", age: time.Hour, mode: "owner", want: ErrUnavailable},
		{name: "typed_nil_owner_rejected", age: time.Hour, mode: "nil_owner", want: ErrUnavailable},
		{name: "nil_manager_rejected", age: time.Hour, mode: "nil_manager", want: ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPaidReportFixture(t)
			m := f.manager
			config := RevenueReportingConfig{Scopes: []billing.RevenueScope{f.original.Scope}, StatusMaxAge: tc.age}
			switch tc.mode {
			case "scopes":
				config.Scopes = nil
			case "duplicate":
				config.Scopes = append(config.Scopes, config.Scopes[0])
			case "owner":
				m.deps.Revenue = &revenueStub{}
			case "nil_owner":
				m.deps.Revenue = (*paidReportSource)(nil)
			case "nil_manager":
				m = nil
			}
			out, err := m.WithRevenueReporting(config)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Nil(t, out)
			} else {
				config.Scopes[0].AccountID = "mutated-host-input"
				require.Equal(t, f.original.Scope, out.revenueReporting.Scopes[0])
				data, err := json.Marshal(config)
				require.NoError(t, err)
				require.JSONEq(t, `{}`, string(data))
			}
		})
	}
}

func TestPaidReferralOriginalPaymentCohorts(t *testing.T) {
	type testCase struct {
		name, mode  string
		paid, net   bool
		statusReads int
	}
	cases := []testCase{
		{name: "inclusive_original_from", mode: "from", paid: true, net: true, statusReads: 1},
		{name: "exclusive_original_to", mode: "to"},
		{name: "original_before_selected_cohort", mode: "after"},
		{name: "refund_outside_cohort_still_attaches_to_original", mode: "late_refund", paid: true},
		{name: "ineligible_frozen_plan_is_not_paid_eligibility", mode: "plan"},
		{name: "recurrence_endpoint_is_exclusive", mode: "recurrence"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPaidReportFixture(t)
			f.capture(t, f.original, "active")
			at := f.original.EffectiveAt
			q := ReferralSummaryQuery{Limit: 1}
			switch tc.mode {
			case "from":
				q.From = &at
			case "to":
				q.To = &at
			case "after":
				after := at.Add(time.Nanosecond)
				q.From = &after
			case "late_refund":
				q.From = &at
				until := at.Add(time.Hour)
				q.To = &until
				raw := f.original
				raw.Kind = billing.RevenueRefund
				raw.AdjustmentID = "late_refund"
				raw.CumulativeRefundedMinor = 10000
				raw.EffectiveAt = until.Add(time.Hour)
				_, err := f.source.AcceptVerified(context.Background(), billing.VerifiedRevenueRequest{Scope: raw.Scope, EnvelopeID: "evt_late_refund", Facts: []billing.RevenueFact{raw}})
				require.NoError(t, err)
			case "plan":
				f.referral.page.Items[0].Periods[0].Terms.EligiblePlanIDs = []string{"other-plan"}
				f.referral.snapshot.Bindings[0].Terms = f.referral.page.Items[0].Periods[0].Terms
			case "recurrence":
				f.referral.page.Items[0].Periods[0].Terms.RecurrenceEndsAt = &at
				f.referral.snapshot.Bindings[0].Terms = f.referral.page.Items[0].Periods[0].Terms
			}
			f.records.readOnly = true
			out, err := f.manager.ReferralSummaries(context.Background(), "owner", q)
			require.NoError(t, err)
			require.Equal(t, tc.paid, out.Items[0].PaidEvidence.Paid)
			require.Equal(t, tc.net, out.Items[0].PaidEvidence.NetPositive)
			require.Equal(t, tc.statusReads, f.source.statusCalls)
			require.NotNil(t, out.PaidCoverage.ActivePaidSubscriptions)
			require.Equal(t, tc.statusReads, *out.PaidCoverage.ActivePaidSubscriptions)
		})
	}
}

func TestPaidReferralEmptyAndLegacyCoverage(t *testing.T) {
	type testCase struct {
		name          string
		empty, legacy bool
	}
	cases := []testCase{{name: "empty_visible_page_is_not_whole_program_source_proof", empty: true}, {name: "legacy_without_opt_in_stays_not_evaluated", legacy: true}, {name: "legacy_empty_page_stays_not_evaluated", empty: true, legacy: true}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPaidReportFixture(t)
			if tc.empty {
				f.referral.page.Items = []referral.Relationship{}
				f.financial.report.Items = []partnerearnings.ReferralAmounts{}
			}
			if tc.legacy {
				f.manager.revenueReporting = nil
			}
			f.records.readOnly = true
			out, err := f.manager.ReferralSummaries(context.Background(), "owner", ReferralSummaryQuery{Limit: 1})
			require.NoError(t, err)
			require.Zero(t, f.source.historyCalls)
			require.Zero(t, f.source.statusCalls)
			require.Zero(t, f.referral.snapshotCalls)
			if tc.legacy {
				require.Nil(t, out.PaidCoverage)
				require.Equal(t, "not_evaluated", out.SourceCoverage)
			} else {
				require.NotNil(t, out.PaidCoverage)
				require.Equal(t, "no_visible_relationships", out.SourceCoverage)
				require.Nil(t, out.PaidCoverage.SourceAsOf)
				require.Empty(t, out.PaidCoverage.SourceRevision)
			}
		})
	}
}

func TestPaidReferralMultiplePeopleAndCapacity(t *testing.T) {
	type testCase struct {
		name, mode     string
		want           error
		people, active int
	}
	cases := []testCase{
		{name: "two_people_with_distinct_subscriptions", people: 2, active: 2},
		{name: "one_scoped_subscription_cannot_belong_to_two_people", mode: "collision", want: billing.ErrRevenueUnassessable},
		{name: "attribution_read_budget_is_independent_of_billing", mode: "capacity", want: ErrReferralEvidenceTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPaidReportFixture(t)
			f.capture(t, f.original, "active")
			if tc.mode == "capacity" {
				f.referral.snapshot.History = make([]referral.Referral, ReferralEvidenceCapacity)
			} else {
				first := f.referral.page.Items[0]
				second := first
				second.ReferredCustomer = "second-private-customer"
				second.ID = referral.RelationshipReferenceID(referral.ProgramID, "partner", second.ReferredCustomer)
				second.Periods = append([]referral.OwnershipPeriod(nil), first.Periods...)
				second.Periods[0].ReferralID = "second-private-revision"
				raw := f.original
				raw.PrincipalID = second.ReferredCustomer
				raw.ProviderCustomerID = "cus_private_second"
				raw.PaymentID = "pi_second"
				raw.InvoiceID = "in_second"
				raw.AllocationID = "il_second"
				if tc.mode != "collision" {
					raw.SubscriptionID = "sub_private_second"
				} else {
					// Current customer-bearing writes reject contradictory lifecycle
					// ownership before financial history can be accepted. Preserve
					// the report's independent defense for valid customer-less legacy
					// payments, which intentionally do not create lifecycle sources.
					before := maps.Clone(f.records.rows)
					rejected, err := f.source.AcceptVerified(context.Background(), billing.VerifiedRevenueRequest{Scope: raw.Scope, EnvelopeID: "evt_second", Facts: []billing.RevenueFact{raw}})
					require.ErrorIs(t, err, billing.ErrRevenueConflict)
					require.Zero(t, rejected)
					require.Equal(t, before, f.records.rows)
					raw.ProviderCustomerID = ""
				}
				o, err := f.source.AcceptVerified(context.Background(), billing.VerifiedRevenueRequest{Scope: raw.Scope, EnvelopeID: "evt_second", Facts: []billing.RevenueFact{raw}})
				require.NoError(t, err)
				row, err := f.source.GetRevenueFact(context.Background(), o.FactIDs[0])
				require.NoError(t, err)
				if tc.mode != "collision" {
					f.capture(t, row, "active")
				}
				secondSnapshot := referral.AttributionSnapshot{ReferredCustomer: second.ReferredCustomer, Head: referral.Referral{ID: second.Periods[0].ReferralID, ProgramID: referral.ProgramID, PartnerID: "partner", ReferredCustomer: second.ReferredCustomer}, Fingerprint: "second-owning-snapshot", Bindings: []referral.PaymentAttribution{{ID: "binding_second", ProgramID: referral.ProgramID, PartnerID: "partner", ReferralID: second.Periods[0].ReferralID, ReferredCustomer: second.ReferredCustomer, PaymentID: row.ID, EffectiveAt: row.EffectiveAt, BoundAt: f.clock.Now(), Terms: second.Periods[0].Terms}}}
				f.referral.snapshots = map[string]referral.AttributionSnapshot{first.ReferredCustomer: f.referral.snapshot, second.ReferredCustomer: secondSnapshot}
				f.referral.page.Items = append(f.referral.page.Items, second)
				sort.Slice(f.referral.page.Items, func(i, j int) bool { return f.referral.page.Items[i].ID < f.referral.page.Items[j].ID })
				f.financial.report.Items = []partnerearnings.ReferralAmounts{}
				for _, item := range f.referral.page.Items {
					f.financial.report.Items = append(f.financial.report.Items, partnerearnings.ReferralAmounts{ID: item.ID})
				}
			}
			f.records.readOnly = true
			out, err := f.manager.ReferralSummaries(context.Background(), "owner", ReferralSummaryQuery{Limit: 2})
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Zero(t, out)
				require.Zero(t, f.source.statusCalls)
			} else {
				require.Equal(t, tc.people, out.PaidCoverage.ConfirmedPaidRelationships)
				require.Equal(t, tc.active, out.PaidCoverage.ConfirmedActivePaidSubscriptions)
				require.Len(t, out.Items, 2)
			}
			require.Zero(t, f.referral.bindCalls)
		})
	}
}
