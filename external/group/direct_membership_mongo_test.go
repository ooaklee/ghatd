package group

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/repository"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Each named case exercises the actual bounded repository and owning service
// against its own native fixture. No memongo fallback or datastore mock can
// stand in for the query/extra-row contract.
func TestBoundedGroupReferencesNativeMembership(t *testing.T) {
	type testCase struct {
		name, userID string
		limit, rows  int
		want         []string
	}
	for _, tc := range []testCase{
		{name: "one_row_plus_probe", userID: "user-a", limit: 1, rows: 2, want: []string{"group-a", "group-b"}},
		{name: "two_rows_plus_probe_include_invitation", userID: "user-a", limit: 2, rows: 3, want: []string{"group-a", "group-b"}},
		{name: "exact_complete_reference_set", userID: "user-a", limit: 5, rows: 5, want: []string{"group-a", "group-b"}},
		{name: "spare_capacity_does_not_invent_rows", userID: "user-a", limit: 6, rows: 5, want: []string{"group-a", "group-b"}},
		{name: "empty_reference_set_is_explicit", userID: "unknown-user", limit: 2, want: []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uri := os.Getenv("GHATD_TEST_MONGO_URI")
			if uri == "" {
				t.Skip("set GHATD_TEST_MONGO_URI to an isolated native MongoDB")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			client, err := mongo.Connect(options.Client().ApplyURI(uri))
			require.NoError(t, err)
			db := client.Database(fmt.Sprintf("ghatd_direct_membership_%d", time.Now().UnixNano()))
			t.Cleanup(func() {
				cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
				defer done()
				require.NoError(t, db.Drop(cleanup))
				require.NoError(t, client.Disconnect(cleanup))
			})
			store, err := repository.NewMongoDbRepositoryFromDatabase(db, nil)
			require.NoError(t, err)
			repo := NewRepository(store)
			for _, row := range []UniversalGroup{
				{ID: "group-a", Status: GroupStatusActive, OwnerID: "user-a"},
				{ID: "group-b", Status: GroupStatusActive, Members: []Member{{ID: "user-a", Type: MemberTypeUser, Role: MemberRoleMember}}},
				{ID: "group-c", Status: GroupStatusActive, Members: []Member{{ID: "user-a", Type: MemberTypeUser, InvitationState: MemberInvitationStateInvited}}},
				{ID: "group-d", Status: GroupStatusSuspended, OwnerID: "user-a"},
				{ID: "group-e", Status: GroupStatusActive, Members: []Member{{ID: "user-a", Type: MemberTypeGroup}}},
				{ID: "unrelated-group", Status: GroupStatusActive, OwnerID: "another-user"},
				{ID: "deleted-group", Status: GroupStatusActive, OwnerID: "user-a", Metadata: &GroupMetadata{DeletedAt: "2026-10-07T12:00:00Z"}},
			} {
				if row.Metadata == nil {
					row.Metadata = &GroupMetadata{}
				}
				// CreateGroup persists supplied metadata without assigning time.
				// Make the equal-time tie explicit for this query contract fixture.
				row.Metadata.CreatedAt = "2026-10-07T10:00:00Z"
				_, err := repo.CreateGroup(ctx, &row)
				require.NoError(t, err)
			}
			rows, err := repo.GetGroupsByReferencedUserIDBounded(ctx, tc.userID, tc.limit)
			require.NoError(t, err)
			require.NotNil(t, rows)
			require.Len(t, rows, tc.rows)
			if tc.rows > 0 {
				require.Equal(t, "group-a", rows[0].ID, "bounded equal-time rows have stable ID order")
			}
			service := &Service{GroupRepository: repo}
			ids, err := service.GetActiveDirectGroupIDs(ctx, tc.userID)
			require.NoError(t, err)
			require.Equal(t, tc.want, ids, "owning service excludes invitations, group references, inactive and deleted groups")
			legacy, err := repo.GetGroupsByReferencedUserID(ctx, tc.userID)
			require.NoError(t, err)
			if tc.userID == "user-a" {
				require.Len(t, legacy, 5, "legacy query remains unbounded and keeps its reference selector")
			} else {
				require.Empty(t, legacy)
			}
		})
	}
}

func TestBoundedGroupReferencesRejectInvalidInput(t *testing.T) {
	type testCase struct {
		name, state string
		limit       int
		want        error
	}
	for _, tc := range []testCase{
		{name: "zero_limit", limit: 0, want: ErrInvalidQueryParam},
		{name: "over_limit", limit: DirectMembershipCapacity + 1, want: ErrInvalidQueryParam},
		{name: "empty_user", state: "empty-user", limit: 1, want: ErrInvalidQueryParam},
		{name: "nil_context", state: "nil-context", limit: 1, want: ErrInvalidQueryParam},
		{name: "cancelled_context", state: "cancelled", limit: 1, want: context.Canceled},
		{name: "nil_repository", state: "nil-repo", limit: 1, want: ErrDirectMembershipUnavailable},
		{name: "typed_nil_store", state: "nil-store", limit: 1, want: ErrDirectMembershipUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, ctx, userID := &Repository{}, t.Context(), "user-a"
			switch tc.state {
			case "empty-user":
				userID = ""
			case "nil-context":
				ctx = nil
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "nil-repo":
				repo = nil
			case "nil-store":
				var missing *repository.MongoDbRepository
				repo.Store = missing
			}
			rows, err := repo.GetGroupsByReferencedUserIDBounded(ctx, userID, tc.limit)
			require.ErrorIs(t, err, tc.want)
			require.Nil(t, rows)
		})
	}
}
