package partnermanager

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// Audit disposition: new named owning-service projection cases, separate from
// user/v2's real atomic creation/storage tests and host composition evidence.
type identityOwnerStub struct {
	account *user.UniversalUser
	capture user.SignupAttribution
	err     error
	calls   int
}

func (s *identityOwnerStub) GetUserByID(context.Context, *user.GetUserByIDRequest) (*user.GetUserByIDResponse, error) {
	s.calls++
	return &user.GetUserByIDResponse{User: s.account}, s.err
}
func (s *identityOwnerStub) GetSignupAttribution(context.Context, string) (user.SignupAttribution, error) {
	s.calls++
	return s.capture, s.err
}

type identityRegionStub struct {
	eligible bool
	err      error
	calls    int
}

func (s *identityRegionStub) IsPartnerRegionEligible(context.Context, string) (bool, error) {
	s.calls++
	return s.eligible, s.err
}
func identityAdapterFixture(t *testing.T) (*UserIdentityAdapter, *identityOwnerStub, *identityRegionStub) {
	t.Helper()
	owner := &identityOwnerStub{account: &user.UniversalUser{ID: "customer", Status: "ACTIVE", Type: "individual", Verification: &user.VerificationStatus{EmailVerified: true}}, capture: user.SignupAttribution{ProgramID: "fixture-program", CustomerID: "customer", CreatedAt: time.Date(2026, 10, 7, 12, 0, 0, 123, time.UTC), Individual: true, Evidence: "private-signed-evidence"}}
	region := &identityRegionStub{eligible: true}
	adapter, err := NewUserIdentityAdapter(owner, region, UserIdentityConfig{ProgramID: "fixture-program", IndividualAccountTypes: []string{"individual"}, ActiveAccountStatuses: []string{"ACTIVE"}})
	require.NoError(t, err)
	return adapter, owner, region
}
func TestUserIdentityCurrentEligibility(t *testing.T) {
	type testCase struct {
		name, status, accountType string
		verified, region          bool
		want                      error
		ownerError, regionError   error
		wrongID                   bool
		wantRegionCalls           int
	}
	cases := []testCase{
		{name: "verified_current_individual", status: "ACTIVE", accountType: "individual", verified: true, region: true, wantRegionCalls: 1},
		{name: "revoked_current_account", status: "SUSPENDED", accountType: "individual", verified: true},
		{name: "organization_seat_is_not_individual", status: "ACTIVE", accountType: "seat", verified: true},
		{name: "unverified_email_remains_ineligible", status: "ACTIVE", accountType: "individual"},
		{name: "region_decline_is_explicit", status: "ACTIVE", accountType: "individual", verified: true, wantRegionCalls: 1},
		{name: "owning_lookup_outage_propagates", ownerError: ErrUnavailable, want: ErrUnavailable},
		{name: "joined_absence_and_outage_propagates", ownerError: errors.Join(user.ErrUserNotFound, ErrUnavailable), want: ErrUnavailable},
		{name: "region_outage_is_not_ineligible_zero", status: "ACTIVE", accountType: "individual", verified: true, regionError: ErrUnavailable, want: ErrUnavailable, wantRegionCalls: 1},
		{name: "owning_output_must_match_target", wrongID: true, want: ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, o, r := identityAdapterFixture(t)
			o.account.Status = tc.status
			o.account.Type = tc.accountType
			o.account.Verification.EmailVerified = tc.verified
			o.err = tc.ownerError
			r.eligible = tc.region
			r.err = tc.regionError
			if tc.wrongID {
				o.account.ID = "other-customer"
			}
			got, err := a.GetPartnerPrincipal(context.Background(), "customer")
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Empty(t, got.ID)
			} else {
				require.NoError(t, err)
				require.Equal(t, "customer", got.ID)
				require.Equal(t, tc.status == "ACTIVE", got.Active)
				require.Equal(t, tc.accountType == "individual", got.Individual)
				require.Equal(t, tc.verified, got.EmailVerified)
				require.Equal(t, tc.region, got.RegionEligible)
			}
			require.Equal(t, tc.wantRegionCalls, r.calls)
		})
	}
}
func TestUserIdentityImmutableSignup(t *testing.T) {
	type testCase struct {
		name          string
		mutate        func(*identityOwnerStub)
		want          error
		individual    bool
		emptyEvidence bool
	}
	cases := []testCase{
		{name: "capture_preserves_exact_creation_time_and_evidence", individual: true},
		{name: "current_profile_cannot_reclassify_signup", mutate: func(o *identityOwnerStub) { o.account.Type = "seat"; o.account.Status = "SUSPENDED" }, individual: true},
		{name: "captured_nonindividual_stays_nonindividual", mutate: func(o *identityOwnerStub) { o.capture.Individual = false }},
		{name: "empty_capture_token_is_not_invented", individual: true, emptyEvidence: true, mutate: func(o *identityOwnerStub) { o.capture.Evidence = "" }},
		{name: "legacy_account_without_capture_is_not_new", mutate: func(o *identityOwnerStub) { o.err = user.ErrUserNotFound }, want: user.ErrUserNotFound},
		{name: "creation_lookup_outage_is_not_absence", mutate: func(o *identityOwnerStub) { o.err = ErrUnavailable }, want: ErrUnavailable},
		{name: "capture_customer_must_match", mutate: func(o *identityOwnerStub) { o.capture.CustomerID = "other" }, want: ErrUnavailable},
		{name: "capture_program_must_match", mutate: func(o *identityOwnerStub) { o.capture.ProgramID = "other" }, want: ErrUnavailable},
		{name: "creation_time_required", mutate: func(o *identityOwnerStub) { o.capture.CreatedAt = time.Time{} }, want: ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, o, _ := identityAdapterFixture(t)
			if tc.mutate != nil {
				tc.mutate(o)
			}
			got, err := a.GetSignupFact(context.Background(), "customer")
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.False(t, got.NewAccount)
				return
			}
			require.NoError(t, err)
			require.True(t, got.NewAccount)
			require.Equal(t, tc.individual, got.Individual)
			require.Equal(t, o.capture.CreatedAt, got.CreatedAt)
			require.Equal(t, "customer", got.ID)
			require.Equal(t, o.capture.Evidence, got.AttributionEvidence)
			encoded, err := json.Marshal(got)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "private-signed-evidence")
		})
	}
}
func TestUserIdentityConstructor(t *testing.T) {
	type testCase struct {
		name   string
		owner  UserIdentityService
		region RegionEligibility
		config UserIdentityConfig
		want   error
	}
	owner := &identityOwnerStub{}
	region := &identityRegionStub{}
	cfg := UserIdentityConfig{ProgramID: "fixture-program", IndividualAccountTypes: []string{"individual"}, ActiveAccountStatuses: []string{"ACTIVE"}}
	var nilOwner *identityOwnerStub
	var nilRegion *identityRegionStub
	cases := []testCase{
		{name: "explicit_owning_dependencies", owner: owner, region: region, config: cfg},
		{name: "typed_nil_owner_rejected", owner: nilOwner, region: region, config: cfg, want: ErrUnavailable},
		{name: "typed_nil_region_rejected", owner: owner, region: nilRegion, config: cfg, want: ErrUnavailable},
		{name: "missing_policy_not_defaulted", owner: owner, region: region, want: ErrInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := NewUserIdentityAdapter(tc.owner, tc.region, tc.config)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.Nil(t, a)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
