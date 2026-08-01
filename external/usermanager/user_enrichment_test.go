package usermanager

import (
	"context"
	"errors"
	"fmt"
	"testing"

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

func TestNormaliseUserEnrichmentIDsDeduplicatesTrimsAndSorts(t *testing.T) {
	got := normaliseUserEnrichmentIDs([]string{" user-b ", "", "user-a", "user-b"})
	want := []string{"user-a", "user-b"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("normaliseUserEnrichmentIDs() = %v, want %v", got, want)
	}
}

func TestLoadUsersForEnrichmentBatchesAndRetainsPartialSuccess(t *testing.T) {
	users := make(map[string]userv2.UniversalUser)
	ids := make([]string, 0, userEnrichmentBatchSize*2+1)
	for i := 0; i < userEnrichmentBatchSize*2+1; i++ {
		userID := fmt.Sprintf("user-%03d", i)
		ids = append(ids, userID)
		users[userID] = userv2.UniversalUser{ID: userID}
	}
	userService := &enrichmentUserServiceStub{
		users:     users,
		failCalls: map[int]error{1: errors.New("database unavailable")},
	}
	service := &Service{UserService: userService}
	ctx, logs := observedEnrichmentContext()

	got := service.loadUsersForEnrichment(ctx, ids, "test-user-enrichment")

	if len(userService.getUsersCalls) != 3 {
		t.Fatalf("GetUsers calls = %d, want 3", len(userService.getUsersCalls))
	}
	if len(userService.getUsersCalls[0]) != 100 || len(userService.getUsersCalls[1]) != 100 || len(userService.getUsersCalls[2]) != 1 {
		t.Fatalf("GetUsers batch sizes = [%d %d %d], want [100 100 1]", len(userService.getUsersCalls[0]), len(userService.getUsersCalls[1]), len(userService.getUsersCalls[2]))
	}
	if len(got) != 101 {
		t.Fatalf("resolved users = %d, want 101 partial successes", len(got))
	}
	if got["user-000"] == nil || got["user-200"] == nil || got["user-100"] != nil {
		t.Fatalf("unexpected partial result keys: first=%v failed-batch=%v final=%v", got["user-000"], got["user-100"], got["user-200"])
	}
	if logs.FilterMessage("user-enrichment-batch-unavailable").Len() != 1 {
		t.Fatalf("fallback DEBUG events = %d, want 1", logs.FilterMessage("user-enrichment-batch-unavailable").Len())
	}
}

func TestLoadUsersForEnrichmentTreatsMissingUsersAsExpected(t *testing.T) {
	userService := &enrichmentUserServiceStub{
		users: map[string]userv2.UniversalUser{"user-a": {ID: "user-a"}},
	}
	service := &Service{UserService: userService}
	ctx, logs := observedEnrichmentContext()

	got := service.loadUsersForEnrichment(ctx, []string{"user-b", "user-a", "user-b"}, "test-user-enrichment")

	if len(userService.getUsersCalls) != 1 || fmt.Sprint(userService.getUsersCalls[0]) != fmt.Sprint([]string{"user-a", "user-b"}) {
		t.Fatalf("GetUsers calls = %v, want one sorted deduplicated batch", userService.getUsersCalls)
	}
	if len(got) != 1 || got["user-a"] == nil || got["user-b"] != nil {
		t.Fatalf("resolved users = %v, want only user-a", got)
	}
	if logs.Len() != 0 {
		t.Fatalf("missing-user logs = %d, want none", logs.Len())
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
