package billingmanager

import (
	"context"
	"testing"

	"github.com/ooaklee/ghatd/external/billing"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// Audit disposition: new named cases prove that current owner authority is
// independent of provider/customer/profile correlation and saved receipts.
func TestUserCheckoutPayerAuthority(t *testing.T) {
	cases := []struct {
		name                string
		accountType, status string
		verified            bool
		id                  string
		outage              error
		want                error
	}{
		{name: "current_verified_self_paying_account", accountType: "INDIVIDUAL", status: "ACTIVE", verified: true},
		{name: "organization_seat_is_not_the_paying_account", accountType: "SEAT", status: "ACTIVE", verified: true, want: ErrBillingManagerUserUnauthorisedToCarryOutOperation},
		{name: "revoked_owner_does_not_recover_receipt", accountType: "INDIVIDUAL", status: "BLOCKED", verified: true, want: ErrBillingManagerUserUnauthorisedToCarryOutOperation},
		{name: "unverified_current_identity_is_not_owner_authority", accountType: "INDIVIDUAL", status: "ACTIVE", want: ErrBillingManagerUserUnauthorisedToCarryOutOperation},
		{name: "wrong_returned_user_identity", accountType: "INDIVIDUAL", status: "ACTIVE", verified: true, id: "another_user", want: billing.ErrRevenueUnavailable},
		{name: "identity_outage_is_not_owner_approval", outage: billing.ErrRevenueUnavailable, want: billing.ErrRevenueUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := tc.id
			if id == "" {
				id = "owning_principal"
			}
			owner := &checkoutUserServiceStub{err: tc.outage, response: &user.GetUserByIDResponse{User: &user.UniversalUser{ID: id, Type: tc.accountType, Status: tc.status, Verification: &user.VerificationStatus{EmailVerified: tc.verified}}}}
			cfg := UserCheckoutPayerConfig{OwningAccountTypes: []string{"INDIVIDUAL"}, ActiveAccountStatuses: []string{"ACTIVE"}}
			authority, err := NewUserCheckoutPayerAuthority(owner, cfg)
			require.NoError(t, err)
			cfg.OwningAccountTypes[0] = "SEAT"
			cfg.ActiveAccountStatuses[0] = "BLOCKED"
			err = authority.AuthorizeCheckoutPayer(context.Background(), "owning_principal")
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
func TestUserCheckoutPayerConstructionAndContext(t *testing.T) {
	cases := []struct {
		name                                                           string
		nilOwner, emptyTypes, duplicateStatuses, nilContext, cancelled bool
		actor                                                          string
		want                                                           error
	}{
		{name: "typed_nil_dependency", nilOwner: true, want: billing.ErrRevenueUnavailable},
		{name: "no_implicit_owning_account_types", emptyTypes: true, want: billing.ErrRevenueInvalid},
		{name: "duplicate_status_config", duplicateStatuses: true, want: billing.ErrRevenueInvalid},
		{name: "nil_context", nilContext: true, want: billing.ErrRevenueInvalid},
		{name: "cancelled_context", cancelled: true, want: context.Canceled},
		{name: "invalid_actor", actor: "bad\nactor", want: billing.ErrRevenueInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var owner *checkoutUserServiceStub
			if !tc.nilOwner {
				owner = &checkoutUserServiceStub{}
			}
			cfg := UserCheckoutPayerConfig{OwningAccountTypes: []string{"INDIVIDUAL"}, ActiveAccountStatuses: []string{"ACTIVE"}}
			if tc.emptyTypes {
				cfg.OwningAccountTypes = nil
			}
			if tc.duplicateStatuses {
				cfg.ActiveAccountStatuses = append(cfg.ActiveAccountStatuses, "ACTIVE")
			}
			authority, err := NewUserCheckoutPayerAuthority(owner, cfg)
			if tc.nilOwner || tc.emptyTypes || tc.duplicateStatuses {
				require.ErrorIs(t, err, tc.want)
				return
			}
			require.NoError(t, err)
			ctx := context.Background()
			if tc.nilContext {
				ctx = nil
			}
			if tc.cancelled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			actor := tc.actor
			if actor == "" {
				actor = "owning_principal"
			}
			require.ErrorIs(t, authority.AuthorizeCheckoutPayer(ctx, actor), tc.want)
		})
	}
}
