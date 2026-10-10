package partnermanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/referral"
	"github.com/stretchr/testify/require"
)

type globalPaidReferral struct {
	*paidReportReferral
	calls int
	mode  string
}

func (r *globalPaidReferral) GetRelationshipEvidence(ctx context.Context, partner string) (referral.RelationshipEvidence, error) {
	r.calls++
	if err := ctx.Err(); err != nil {
		return referral.RelationshipEvidence{}, err
	}
	if r.mode == "outage" {
		return referral.RelationshipEvidence{}, errors.Join(referral.ErrNotFound, referral.ErrUnavailable)
	}
	if r.mode == "capacity" {
		return referral.RelationshipEvidence{}, referral.ErrCapacity
	}
	out := referral.RelationshipEvidence{ProgramID: referral.ProgramID, PartnerID: partner, Revision: "complete-owning-revision", AsOf: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), Items: []referral.RelationshipEvidenceItem{}}
	for _, item := range r.page.Items {
		snapshot := r.snapshot
		if r.snapshots != nil {
			snapshot = r.snapshots[item.ReferredCustomer]
		}
		out.Items = append(out.Items, referral.RelationshipEvidenceItem{Relationship: item, Attribution: snapshot})
	}
	if r.mode == "changed" && r.calls > 1 {
		out.Revision = "observed-new-membership-or-binding"
	}
	if r.mode == "foreign" {
		out.PartnerID = "other-partner"
	}
	if r.mode == "nil" {
		out.Items = nil
	}
	return out, nil
}

func addGlobalPaidCustomer(t *testing.T, f *paidReportFixture, n int) {
	t.Helper()
	first := f.referral.page.Items[0]
	item := first
	item.ReferredCustomer = fmt.Sprintf("private-global-customer-%03d", n)
	item.ID = referral.RelationshipReferenceID(referral.ProgramID, "partner", item.ReferredCustomer)
	item.Periods = append([]referral.OwnershipPeriod(nil), first.Periods...)
	item.Periods[0].ReferralID = fmt.Sprintf("private-global-revision-%03d", n)
	raw := f.original
	raw.PrincipalID, raw.ProviderCustomerID = item.ReferredCustomer, fmt.Sprintf("cus_global_%03d", n)
	raw.PaymentID, raw.InvoiceID, raw.AllocationID = fmt.Sprintf("pi_global_%03d", n), fmt.Sprintf("in_global_%03d", n), fmt.Sprintf("il_global_%03d", n)
	raw.SubscriptionID = fmt.Sprintf("sub_global_%03d", n)
	o, err := f.source.AcceptVerified(context.Background(), billing.VerifiedRevenueRequest{Scope: raw.Scope, EnvelopeID: fmt.Sprintf("evt_global_%03d", n), Facts: []billing.RevenueFact{raw}})
	require.NoError(t, err)
	row, err := f.source.GetRevenueFact(context.Background(), o.FactIDs[0])
	require.NoError(t, err)
	f.capture(t, row, "active")
	if f.referral.snapshots == nil {
		f.referral.snapshots = map[string]referral.AttributionSnapshot{first.ReferredCustomer: f.referral.snapshot}
	}
	f.referral.snapshots[item.ReferredCustomer] = referral.AttributionSnapshot{ReferredCustomer: item.ReferredCustomer, Head: referral.Referral{ProgramID: referral.ProgramID, PartnerID: "partner", ReferredCustomer: item.ReferredCustomer}, Fingerprint: fmt.Sprintf("owning-snapshot-%03d", n), Bindings: []referral.PaymentAttribution{{ID: fmt.Sprintf("binding-%03d", n), ProgramID: referral.ProgramID, PartnerID: "partner", ReferredCustomer: item.ReferredCustomer, ReferralID: item.Periods[0].ReferralID, PaymentID: row.ID, EffectiveAt: row.EffectiveAt, BoundAt: f.clock.Now(), Terms: item.Periods[0].Terms}}}
	f.referral.page.Items = append(f.referral.page.Items, item)
}

// Audit disposition: related customer/admin reports use isolated real billing
// acceptance/status storage fixtures. Referral owning-port contract is tested
// separately against native Mongo; no source pages are added into global totals.
func TestGlobalPaidReferralMetrics(t *testing.T) {
	for _, admin := range []bool{false, true} {
		for _, tc := range []struct {
			name, mode                                            string
			people, allocations, active, unknown, reads, deferred int
		}{
			{"global_set_exceeds_page_limit", "many", 101, 101, 101, 0, 101, 0},
			{"renewals_do_not_create_people", "renewal", 1, 3, 1, 0, 1, 0},
			{"full_refund_keeps_history_without_current_paid_activity", "refund", 1, 1, 0, 0, 0, 0},
			{"former_owner_retains_history_without_status_read", "former", 1, 1, 0, 0, 0, 0},
			{"status_capacity_preserves_all_paid_people", "status_capacity", 201, 201, 200, 1, 200, 1},
			{"status_outage_preserves_verified_paid_history", "status_outage", 1, 1, 0, 1, 1, 0},
			{"empty_complete_set_has_no_invented_source_clock", "empty", 0, 0, 0, 0, 0, 0},
			{"original_payment_to_is_exclusive", "to", 1, 0, 0, 0, 0, 0},
		} {
			t.Run(fmt.Sprintf("admin_%t/%s", admin, tc.name), func(t *testing.T) {
				f := newPaidReportFixture(t)
				f.capture(t, f.original, "active")
				q := PaidReferralQuery{}
				switch tc.mode {
				case "many", "status_capacity":
					for n := 1; n < tc.people; n++ {
						addGlobalPaidCustomer(t, f, n)
					}
					sort.Slice(f.referral.page.Items, func(i, j int) bool { return f.referral.page.Items[i].ID < f.referral.page.Items[j].ID })
				case "renewal":
					f.addPayment(t, "renewal-one", 10000, f.original.SubscriptionID, true)
					f.addPayment(t, "renewal-two", 10000, f.original.SubscriptionID, true)
				case "refund":
					f.adjust(t, billing.RevenueRefund, 10000, "full-refund")
				case "former":
					f.referral.page.Items[0].Current = false
					f.referral.snapshot.Head.PartnerID = "other"
				case "status_outage":
					f.source.mode = tc.mode
				case "empty":
					f.referral.page.Items = []referral.Relationship{}
				case "to":
					q.To = &f.original.EffectiveAt
				}
				global := &globalPaidReferral{paidReportReferral: f.referral}
				f.manager.deps.Referral = global
				beforeWrites := f.records.writes
				f.records.readOnly = true
				var out PaidReferralReport
				var err error
				if admin {
					out, err = f.manager.AdminPaidReferralMetrics(context.Background(), "operator", "partner", q)
				} else {
					out, err = f.manager.PaidReferralMetrics(context.Background(), "owner", q)
				}
				require.NoError(t, err)
				require.Equal(t, "complete_partner_relationships", out.Scope)
				require.Equal(t, tc.people, out.LifetimeRelationships)
				require.Equal(t, tc.allocations, out.Paid.ConfirmedAllocationRows)
				paid := tc.people
				if tc.mode == "to" {
					paid = 0
				}
				require.Equal(t, paid, out.Paid.ConfirmedPaidRelationships)
				require.Equal(t, tc.active, out.Paid.ConfirmedActivePaidSubscriptions)
				require.Equal(t, tc.unknown, out.Paid.UnknownSubscriptions)
				require.Equal(t, tc.deferred, out.Paid.DeferredStatusReads)
				require.Equal(t, tc.reads, f.source.statusCalls)
				if tc.unknown > 0 {
					require.Nil(t, out.Paid.ActivePaidSubscriptions)
				} else {
					require.NotNil(t, out.Paid.ActivePaidSubscriptions)
					require.Equal(t, tc.active, *out.Paid.ActivePaidSubscriptions)
				}
				require.Equal(t, 2, global.calls, "one complete read plus one complete revision recheck")
				require.Zero(t, f.referral.snapshotCalls, "no per-customer guard transaction")
				require.Zero(t, f.referral.relationshipReadStub.calls, "global totals never depend on a list page")
				require.Zero(t, f.referral.bindCalls)
				require.Equal(t, beforeWrites, f.records.writes)
				if tc.people == 0 {
					require.Nil(t, out.Paid.SourceAsOf)
					require.Zero(t, f.source.historyCalls)
				} else {
					require.Equal(t, 1, f.source.historyCalls, "one complete billing snapshot regardless of principal count")
				}
				bytes, err := json.Marshal(out)
				require.NoError(t, err)
				for _, private := range []string{"private-customer", "private-global-customer", "acct_private", "cus_private", "sub_private", "binding", "private-plan"} {
					require.NotContains(t, string(bytes), private)
				}
			})
		}
	}
}

func TestGlobalPaidReferralFailureBoundaries(t *testing.T) {
	for _, admin := range []bool{false, true} {
		for _, tc := range []struct {
			name, mode string
			want       error
		}{
			{"complete_revision_changed", "changed", referral.ErrStaleWrite},
			{"read_outage_discards_all", "outage", referral.ErrUnavailable},
			{"referral_capacity_discards_all", "capacity", referral.ErrCapacity},
			{"foreign_complete_scope", "foreign", ErrUnavailable},
			{"nil_complete_set", "nil", ErrUnavailable},
			{"billing_capacity_discards_all", "source_capacity", billing.ErrRevenueHistoryTooLarge},
			{"revoked_during_source_join", "revoke", ErrDenied},
			{"denied_before_owning_read", "deny", ErrDenied},
			{"missing_revenue_opt_in", "config", ErrUnavailable},
			{"missing_same_referral_capability", "legacy", ErrUnavailable},
			{"invalid_date_before_read", "date", ErrInvalid},
		} {
			t.Run(fmt.Sprintf("admin_%t/%s", admin, tc.name), func(t *testing.T) {
				f := newPaidReportFixture(t)
				f.capture(t, f.original, "active")
				global := &globalPaidReferral{paidReportReferral: f.referral, mode: tc.mode}
				f.manager.deps.Referral = global
				q := PaidReferralQuery{}
				switch tc.mode {
				case "source_capacity":
					f.source.mode = "capacity"
				case "revoke":
					f.source.revoke = f.authority
				case "deny":
					f.authority.deny = true
				case "config":
					f.manager.revenueReporting = nil
				case "legacy":
					f.manager.deps.Referral = f.referral
				case "date":
					at := time.Time{}
					q.From = &at
				}
				f.records.readOnly = true
				var out PaidReferralReport
				var err error
				if admin {
					out, err = f.manager.AdminPaidReferralMetrics(context.Background(), "operator", "partner", q)
				} else {
					out, err = f.manager.PaidReferralMetrics(context.Background(), "owner", q)
				}
				require.ErrorIs(t, err, tc.want)
				require.Zero(t, out)
				if tc.mode == "deny" || tc.mode == "date" {
					require.Zero(t, global.calls)
				}
				require.Zero(t, f.referral.bindCalls)
			})
		}
	}
}
