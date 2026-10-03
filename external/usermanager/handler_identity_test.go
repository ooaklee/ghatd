package usermanager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/group"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// identityUsers records the identities used by real manager authorization and
// deletion logic. Only the named admin account has administrative authority.
type identityUsers struct {
	// UserService fails immediately if an unexpected capability is invoked.
	UserService
	// lookedUp and deleted capture calls without mutating a real account.
	lookedUp, deleted []string
}

// GetUserByID resolves fixture accounts and records authorization lookups.
func (s *identityUsers) GetUserByID(_ context.Context, r *userv2.GetUserByIDRequest) (*userv2.GetUserByIDResponse, error) {
	s.lookedUp = append(s.lookedUp, r.ID)
	u := &userv2.UniversalUser{ID: r.ID}
	if r.ID == "admin" {
		u.Roles = []string{"ADMIN"}
	}
	return &userv2.GetUserByIDResponse{User: u}, nil
}

// DeleteUser records the account selected by the orchestration service.
func (s *identityUsers) DeleteUser(_ context.Context, r *userv2.DeleteUserRequest) error {
	s.deleted = append(s.deleted, r.ID)
	return nil
}

// identityTokens records credential cleanup without touching stored tokens.
type identityTokens struct {
	// ApiTokenService supplies unused capabilities which must not be invoked.
	ApiTokenService
	// owners contains each owner selected for credential cleanup.
	owners []string
}

// DeleteApiTokensByOwnerId records the exact owner passed by account deletion.
func (s *identityTokens) DeleteApiTokensByOwnerId(_ context.Context, owner string) error {
	s.owners = append(s.owners, owner)
	return nil
}

// identityGroups exposes a single authorized group and records mutations made
// after real manager authorization. It does not implement domain persistence.
type identityGroups struct {
	// GroupService fails unexpected calls instead of silently accepting them.
	GroupService
	// accessActors records whose group access was evaluated.
	accessActors []string
	// created and updated retain the requests that passed manager authorization.
	created []*group.CreateGroupRequest
	updated []*group.UpdateGroupRequest
}

// GetUserGroupAccessMap grants only the caller access to the route group.
func (s *identityGroups) GetUserGroupAccessMap(_ context.Context, actor string) (map[string]group.UserGroupAccessSummary, error) {
	s.accessActors = append(s.accessActors, actor)
	access := map[string]group.UserGroupAccessSummary{}
	if actor == "caller" {
		access["route-group"] = group.UserGroupAccessSummary{IsAccessible: true, IsAdmin: true}
	}
	return access, nil
}

// CreateGroup records an authorized creation and returns a minimal result.
func (s *identityGroups) CreateGroup(_ context.Context, r *group.CreateGroupRequest) (*group.CreateGroupResponse, error) {
	s.created = append(s.created, r)
	return &group.CreateGroupResponse{Group: &group.UniversalGroup{ID: "created"}}, nil
}

// UpdateGroup records the exact target and payload delivered to the lower domain.
func (s *identityGroups) UpdateGroup(_ context.Context, r *group.UpdateGroupRequest) (*group.UpdateGroupResponse, error) {
	s.updated = append(s.updated, r)
	return &group.UpdateGroupResponse{Group: &group.UniversalGroup{ID: r.ID}}, nil
}

// identityHTTPRequest represents the output of trusted authentication middleware.
// Tests exercise handler/mapper/service composition, not credential verification.
func identityHTTPRequest(actor string, authenticated bool, body string) *http.Request {
	ctx := accessmanagerhelpers.TransitWith(context.Background(), actor)
	ctx = accessmanagerhelpers.TransitAuthenticatedWith(ctx, authenticated)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/ums/groups/route-group", strings.NewReader(body)).WithContext(ctx)
	return mux.SetURLVars(r, map[string]string{"groupID": "route-group"})
}

// TestDeleteSelfUsesVerifiedIdentity follows the real deletion workflow through
// account selection and credential cleanup while all effects remain in memory.
func TestDeleteSelfUsesVerifiedIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, actor, body string
		authenticated     bool
		status            int
		want              []string
	}{
		{"member cannot select victim", "caller", `{"UserId":"victim","ID":"victim","reason":"Leaving"}`, true, 200, []string{"caller"}},
		{"admin still deletes self", "admin", `{"UserId":"victim","ID":"victim"}`, true, 200, []string{"admin"}},
		{"anonymous ID rejected", "caller", `{}`, false, 401, nil},
		{"missing caller rejected", "", `{"UserId":"admin","ID":"victim"}`, true, 401, nil},
		{"malformed body rejected", "caller", `{`, true, 400, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			users, tokens := &identityUsers{}, &identityTokens{}
			h := NewHandler(&NewHandlerRequest{Service: &Service{UserService: users, ApiTokenService: tokens}, Validator: usermanagerAuthTestValidator{}})
			rec := httptest.NewRecorder()
			r := identityHTTPRequest(tc.actor, tc.authenticated, tc.body)
			r.Method = http.MethodDelete
			r.URL.Path = "/api/v1/ums/me"
			h.DeleteUserPermanently(rec, r)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			require.Equal(t, tc.want, users.lookedUp)
			require.Equal(t, tc.want, users.deleted)
			require.Equal(t, tc.want, tokens.owners)
		})
	}
}

// TestGroupMutationAuthorizationUsesBoundIdentity exercises real manager admin
// and access-map checks. Body identities cannot change who is authorized, and
// nested full records cannot redirect the lower-domain update.
func TestGroupMutationAuthorizationUsesBoundIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, operation, actor, body string
		status, writes               int
	}{
		{"forged admin cannot create root", "create", "caller", `{"UserID":"admin","name":"Team"}`, 403, 0},
		{"real admin creates root", "create", "admin", `{"UserID":"caller","name":"Team","owner_id":"chosen"}`, 201, 1},
		{"group admin creates child", "create", "caller", `{"name":"Team","parent_group_id":"route-group"}`, 201, 1},
		{"unrelated user cannot impersonate group admin", "update", "outsider", `{"UserId":"admin","name":"Renamed"}`, 403, 0},
		{"group admin cannot redirect target", "update", "caller", `{"ID":"victim","name":"Renamed","group":{"id":"victim"}}`, 200, 1},
		{"real admin also uses route target", "update", "admin", `{"ID":"victim","name":"Renamed","group":{"id":"victim"}}`, 200, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			users, groups := &identityUsers{}, &identityGroups{}
			h := NewHandler(&NewHandlerRequest{Service: &Service{UserService: users, GroupService: groups}, Validator: usermanagerAuthTestValidator{}})
			rec := httptest.NewRecorder()
			r := identityHTTPRequest(tc.actor, true, tc.body)
			if tc.operation == "create" {
				h.CreateGroup(rec, r)
			} else {
				h.UpdateGroup(rec, r)
			}
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			require.Equal(t, []string{tc.actor}, users.lookedUp)
			require.Equal(t, tc.writes, len(groups.created)+len(groups.updated))
			for _, actor := range groups.accessActors {
				require.Equal(t, tc.actor, actor)
			}
			for _, update := range groups.updated {
				require.Equal(t, "route-group", update.ID)
				require.Nil(t, update.Group)
				require.Equal(t, "Renamed", *update.Name)
			}
		})
	}
}
