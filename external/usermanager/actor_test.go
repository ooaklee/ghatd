package usermanager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/group"
	"github.com/ritwickdey/querydecoder"
	"github.com/stretchr/testify/require"
)

// actorMapper adapts typed mappers to one contract table without replacing their
// decoding, context binding or validation. Each row calls the production mapper.
func actorMapper[T any](mapRequest func(*http.Request, UsermanagerValidator) (*T, error)) func(*http.Request) (any, error) {
	return func(r *http.Request) (any, error) { return mapRequest(r, usermanagerAuthTestValidator{}) }
}

// TestActorMappersRequireVerifiedContext covers every actor-bearing mapper in
// fender.go. Contact creation is intentionally optional-auth and is covered by
// TestMapRequestToCreateCommsRequestUsesOnlyAuthenticatedUserID instead.
func TestActorMappersRequireVerifiedContext(t *testing.T) {
	for _, endpoint := range []struct {
		name       string
		mapRequest func(*http.Request) (any, error)
	}{
		{"update profile", actorMapper(MapRequestToUpdateUserProfileRequest)},
		{"micro profile", actorMapper(MapRequestToGetUserMicroProfileRequest)},
		{"groups by user", actorMapper(MapRequestToGetGroupsByUserIDRequest)},
		{"user by ID", actorMapper(MapRequestToGetUserByIDRequest)},
		{"users", actorMapper(MapRequestToGetUsersRequest)},
		{"group lineage", actorMapper(MapRequestToGetGroupLineageRequest)},
		{"group descendants", actorMapper(MapRequestToGetGroupDescendantsRequest)},
		{"groups config", actorMapper(MapRequestToGetGroupsConfigRequest)},
		{"profile", actorMapper(MapRequestToGetUserProfileRequest)},
		{"delete account", actorMapper(MapRequestToDeleteUserPermanentlyRequest)},
		{"contacts", actorMapper(mapGetCommsRequest)},
		{"contact stats", actorMapper(mapGetCommsStatsRequest)},
		{"update contact", actorMapper(MapRequestToUpdateCommsRequest)},
		{"enriched profile", actorMapper(MapRequestToGetEnrichedUserProfileRequest)},
		{"user groups", actorMapper(MapRequestToGetUserGroupsRequest)},
		{"notification overviews", actorMapper(MapRequestToGetLatestNotificationOverviewsRequest)},
		{"notifier config", actorMapper(MapRequestToGetNotifierConfigRequest)},
		{"register address", actorMapper(MapRequestToRegisterNotificationAddressRequest)},
		{"list addresses", actorMapper(MapRequestToListNotificationAddressesRequest)},
		{"delete address", actorMapper(MapRequestToDeleteNotificationAddressRequest)},
		{"get preferences", actorMapper(MapRequestToGetNotificationPreferencesRequest)},
		{"update preferences", actorMapper(MapRequestToUpdateNotificationPreferencesRequest)},
		{"notify user", actorMapper(MapRequestToNotifyUserRequest)},
		{"notify users", actorMapper(MapRequestToNotifyUsersRequest)},
		{"invitations", actorMapper(MapRequestToGetMyGroupInvitationsRequest)},
		{"accept invitation", actorMapper(MapRequestToAcceptMyGroupInvitationRequest)},
		{"reject invitation", actorMapper(MapRequestToRejectMyGroupInvitationRequest)},
		{"memberships", actorMapper(MapRequestToGetUserGroupMembershipsRequestRequest)},
		{"group detail", actorMapper(MapRequestToGetGroupDetailRequest)},
		{"group stats", actorMapper(MapRequestToGetGroupStatsRequest)},
		{"create group", actorMapper(MapRequestToCreateGroupRequest)},
		{"update group", actorMapper(MapRequestToUpdateGroupRequest)},
		{"delete group", actorMapper(MapRequestToDeleteGroupRequest)},
		{"add member", actorMapper(MapRequestToAddGroupMemberRequest)},
		{"remove member", actorMapper(MapRequestToRemoveGroupMemberRequest)},
		{"update member", actorMapper(MapRequestToUpdateGroupMemberRequest)},
		{"update owner", actorMapper(MapRequestToUpdateGroupOwnerRequest)},
		{"validate name", actorMapper(MapRequestToValidateGroupNameRequest)},
		{"create reminder", actorMapper(MapRequestToCreateReminderRequest)},
		{"get reminder", actorMapper(MapRequestToGetReminderByIDRequest)},
		{"list reminders", actorMapper(MapRequestToListRemindersRequest)},
		{"update reminder", actorMapper(MapRequestToUpdateReminderByIDRequest)},
		{"delete reminder", actorMapper(MapRequestToDeleteReminderByIDRequest)},
		{"disable reminder", actorMapper(MapRequestToDisableReminderByIDRequest)},
		{"reminder stats", actorMapper(MapRequestToGetReminderStatsRequest)},
		{"due reminders", actorMapper(MapRequestToGetDueRemindersRequest)},
		{"record streak", actorMapper(MapRequestToRecordStreakRequest)},
		{"list streaks", actorMapper(MapRequestToListStreaksRequest)},
		{"current streak", actorMapper(MapRequestToGetCurrentStreakRequest)},
		{"longest streak", actorMapper(MapRequestToGetLongestStreakRequest)},
		{"streak count", actorMapper(MapRequestToGetNumberOfStreaksRequest)},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			for _, tc := range []struct {
				name, id                   string
				authenticated, publishFlag bool
				wantErr                    bool
			}{
				{"verified caller", "trusted-actor", true, true, false},
				{"anonymous placeholder", "rate-limit-placeholder", false, true, true},
				{"ID without authentication result", "trusted-actor", false, false, true},
				{"authentication flag without ID", "", true, true, true},
				{"no authentication context", "", false, false, true},
			} {
				t.Run(tc.name, func(t *testing.T) {
					ctx := accesshelpers.TransitWith(context.Background(), tc.id)
					if tc.publishFlag {
						ctx = accesshelpers.TransitAuthenticatedWith(ctx, tc.authenticated)
					}
					r := httptest.NewRequest(http.MethodPost,
						"/api/v1/ums/me?ActorID=forged&actorid=forged&actor_id=forged&-=forged&UserId=forged&user_id=selected-target",
						strings.NewReader(`{"ActorID":"forged","actorid":"forged","actor_id":"forged","UserId":"forged","user_id":"forged"}`)).WithContext(ctx)
					r = mux.SetURLVars(r, map[string]string{"id": "route-contact", "userId": "route-user", "groupID": "route-group", "memberID": "route-member", "addressID": "route-address", "reminderID": "route-reminder"})
					mapped, err := endpoint.mapRequest(r)
					if tc.wantErr {
						require.ErrorIs(t, err, ErrUnableToIdentifyUser)
						return
					}
					require.NoError(t, err)
					value := reflect.ValueOf(mapped).Elem()
					require.Equal(t, tc.id, value.FieldByName("ActorID").String())
					assertActorTransportContract(t, value.Type())
				})
			}
		})
	}
}

// assertActorTransportContract checks direct ownership and real codec behavior.
// querydecoder ignores untagged fields but treats query:"-" as a literal key;
// actor fields therefore deliberately have no query tag.
func assertActorTransportContract(t *testing.T, typ reflect.Type) {
	t.Helper()
	field, ok := typ.FieldByName("ActorID")
	require.True(t, ok)
	require.Len(t, field.Index, 1, "actor must be owned by the manager request")
	require.Equal(t, "-", field.Tag.Get("json"))
	require.Empty(t, field.Tag.Get("query"))
	require.Empty(t, field.Tag.Get("path"))
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.Anonymous {
			continue
		}
		embedded := f.Type
		if embedded.Kind() == reflect.Pointer {
			embedded = embedded.Elem()
		}
		_, hasActor := embedded.FieldByName("ActorID")
		require.False(t, hasActor, "embedded actor would create two authority sources")
	}
	value := reflect.New(typ)
	value.Elem().FieldByName("ActorID").SetString("server-bound-actor")
	require.NoError(t, json.Unmarshal([]byte(`{"ActorID":"forged","actorid":"forged","actor_id":"forged"}`), value.Interface()))
	require.NoError(t, querydecoder.New(url.Values{"ActorID": {"forged"}, "actor_id": {"forged"}, "-": {"forged"}}).Decode(value.Interface()))
	require.Equal(t, "server-bound-actor", value.Elem().FieldByName("ActorID").String())
	encoded, err := json.Marshal(value.Interface())
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "server-bound-actor")
}

// TestNonFenderActorContracts covers requests not mapped through fender.go:
// handles use their stricter session-only mapper; insights have no public route.
func TestNonFenderActorContracts(t *testing.T) {
	for _, tc := range []any{MyHandleRequest{}, GetUserInsightsUsageRequest{}} {
		typ := reflect.TypeOf(tc)
		t.Run(typ.Name(), func(t *testing.T) { assertActorTransportContract(t, typ) })
	}
}

// actorTargetGroups records the lower-domain target independently of the actor
// used by User Manager's real administrative permission check.
type actorTargetGroups struct {
	// GroupService fails unexpected capability calls rather than accepting them.
	GroupService
	// requests records the exact target queries admitted by the real manager.
	requests []*group.GetGroupsByUserIDRequest
}

// GetGroupsByUserID records the query without persistence or extra authorization.
func (g *actorTargetGroups) GetGroupsByUserID(_ context.Context, r *group.GetGroupsByUserIDRequest) (*group.GetGroupsByUserIDResponse, error) {
	g.requests = append(g.requests, r)
	return &group.GetGroupsByUserIDResponse{}, nil
}

// TestGroupLookupSeparatesActorAndTarget verifies that a target identity is not
// promoted to authority and that self/admin authorization remains unchanged.
func TestGroupLookupSeparatesActorAndTarget(t *testing.T) {
	for _, tc := range []struct {
		name, actor, target string
		denied              bool
	}{
		{"self", "caller", "caller", false},
		{"admin acting on different user", "admin", "target", false},
		{"member cannot impersonate admin target", "caller", "admin", true},
		{"member cannot select another user", "caller", "target", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			users, groups := &identityUsers{}, &actorTargetGroups{}
			service := &Service{UserService: users, GroupService: groups}
			_, err := service.GetGroupsByUserID(context.Background(), &GetGroupsByUserIDRequest{ActorID: tc.actor, GetGroupsByUserIDRequest: &group.GetGroupsByUserIDRequest{UserID: tc.target}})
			if tc.denied {
				require.ErrorIs(t, err, group.ErrInsufficientPermissions)
				require.Empty(t, groups.requests)
			} else {
				require.NoError(t, err)
				require.Len(t, groups.requests, 1)
				require.Equal(t, tc.target, groups.requests[0].UserID)
			}
			if tc.actor != tc.target {
				require.Equal(t, []string{tc.actor}, users.lookedUp)
			}
		})
	}
}

// TestMembershipProjectionKeepsTargetsDistinct exercises self-service actor
// binding and internal target-only enrichment without manufacturing an actor.
func TestMembershipProjectionKeepsTargetsDistinct(t *testing.T) {
	for _, tc := range []struct {
		name, operation, user string
		descendants, prefix   bool
	}{
		{"self service", "self", "caller", true, true},
		{"target enrichment", "all", "target", false, true},
		{"typed target enrichment", "type", "target", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groups := &actorTargetGroups{}
			service := &Service{GroupService: groups}
			var err error
			switch tc.operation {
			case "self":
				_, err = service.GetUserGroupMemberships(context.Background(), &GetUserGroupMembershipsRequest{ActorID: tc.user, IncludeDescendants: tc.descendants, PrefixName: tc.prefix})
			case "all":
				_, err = service.getUserAllGroups(context.Background(), tc.user, tc.prefix)
			case "type":
				_, err = service.getUserGroupsByType(context.Background(), tc.user, "TEAM")
			}
			require.NoError(t, err)
			require.Equal(t, []*group.GetGroupsByUserIDRequest{{UserID: tc.user, IncludeDescendants: tc.descendants, PrefixName: tc.prefix}}, groups.requests)
		})
	}
}
