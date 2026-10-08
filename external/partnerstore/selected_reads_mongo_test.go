package partnerstore

import (
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/stretchr/testify/require"
)

// Real identity/program/destination/financial owners provide these reads. The
// exact action/target authority port is metadata-only, not policy persistence
// or authenticated host proof. Setup credits are controlled, not provider facts.
func TestMongoSelectedOperatorReadsPreserveOwnerState(t *testing.T) {
	for _, tc := range []struct {
		name, operation string
		destination     bool
		paused, other   bool
		history         bool
	}{
		{"current_partner_status", "status", false, false, false, false},
		{"paused_partner_status", "status", false, true, false, false},
		{"selected_status_cannot_read_another_partner", "status", false, false, true, false},
		{"preparation_missing_destination", "preparation", false, false, false, false},
		{"preparation_current_destination", "preparation", true, false, false, false},
		{"preparation_pause_is_data", "preparation", true, true, false, false},
		{"selected_preparation_cannot_read_another_partner", "preparation", true, false, true, false},
		{"first_individual_publication", "policy", false, false, false, false},
		{"individual_head_includes_future_and_expired_versions", "policy", false, false, false, true},
		{"selected_policy_cannot_read_another_customer", "policy", false, false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := owningWorkerFixture(t)
			p := enrollCorrectionPartner(t, f, "selected-read-owner@example.test")
			_, err := f.earnings.Accrue(f.ctx, partnerearnings.AccrualRequest{PartnerID: p.ID, PaymentID: "fixture-credit", PaymentMinor: 10000, RateBasisPoints: 2000, Currency: "EUR", OccurredAt: f.clock.Now().Add(-8 * 24 * time.Hour)})
			require.NoError(t, err)
			_, err = f.earnings.Mature(f.ctx, p.ID)
			require.NoError(t, err)
			var destination partnerprogram.Destination
			if tc.destination {
				destination, err = f.program.UpdatePayoutDestination(f.ctx, partnerprogram.DestinationRequest{CustomerID: p.CustomerID, Method: "paypal", PayPalEmail: "selected-read-owner@example.test"})
				require.NoError(t, err)
			}
			if tc.paused {
				p, err = f.program.ChangeStatus(f.ctx, partnerprogram.StatusChangeRequest{ActorID: "fixture-operator", PartnerID: p.ID, ExpectedRevision: p.Revision, NewStatus: partnerprogram.StatusSuspended, Reason: "controlled admission pause"})
				require.NoError(t, err)
			}
			if tc.history {
				draft := partnerprogram.PolicyDraft{Scope: "individual", PartnerCustomer: p.CustomerID, RateBasisPoints: 2000, HoldDays: 7, Currency: "EUR", EligiblePlanIDs: []string{"fixture-plan"}, TermsVersion: "fixture-terms", EffectiveFrom: f.clock.Now().Add(24 * time.Hour)}
				_, err = f.program.PublishPolicy(f.ctx, partnerprogram.PublishPolicyRequest{ActorID: "fixture-operator", Draft: draft})
				require.NoError(t, err)
				draft.EffectiveFrom = f.clock.Now().Add(-48 * time.Hour)
				end := f.clock.Now().Add(-24 * time.Hour)
				draft.EffectiveTo = &end
				_, err = f.program.PublishPolicy(f.ctx, partnerprogram.PublishPolicyRequest{ActorID: "fixture-operator", Draft: draft, ExpectedRevision: 1})
				require.NoError(t, err)
			}
			storedPartner, err := f.program.GetPartner(f.ctx, p.ID)
			require.NoError(t, err)
			policies, err := f.program.ListPolicyVersions(f.ctx)
			require.NoError(t, err)
			statement, err := f.earnings.GetStatement(f.ctx, p.ID, partnerearnings.StatementQuery{Limit: 100})
			require.NoError(t, err)
			capability, target := partnermanager.CapabilityPolicy, p.ID
			if tc.operation == "preparation" {
				capability = partnermanager.CapabilityCreateClaimOnBehalf
			}
			if tc.operation == "policy" {
				target = p.CustomerID
			}
			a := &actionClaimAuthority{capability: capability, target: target}
			m, err := partnermanager.NewManager(partnermanager.Dependencies{Program: f.program, Referral: f.referrals, Earnings: f.earnings, Identity: f.identity, Authority: a, Groups: workerGroups{}, Revenue: f.revenue, Evidence: f.signer, Clock: f.clock, Controls: partnermanager.Controls{Claims: !tc.paused}, Claims: partnermanager.ClaimsConfig{MinimumMinor: 100}})
			require.NoError(t, err)
			if tc.other {
				target = "another-selected-target"
			}
			var readErr error
			switch tc.operation {
			case "status":
				var out partnerprogram.Partner
				out, readErr = m.AdminPartnerStatus(f.ctx, "fixture-operator", target)
				if !tc.other {
					require.Equal(t, storedPartner, out)
				}
			case "preparation":
				var out partnermanager.ClaimPreparation
				out, readErr = m.AdminClaimPreparation(f.ctx, "fixture-operator", target)
				if !tc.other {
					require.EqualValues(t, 2000, out.AvailableMinor)
					require.EqualValues(t, 100, out.MinimumMinor)
					require.Equal(t, "EUR", out.Currency)
					require.Equal(t, f.program.Config().CurrencyExponent, out.CurrencyExponent)
					require.True(t, out.IdentityEligible)
					require.Equal(t, !tc.paused, out.ClaimsEnabled)
					require.Equal(t, storedPartner.CanRequestPayouts, out.CanRequestPayouts)
					if tc.destination {
						require.Equal(t, &partnermanager.ClaimPreparationDestination{ID: destination.ID, Method: destination.Method, Email: destination.Email, Version: destination.Version}, out.Destination)
					} else {
						require.Nil(t, out.Destination)
					}
				}
			case "policy":
				var out partnermanager.IndividualPolicyHistory
				out, readErr = m.AdminIndividualPolicyVersions(f.ctx, "fixture-operator", target)
				if !tc.other {
					if tc.history {
						require.EqualValues(t, 2, out.Revision)
						require.Len(t, out.Versions, 2)
						require.EqualValues(t, 1, out.Versions[len(out.Versions)-1].Revision, "native date ordering cannot select the CAS head")
						for _, row := range out.Versions {
							require.Equal(t, p.CustomerID, row.PartnerCustomer)
							require.Equal(t, "individual", row.Scope)
						}
					} else {
						require.NotNil(t, out.Versions)
						require.Empty(t, out.Versions)
						require.Zero(t, out.Revision)
					}
				}
			}
			if tc.other {
				require.ErrorIs(t, readErr, partnermanager.ErrDenied)
				require.Equal(t, 1, a.checks)
			} else {
				require.NoError(t, readErr)
				require.Equal(t, 2, a.checks)
			}
			afterPartner, err := f.program.GetPartner(f.ctx, p.ID)
			require.NoError(t, err)
			require.Equal(t, storedPartner, afterPartner)
			afterPolicies, err := f.program.ListPolicyVersions(f.ctx)
			require.NoError(t, err)
			require.Equal(t, policies, afterPolicies)
			afterStatement, err := f.earnings.GetStatement(f.ctx, p.ID, partnerearnings.StatementQuery{Limit: 100})
			require.NoError(t, err)
			require.Equal(t, statement, afterStatement)
			if tc.destination {
				afterDestination, err := f.program.GetPayoutDestination(f.ctx, p.CustomerID)
				require.NoError(t, err)
				require.Equal(t, destination, afterDestination)
			}
		})
	}
}
