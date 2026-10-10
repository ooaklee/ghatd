package partneraccess

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/stretchr/testify/require"
)

// The real access-policy service owns exact current approval. Only stored
// grants/errors are faked; this does not certify a country's policy or identity.
func TestRegionsCurrentReviewedAdmission(t *testing.T) {
	outage := errors.New("current region grant unavailable")
	type testCase struct {
		name, state string
		eligible    bool
		wantErr     error
	}
	for _, tc := range []testCase{
		{name: "exact_review_scope_and_permission", eligible: true},
		{name: "scope_without_permission", state: "permission"},
		{name: "permission_without_scope", state: "scope"},
		{name: "generic_admin_is_not_region_review", state: "admin"},
		{name: "cohort_admission_is_not_region_review", state: "cohort"},
		{name: "program_authority_is_not_region_review", state: "program"},
		{name: "wrong_customer", state: "customer"},
		{name: "wrong_system", state: "system"},
		{name: "disabled_grant", state: "disabled"},
		{name: "expired_grant", state: "expired"},
		{name: "sole_known_absence", state: "absent"},
		{name: "sole_wrapped_absence", state: "wrapped"},
		{name: "operational_error_is_not_false_success", state: "outage", wantErr: outage},
		{name: "joined_denial_and_outage_is_not_false_success", state: "joined", wantErr: outage},
		{name: "alias_denial_is_not_established_absence", state: "alias", wantErr: partnermanager.ErrUnavailable},
		{name: "late_cancellation_discards_approval", state: "cancel", wantErr: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &grantStore{grant: accesspolicy.Grant{Subject: accesspolicy.Subject{System: "hostapp", Kind: accesspolicy.UserSubject, ID: "user-a"}, Revision: 1, Enabled: true, Scopes: []string{ReviewedRegionScope}, Permissions: []string{RegionPermission}}}
			policy, err := accesspolicy.NewService(store, nil)
			require.NoError(t, err)
			regions, err := NewRegions("hostapp", policy)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			switch tc.state {
			case "permission":
				store.grant.Permissions = nil
			case "scope":
				store.grant.Scopes = nil
			case "admin":
				store.grant.Permissions = []string{"ADMIN"}
			case "cohort":
				store.grant.Scopes, store.grant.Permissions = []string{ReviewedRegionScope}, []string{CohortPermission}
			case "program":
				store.grant.Scopes = []string{ProgramScope}
			case "customer":
				store.grant.Subject.ID = "user-b"
			case "system":
				store.grant.Subject.System = "another-system"
			case "disabled":
				store.grant.Enabled = false
			case "expired":
				store.grant.ExpiresAt = time.Now().Add(-time.Hour)
			case "absent":
				store.err = accesspolicy.ErrDenied
			case "wrapped":
				store.err = fmt.Errorf("known absent: %w", accesspolicy.ErrDenied)
			case "outage":
				store.err = outage
			case "joined":
				store.err = errors.Join(accesspolicy.ErrDenied, outage)
			case "alias":
				store.err = cohortDenialAlias{}
			case "cancel":
				store.onRead = cancel
			}
			eligible, err := regions.IsPartnerRegionEligible(ctx, "user-a")
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.eligible, eligible)
			require.Equal(t, 1, store.reads)
			require.Zero(t, store.writes)
		})
	}
}

func TestRegionsRevocationOnRepeat(t *testing.T) {
	type testCase struct{ name, state string }
	for _, tc := range []testCase{
		{name: "review_scope_removed", state: "scope"},
		{name: "admission_permission_removed", state: "permission"},
		{name: "grant_disabled", state: "disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &grantStore{grant: accesspolicy.Grant{Subject: accesspolicy.Subject{System: "hostapp", Kind: accesspolicy.UserSubject, ID: "user-a"}, Revision: 1, Enabled: true, Scopes: []string{ReviewedRegionScope}, Permissions: []string{RegionPermission}}}
			policy, err := accesspolicy.NewService(store, nil)
			require.NoError(t, err)
			regions, err := NewRegions("hostapp", policy)
			require.NoError(t, err)
			eligible, err := regions.IsPartnerRegionEligible(t.Context(), "user-a")
			require.NoError(t, err)
			require.True(t, eligible)
			switch tc.state {
			case "scope":
				store.grant.Scopes = nil
			case "permission":
				store.grant.Permissions = nil
			case "disabled":
				store.grant.Enabled = false
			}
			eligible, err = regions.IsPartnerRegionEligible(t.Context(), "user-a")
			require.NoError(t, err)
			require.False(t, eligible, "earlier review is not cached authority")
			require.Equal(t, 2, store.reads)
			require.Zero(t, store.writes)
		})
	}
}

func TestRegionsAdmissionAndConstructionGuards(t *testing.T) {
	type testCase struct{ name, state string }
	for _, tc := range []testCase{
		{name: "nil_context", state: "nil-context"},
		{name: "cancelled_context", state: "cancelled"},
		{name: "empty_customer", state: "empty"},
		{name: "whitespace_customer", state: "whitespace"},
		{name: "invalid_utf8_customer", state: "utf8"},
		{name: "oversized_customer", state: "oversized"},
		{name: "nil_receiver", state: "nil-receiver"},
		{name: "nil_policy", state: "nil-policy"},
		{name: "typed_nil_policy", state: "typed-nil-policy"},
		{name: "invalid_system", state: "invalid-system"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &grantStore{}
			policy, err := accesspolicy.NewService(store, nil)
			require.NoError(t, err)
			regions, err := NewRegions("hostapp", policy)
			require.NoError(t, err)
			ctx, id, want := t.Context(), "user-a", partnermanager.ErrInvalid
			switch tc.state {
			case "nil-context":
				ctx = nil
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = context.Canceled
			case "empty":
				id = ""
			case "whitespace":
				id = "user a"
			case "utf8":
				id = "user\xff"
			case "oversized":
				id = strings.Repeat("u", 257)
			case "nil-receiver":
				regions, want = nil, partnermanager.ErrUnavailable
			case "nil-policy", "typed-nil-policy", "invalid-system":
				var port PolicyService = policy
				system := "hostapp"
				switch tc.state {
				case "nil-policy":
					port = nil
				case "typed-nil-policy":
					var missing *accesspolicy.Service
					port = missing
				default:
					system = "invalid system"
				}
				got, err := NewRegions(system, port)
				require.ErrorIs(t, err, partnermanager.ErrUnavailable)
				require.Nil(t, got)
				require.Zero(t, store.reads)
				require.Zero(t, store.writes)
				return
			}
			eligible, err := regions.IsPartnerRegionEligible(ctx, id)
			require.ErrorIs(t, err, want)
			require.False(t, eligible)
			require.Zero(t, store.reads)
			require.Zero(t, store.writes)
		})
	}
}
