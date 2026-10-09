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

// Only current membership evidence is faked. Owning group invitation/status
// projection and native capacity probes have separate owning package tests.
// The real access-policy service evaluates stored grants, never a boolean fake.
type cohortGroups struct {
	ids    []string
	err    error
	reads  int
	onRead func(int)
}

func (g *cohortGroups) GetActiveDirectGroupIDs(_ context.Context, customerID string) ([]string, error) {
	g.reads++
	if g.onRead != nil {
		g.onRead(g.reads)
	}
	if customerID != "user-a" {
		return nil, partnermanager.ErrInvalid
	}
	return g.ids, g.err
}

func cohortFixture(t *testing.T, ids []string) (*Cohorts, *cohortGroups, *grantStore) {
	t.Helper()
	scopes := make([]string, 0, len(ids))
	for _, id := range ids {
		scope, err := CohortScope(id)
		require.NoError(t, err)
		scopes = append(scopes, scope)
	}
	store := &grantStore{grant: accesspolicy.Grant{Subject: accesspolicy.Subject{System: "hostapp", Kind: accesspolicy.UserSubject, ID: "user-a"}, Revision: 1, Enabled: true, Scopes: scopes, Permissions: []string{CohortPermission}}}
	policy, err := accesspolicy.NewService(store, nil)
	require.NoError(t, err)
	groups := &cohortGroups{ids: ids}
	cohorts, err := NewCohorts("hostapp", groups, policy)
	require.NoError(t, err)
	return cohorts, groups, store
}

func TestCohortsCurrentMembershipAndPrivilegedApproval(t *testing.T) {
	type testCase struct {
		name, state string
		want        []string
		policyReads int
	}
	for _, tc := range []testCase{
		{name: "explicit_customer_cohort_admission", want: []string{"group-a"}, policyReads: 2},
		{name: "self_created_membership_confers_no_approval", state: "unapproved", want: []string{}, policyReads: 1},
		{name: "scope_requires_independent_permission", state: "permission", want: []string{}, policyReads: 1},
		{name: "generic_administrator_label_cannot_approve", state: "admin", want: []string{}, policyReads: 1},
		{name: "program_scope_cannot_approve_cohort", state: "program", want: []string{}, policyReads: 1},
		{name: "another_group_scope_cannot_approve", state: "wrong-group", want: []string{}, policyReads: 1},
		{name: "another_customer_grant_cannot_approve", state: "wrong-customer", want: []string{}, policyReads: 1},
		{name: "another_system_grant_cannot_approve", state: "wrong-system", want: []string{}, policyReads: 1},
		{name: "disabled_grant_has_no_approved_cohorts", state: "disabled", want: []string{}, policyReads: 1},
		{name: "expired_grant_has_no_approved_cohorts", state: "expired", want: []string{}, policyReads: 1},
		{name: "sole_wrapped_denial_is_explicit_exclusion", state: "wrapped-denial", want: []string{}, policyReads: 1},
		{name: "empty_owning_membership_is_explicit", state: "empty", want: []string{}},
		{name: "two_scopes_confirmed_in_one_grant", state: "two", want: []string{"group-a", "group-b"}, policyReads: 3},
		{name: "unapproved_membership_is_excluded", state: "partial", want: []string{"group-a"}, policyReads: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cohorts, groups, store := cohortFixture(t, []string{"group-a"})
			switch tc.state {
			case "unapproved":
				store.grant.Scopes = nil
			case "permission":
				store.grant.Permissions = nil
			case "admin":
				store.grant.Permissions = []string{"ADMIN"}
			case "program":
				store.grant.Scopes = []string{ProgramScope}
			case "wrong-group":
				scope, err := CohortScope("group-b")
				require.NoError(t, err)
				store.grant.Scopes = []string{scope}
			case "wrong-customer":
				store.grant.Subject.ID = "user-b"
			case "wrong-system":
				store.grant.Subject.System = "another-system"
			case "disabled":
				store.grant.Enabled = false
			case "expired":
				store.grant.ExpiresAt = time.Now().Add(-time.Hour)
			case "wrapped-denial":
				store.err = fmt.Errorf("known grant absence: %w", accesspolicy.ErrDenied)
			case "empty":
				groups.ids = []string{}
			case "two", "partial":
				groups.ids = []string{"group-a", "group-b"}
				if tc.state == "two" {
					scope, err := CohortScope("group-b")
					require.NoError(t, err)
					store.grant.Scopes = append(store.grant.Scopes, scope)
				}
			}
			ids, err := cohorts.PartnerGroupIDs(t.Context(), "user-a")
			require.NoError(t, err)
			require.Equal(t, tc.want, ids)
			require.Equal(t, 2, groups.reads)
			require.Equal(t, tc.policyReads, store.reads)
			require.Zero(t, store.writes)
		})
	}
}

type cohortDenialAlias struct{}

func (cohortDenialAlias) Error() string        { return "unestablished denial alias" }
func (cohortDenialAlias) Is(target error) bool { return target == accesspolicy.ErrDenied }

func TestCohortsDiscardIncompleteOrChangingEvidence(t *testing.T) {
	outage := errors.New("owning source outage")
	type testCase struct {
		name, state string
		want        error
		policyReads int
	}
	for _, tc := range []testCase{
		{name: "membership_outage", state: "group-outage", want: outage},
		{name: "nil_membership_is_not_empty", state: "nil", want: partnermanager.ErrUnavailable},
		{name: "malformed_group", state: "invalid", want: partnermanager.ErrUnavailable},
		{name: "duplicate_group", state: "duplicate", want: partnermanager.ErrUnavailable},
		{name: "unsorted_evidence", state: "unsorted", want: partnermanager.ErrUnavailable},
		{name: "complete_budget_before_any_policy_read", state: "overcap", want: partnermanager.ErrUnavailable},
		{name: "policy_outage_cannot_be_empty_approval", state: "policy-outage", want: outage, policyReads: 1},
		{name: "joined_denial_outage_is_not_exclusion", state: "joined", want: outage, policyReads: 1},
		{name: "is_only_denial_is_not_exclusion", state: "alias", want: partnermanager.ErrUnavailable, policyReads: 1},
		{name: "membership_removed_after_selection", state: "removed", want: partnermanager.ErrUnavailable, policyReads: 1},
		{name: "source_reusing_slice_cannot_rewrite_first_snapshot", state: "reused-slice", want: partnermanager.ErrUnavailable, policyReads: 1},
		{name: "membership_added_after_selection", state: "added", want: partnermanager.ErrUnavailable, policyReads: 1},
		{name: "late_membership_outage", state: "late-outage", want: outage, policyReads: 1},
		{name: "late_membership_nil", state: "late-nil", want: partnermanager.ErrUnavailable, policyReads: 1},
		{name: "revoked_before_final_combined_confirmation", state: "revoked", want: partnermanager.ErrUnavailable, policyReads: 2},
		{name: "incompatible_per_group_grant_revisions", state: "mixed-revision", want: partnermanager.ErrUnavailable, policyReads: 3},
		{name: "cancel_during_initial_membership_read", state: "group-cancel", want: context.Canceled},
		{name: "cancel_during_policy_read", state: "policy-cancel", want: context.Canceled, policyReads: 1},
		{name: "cancel_during_final_confirmation", state: "final-cancel", want: context.Canceled, policyReads: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cohorts, groups, store := cohortFixture(t, []string{"group-a"})
			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)
			switch tc.state {
			case "group-outage":
				groups.err = outage
			case "nil":
				groups.ids = nil
			case "invalid":
				groups.ids = []string{"malformed group"}
			case "duplicate":
				groups.ids = []string{"group-a", "group-a"}
			case "unsorted":
				groups.ids = []string{"group-b", "group-a"}
			case "overcap":
				groups.ids = make([]string, cohortCandidateCapacity+1)
			case "policy-outage":
				store.err = outage
			case "joined":
				store.err = errors.Join(accesspolicy.ErrDenied, outage)
			case "alias":
				store.err = cohortDenialAlias{}
			case "removed", "added", "reused-slice", "late-outage", "late-nil", "revoked":
				groups.onRead = func(read int) {
					if read != 2 {
						return
					}
					switch tc.state {
					case "removed":
						groups.ids = []string{}
					case "added":
						groups.ids = append(groups.ids, "group-b")
					case "reused-slice":
						groups.ids[0] = "group-b"
					case "late-outage":
						groups.err = outage
					case "late-nil":
						groups.ids = nil
					case "revoked":
						store.grant.Scopes = nil
					}
				}
			case "mixed-revision":
				groups.ids = []string{"group-a", "group-b"}
				store.onRead = func() {
					if store.reads == 2 {
						scope, err := CohortScope("group-b")
						require.NoError(t, err)
						store.grant.Scopes = []string{scope}
						store.grant.Revision++
					}
				}
			case "group-cancel":
				groups.onRead = func(int) { cancel() }
			case "policy-cancel":
				store.onRead = cancel
			case "final-cancel":
				store.onRead = func() {
					if store.reads == 2 {
						cancel()
					}
				}
			}
			ids, err := cohorts.PartnerGroupIDs(ctx, "user-a")
			require.ErrorIs(t, err, tc.want)
			require.Nil(t, ids, "no partial current approvals on any failure")
			require.Equal(t, tc.policyReads, store.reads)
			require.Zero(t, store.writes)
		})
	}
}

func TestCohortsInvalidAdmissionAndConstruction(t *testing.T) {
	type testCase struct{ name, state string }
	for _, tc := range []testCase{
		{name: "nil_context", state: "nil-context"},
		{name: "cancelled_context", state: "cancelled"},
		{name: "empty_customer", state: "empty"},
		{name: "malformed_customer", state: "invalid"},
		{name: "oversized_customer", state: "oversized"},
		{name: "nil_receiver", state: "nil-receiver"},
		{name: "typed_nil_groups", state: "nil-groups"},
		{name: "typed_nil_policy", state: "nil-policy"},
		{name: "invalid_system", state: "invalid-system"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cohorts, groups, store := cohortFixture(t, []string{"group-a"})
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
			case "invalid":
				id = "user a"
			case "oversized":
				id = strings.Repeat("u", 257)
			case "nil-receiver":
				cohorts, want = nil, partnermanager.ErrUnavailable
			case "nil-groups", "nil-policy", "invalid-system":
				policy, err := accesspolicy.NewService(store, nil)
				require.NoError(t, err)
				var groupPort DirectGroups = groups
				var policyPort PolicyService = policy
				system := "hostapp"
				switch tc.state {
				case "nil-groups":
					var missing *cohortGroups
					groupPort = missing
				case "nil-policy":
					var missing *accesspolicy.Service
					policyPort = missing
				default:
					system = "invalid system"
				}
				got, err := NewCohorts(system, groupPort, policyPort)
				require.ErrorIs(t, err, partnermanager.ErrUnavailable)
				require.Nil(t, got)
				require.Zero(t, groups.reads)
				require.Zero(t, store.reads)
				require.Zero(t, store.writes)
				return
			}
			ids, err := cohorts.PartnerGroupIDs(ctx, id)
			require.ErrorIs(t, err, want)
			require.Nil(t, ids)
			require.Zero(t, groups.reads)
			require.Zero(t, store.reads)
			require.Zero(t, store.writes)
		})
	}
}

func TestCohortsRecheckOnRepeat(t *testing.T) {
	type testCase struct{ name, state string }
	for _, tc := range []testCase{
		{name: "membership_removed", state: "membership"},
		{name: "admission_revoked", state: "scope"},
		{name: "grant_disabled", state: "disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cohorts, groups, store := cohortFixture(t, []string{"group-a"})
			ids, err := cohorts.PartnerGroupIDs(t.Context(), "user-a")
			require.NoError(t, err)
			require.Equal(t, []string{"group-a"}, ids)
			groups.reads, store.reads = 0, 0
			switch tc.state {
			case "membership":
				groups.ids = []string{}
			case "scope":
				store.grant.Scopes = nil
			case "disabled":
				store.grant.Enabled = false
			}
			ids, err = cohorts.PartnerGroupIDs(t.Context(), "user-a")
			require.NoError(t, err)
			require.Equal(t, []string{}, ids, "an earlier approval is not cached authority")
			require.Equal(t, 2, groups.reads)
			if tc.state == "membership" {
				require.Zero(t, store.reads)
			} else {
				require.Equal(t, 1, store.reads)
			}
			require.Zero(t, store.writes)
		})
	}
}

func TestCohortScopeIdentity(t *testing.T) {
	type testCase struct {
		name, id string
		valid    bool
	}
	for _, tc := range []testCase{
		{name: "exact_group", id: "group-a", valid: true},
		{name: "punctuation_group", id: "group:a/b", valid: true},
		{name: "unicode_group", id: "grüppe", valid: true},
		{name: "empty"},
		{name: "whitespace", id: " group-a"},
		{name: "control", id: "group\x00a"},
		{name: "invalid_utf8", id: "group\xff"},
		{name: "oversized", id: strings.Repeat("g", 257)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scope, err := CohortScope(tc.id)
			if !tc.valid {
				require.ErrorIs(t, err, partnermanager.ErrInvalid)
				require.Empty(t, scope)
				return
			}
			require.NoError(t, err)
			repeated, err := CohortScope(tc.id)
			require.NoError(t, err)
			require.Equal(t, scope, repeated)
			other, err := CohortScope(tc.id + "-different")
			require.NoError(t, err)
			require.NotEqual(t, scope, other)
			require.True(t, strings.HasPrefix(scope, "partners.cohort."))
			require.NotEqual(t, ProgramScope, scope)
		})
	}
}
