package usermanager

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/group"
	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/notifier"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

type enrichmentUserServiceStub struct {
	users         map[string]userv2.UniversalUser
	getUsersCalls [][]string
	failCalls     map[int]error
	getByIDCalls  []string
}

// enrichmentLookupRepository bridges existing fixtures to the real user-domain
// lookup, so consumer tests cannot accidentally reimplement its batching.
type enrichmentLookupRepository struct {
	userv2.UserRepository
	list func(context.Context, *userv2.GetUsersRequest) (*userv2.GetUsersResponse, error)
}

func (p *enrichmentLookupRepository) GetUsers(ctx context.Context, req *userv2.GetUsersRequest) ([]userv2.UniversalUser, error) {
	response, err := p.list(ctx, req)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, userv2.ErrDatabaseError
	}
	return response.Users, nil
}

func (s *enrichmentUserServiceStub) GetUsersByIDs(ctx context.Context, req *userv2.GetUsersByIDsRequest) (*userv2.GetUsersByIDsResponse, error) {
	service := userv2.NewService(&enrichmentLookupRepository{list: s.GetUsers}, nil, nil, nil, nil, nil, "")
	return service.GetUsersByIDs(ctx, req)
}

func (*enrichmentUserServiceStub) GetUserMicroProfile(context.Context, *userv2.GetUserMicroProfileRequest) (*userv2.GetUserMicroProfileResponse, error) {
	return nil, errors.New("not implemented")
}

func (*enrichmentUserServiceStub) GetUserProfile(context.Context, *userv2.GetUserProfileRequest) (*userv2.GetUserProfileResponse, error) {
	return nil, errors.New("not implemented")
}

func (s *enrichmentUserServiceStub) GetUserByID(_ context.Context, req *userv2.GetUserByIDRequest) (*userv2.GetUserByIDResponse, error) {
	s.getByIDCalls = append(s.getByIDCalls, req.ID)
	user, ok := s.users[req.ID]
	if !ok {
		return nil, userv2.ErrUserNotFound
	}
	return &userv2.GetUserByIDResponse{User: &user}, nil
}

func (s *enrichmentUserServiceStub) GetUsers(_ context.Context, req *userv2.GetUsersRequest) (*userv2.GetUsersResponse, error) {
	callIndex := len(s.getUsersCalls)
	s.getUsersCalls = append(s.getUsersCalls, append([]string(nil), req.IDsFilter...))
	if err := s.failCalls[callIndex]; err != nil {
		return nil, err
	}

	users := make([]userv2.UniversalUser, 0, len(req.IDsFilter))
	for _, userID := range req.IDsFilter {
		if user, ok := s.users[userID]; ok {
			users = append(users, user)
		}
	}
	return &userv2.GetUsersResponse{Users: users}, nil
}

func (*enrichmentUserServiceStub) GetUserByEmail(context.Context, *userv2.GetUserByEmailRequest) (*userv2.GetUserByEmailResponse, error) {
	return nil, errors.New("not implemented")
}

func (*enrichmentUserServiceStub) UpdateUser(context.Context, *userv2.UpdateUserRequest) (*userv2.UpdateUserResponse, error) {
	return nil, errors.New("not implemented")
}

func (*enrichmentUserServiceStub) DeleteUser(context.Context, *userv2.DeleteUserRequest) error {
	return errors.New("not implemented")
}

func observedEnrichmentContext() (context.Context, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)
	return ghatdlogger.TransitWith(context.Background(), zap.New(core)), logs
}

// TestLoadUsersForEnrichment verifies UMS's fallback policy over the real domain
// lookup. Lower-domain tests own the detailed batching/error contract.
func TestLoadUsersForEnrichment(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		count                          int
		missing                        bool
		failures                       map[int]error
		wantUsers, wantCalls, wantLogs int
	}{
		{"empty", 0, false, nil, 0, 0, 0},
		{"missing is expected", 1, true, nil, 1, 1, 0},
		{"multiple batches", 201, false, nil, 201, 3, 0},
		{"partial successes", 201, false, map[int]error{1: errors.New("private diagnostic")}, 101, 3, 1},
		{"one fallback for multiple failures", 201, false, map[int]error{0: errors.New("private diagnostic"), 1: errors.New("private diagnostic")}, 1, 3, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			port := &enrichmentUserServiceStub{users: map[string]userv2.UniversalUser{}, failCalls: tc.failures}
			ids := make([]string, tc.count)
			for i := range ids {
				ids[i] = fmt.Sprintf("user-%03d", i)
				port.users[ids[i]] = userv2.UniversalUser{ID: ids[i]}
			}
			if tc.missing {
				ids = append(ids, "missing", ids[0])
			}
			ctx, logs := observedEnrichmentContext()
			got := (&Service{UserService: port}).loadUsersForEnrichment(ctx, ids, "test-user-enrichment")
			require.Len(t, got, tc.wantUsers)
			require.Len(t, port.getUsersCalls, tc.wantCalls)
			require.Equal(t, tc.wantLogs, logs.FilterMessage("user-enrichment-unavailable").Len())
			require.NotContains(t, got, "missing")
			for batchIndex, batch := range port.getUsersCalls {
				require.LessOrEqual(t, len(batch), 100)
				for _, id := range batch {
					if id == "missing" {
						continue
					}
					_, found := got[id]
					require.Equal(t, tc.failures[batchIndex] == nil, found)
				}
			}
			for _, event := range logs.All() {
				require.NotContains(t, fmt.Sprint(event.ContextMap()), "private diagnostic")
			}
		})
	}
}

func TestEnrichGroupDetailUsersBatchesMembersAndOwner(t *testing.T) {
	userService := &enrichmentUserServiceStub{users: map[string]userv2.UniversalUser{
		"owner": {
			ID:           "owner",
			Email:        "owner@example.com",
			Type:         "USER",
			PersonalInfo: &userv2.PersonalInfo{FirstName: "Owner", LastName: "Person"},
		},
	}}
	service := &Service{UserService: userService}
	members := []group.Member{
		{ID: "owner", Type: group.MemberTypeUser, Role: group.MemberRoleOwner},
		{ID: "deleted-user", Type: group.MemberTypeUser, Role: group.MemberRoleMember},
		{ID: "child-group", Type: group.MemberTypeGroup, Role: group.MemberRoleMember},
	}

	enriched, owner := service.enrichGroupDetailUsers(context.Background(), members, "owner")

	if len(userService.getUsersCalls) != 1 || fmt.Sprint(userService.getUsersCalls[0]) != fmt.Sprint([]string{"deleted-user", "owner"}) {
		t.Fatalf("GetUsers calls = %v, want one member-and-owner batch", userService.getUsersCalls)
	}
	if len(userService.getByIDCalls) != 0 {
		t.Fatalf("GetUserByID calls = %v, want none", userService.getByIDCalls)
	}
	if len(enriched) != 3 || enriched[0].FullName != "Owner Person" {
		t.Fatalf("enriched members = %+v", enriched)
	}
	if enriched[1].ID != "deleted-user" || enriched[1].FullName != "" || enriched[1].Type != group.MemberTypeUser {
		t.Fatalf("deleted-user fallback = %+v, want group membership stub", enriched[1])
	}
	if owner == nil || owner.Owner == nil || owner.Owner.ID != "owner" || owner.Owner.FullName != "Owner Person" {
		t.Fatalf("owner enrichment = %+v", owner)
	}
}

func TestEnrichNotificationAddressesBatchesDistinctOwners(t *testing.T) {
	userService := &enrichmentUserServiceStub{users: map[string]userv2.UniversalUser{
		"user-a": {ID: "user-a", Email: "a@example.com"},
	}}
	service := &Service{UserService: userService}
	summaries := []notifier.NotificationAddressSummary{
		{ID: "address-1", UserID: "user-a"},
		{ID: "address-2", UserID: "user-a"},
		{ID: "address-3", UserID: "deleted-user"},
	}

	got := service.enrichNotificationAddresses(context.Background(), summaries, true)

	if len(userService.getUsersCalls) != 1 || fmt.Sprint(userService.getUsersCalls[0]) != fmt.Sprint([]string{"deleted-user", "user-a"}) {
		t.Fatalf("GetUsers calls = %v, want one sorted deduplicated batch", userService.getUsersCalls)
	}
	if len(userService.getByIDCalls) != 0 {
		t.Fatalf("GetUserByID calls = %v, want none", userService.getByIDCalls)
	}
	if len(got) != 3 || got[0].User == nil || got[1].User == nil || got[2].User != nil {
		t.Fatalf("notification address enrichment = %+v", got)
	}
}
