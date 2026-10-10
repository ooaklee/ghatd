package partneraccess

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/repository"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Actual owning group and access-policy services/repositories share a borrowed
// native database here. Only the privileged management actor is a controlled
// fixture; this does not certify browser sessions, region approval or platform
// wiring. No production policy/grant or financial record is created.
func TestCohortsNativeOwningMembershipAndPolicy(t *testing.T) {
	type testCase struct {
		name, state string
		want        []string
	}
	for _, tc := range []testCase{
		{name: "accepted_direct_user_with_privileged_admission", want: []string{"group-a"}},
		{name: "owner_with_privileged_admission", state: "owner", want: []string{"group-a"}},
		{name: "invitation_cannot_use_approved_scope", state: "invited", want: []string{}},
		{name: "inactive_group_cannot_use_approved_scope", state: "inactive", want: []string{}},
		{name: "group_typed_reference_cannot_use_approved_scope", state: "group-reference", want: []string{}},
		{name: "ordinary_owned_group_requires_approval", state: "unapproved", want: []string{}},
		{name: "missing_customer_grant_is_explicit_exclusion", state: "missing-grant", want: []string{}},
		{name: "current_native_grant_revocation_is_observed", state: "revoke", want: []string{}},
		{name: "current_native_membership_removal_is_observed", state: "remove", want: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uri := os.Getenv("GHATD_TEST_MONGO_URI")
			if uri == "" {
				t.Skip("set GHATD_TEST_MONGO_URI to an isolated native replica set")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			client, err := mongo.Connect(options.Client().ApplyURI(uri))
			require.NoError(t, err)
			db := client.Database(fmt.Sprintf("hostapp_partner_cohort_%d", time.Now().UnixNano()))
			t.Cleanup(func() {
				cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
				defer done()
				require.NoError(t, db.Drop(cleanup))
				require.NoError(t, client.Disconnect(cleanup))
			})
			core, err := repository.NewMongoDbRepositoryFromDatabase(db, nil)
			require.NoError(t, err)
			groups := group.NewRepository(core)
			row := group.UniversalGroup{ID: "group-a", OwnerID: "another-owner", Status: group.GroupStatusActive, Members: []group.Member{{ID: "user-a", Type: group.MemberTypeUser, Role: group.MemberRoleMember}}}
			switch tc.state {
			case "owner", "unapproved":
				row.OwnerID, row.Members = "user-a", nil
			case "invited":
				row.Members[0].InvitationState = group.MemberInvitationStateInvited
			case "inactive":
				row.Status = group.GroupStatusSuspended
			case "group-reference":
				row.Members[0].Type = group.MemberTypeGroup
			}
			_, err = groups.CreateGroup(ctx, &row)
			require.NoError(t, err)
			policyStore, err := accesspolicy.NewMongoStore(ctx, core)
			require.NoError(t, err)
			require.NoError(t, policyStore.Initialize(ctx))
			policy, err := accesspolicy.NewService(policyStore, func(ctx context.Context, system string) (string, error) {
				if err := ctx.Err(); err != nil {
					return "", err
				}
				if system != "hostapp" {
					return "", accesspolicy.ErrDenied
				}
				return "fixture-policy-admin", nil
			})
			require.NoError(t, err)
			scope, err := CohortScope(row.ID)
			require.NoError(t, err)
			grant := accesspolicy.Grant{Subject: accesspolicy.Subject{System: "hostapp", Kind: accesspolicy.UserSubject, ID: "user-a"}, Enabled: true, Scopes: []string{scope}, Permissions: []string{CohortPermission}}
			if tc.state == "unapproved" {
				grant.Scopes = nil
			}
			if tc.state != "missing-grant" {
				grant, err = policy.ReplaceGrant(ctx, grant, 0)
				require.NoError(t, err)
			}
			cohorts, err := NewCohorts("hostapp", &group.Service{GroupRepository: groups}, policy)
			require.NoError(t, err)
			if tc.state == "revoke" || tc.state == "remove" {
				ids, err := cohorts.PartnerGroupIDs(ctx, "user-a")
				require.NoError(t, err)
				require.Equal(t, []string{row.ID}, ids)
				if tc.state == "revoke" {
					grant.Scopes = nil
					_, err = policy.ReplaceGrant(ctx, grant, grant.Revision)
					require.NoError(t, err)
				} else {
					row.Members = nil
					_, err = groups.UpdateGroup(ctx, &row)
					require.NoError(t, err)
				}
			}
			ids, err := cohorts.PartnerGroupIDs(ctx, "user-a")
			require.NoError(t, err)
			require.Equal(t, tc.want, ids)
		})
	}
}

var _ DirectGroups = (*group.Service)(nil)
