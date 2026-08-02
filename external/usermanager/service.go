package usermanager

import (
	"context"
	"errors"
	"strings"

	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/notifier"
	"github.com/ooaklee/ghatd/external/reminder"
	"github.com/ooaklee/ghatd/external/streaker"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/vision"
	"go.uber.org/zap"
)

// UserService expected methods of a valid user service
type UserService interface {
	GetUserMicroProfile(ctx context.Context, r *userv2.GetUserMicroProfileRequest) (*userv2.GetUserMicroProfileResponse, error)
	GetUserProfile(ctx context.Context, r *userv2.GetUserProfileRequest) (*userv2.GetUserProfileResponse, error)
	GetUserByID(ctx context.Context, r *userv2.GetUserByIDRequest) (*userv2.GetUserByIDResponse, error)
	GetUsers(ctx context.Context, r *userv2.GetUsersRequest) (*userv2.GetUsersResponse, error)
	GetUserByEmail(ctx context.Context, r *userv2.GetUserByEmailRequest) (*userv2.GetUserByEmailResponse, error)
	UpdateUser(ctx context.Context, r *userv2.UpdateUserRequest) (*userv2.UpdateUserResponse, error)
	DeleteUser(ctx context.Context, r *userv2.DeleteUserRequest) error
}

// ApiTokenService expected methods of a valid api token service
type ApiTokenService interface {
	DeleteApiTokensByOwnerId(ctx context.Context, ownerId string) error
	GetTotalApiTokens(ctx context.Context, r *apitoken.GetTotalApiTokensRequest) (int64, error)
}

// AuditService expected methods of a valid audit service
type AuditService interface {
	LogAuditEvent(ctx context.Context, r *audit.LogAuditEventRequest) error
	GetTotalAuditLogEvents(ctx context.Context, r *audit.GetTotalAuditLogEventsRequest) (int64, error)
}

// ContacterService expected methods of a valid contacter service
type ContacterService interface {
	CreateComms(ctx context.Context, req *contacter.CreateCommsRequest) (*contacter.CreateCommsResponse, error)
	GetComms(ctx context.Context, req *contacter.GetCommsRequest) (*contacter.GetCommsResponse, error)
	UpdateComms(ctx context.Context, req *contacter.UpdateCommsRequest) (*contacter.UpdateCommsResponse, error)
	GetCommsStats(ctx context.Context, req *contacter.GetCommsStatsRequest) (*contacter.GetCommsStatsResponse, error)
	GetAvailableCommsTypes(ctx context.Context) (*contacter.GetAvailableCommsTypesResponse, error)
}

// GroupService expected methods of a valid group service
type GroupService interface {
	GetGroups(ctx context.Context, r *group.GetGroupsRequest) (*group.GetGroupsResponse, error)
	GetGroupByID(ctx context.Context, r *group.GetGroupByIDRequest) (*group.GetGroupByIDResponse, error)
	GetGroupByNanoID(ctx context.Context, r *group.GetGroupByNanoIDRequest) (*group.GetGroupByNanoIDResponse, error)
	GetGroupMembers(ctx context.Context, r *group.GetGroupMembersRequest) (*group.GetGroupMembersResponse, error)
	AddMember(ctx context.Context, r *group.AddMemberRequest) (*group.AddMemberResponse, error)
	RemoveMember(ctx context.Context, r *group.RemoveMemberRequest) (*group.RemoveMemberResponse, error)
	UpdateMemberRole(ctx context.Context, req *group.UpdateMemberRoleRequest) (*group.UpdateMemberRoleResponse, error)
	UpdateOwner(ctx context.Context, req *group.UpdateOwnerRequest) (*group.UpdateOwnerResponse, error)
	UpdateGroup(ctx context.Context, req *group.UpdateGroupRequest) (*group.UpdateGroupResponse, error)
	CreateGroup(ctx context.Context, req *group.CreateGroupRequest) (*group.CreateGroupResponse, error)
	GetGroupDescendants(ctx context.Context, req *group.GetGroupDescendantsRequest) (*group.GetGroupDescendantsResponse, error)
	GetGroupsByUserID(ctx context.Context, req *group.GetGroupsByUserIDRequest) (*group.GetGroupsByUserIDResponse, error)
	GetGroupsAwaitingAnswerForInvitationsByMemberID(ctx context.Context, req *group.GetGroupsAwaitingAnswerForInvitationsByMemberIDRequest) (*group.GetGroupsAwaitingAnswerForInvitationsByMemberIDResponse, error)
	GetUserGroupAccessMap(ctx context.Context, userID string) (map[string]group.UserGroupAccessSummary, error)
	GetGroupsConfig(ctx context.Context, req *group.GetGroupsConfigRequest) (*group.GetGroupsConfigResponse, error)
	GetGroupLineage(ctx context.Context, req *group.GetGroupLineageRequest) (*group.GetGroupLineageResponse, error)
	DeleteGroup(ctx context.Context, req *group.DeleteGroupRequest) (*group.DeleteGroupResponse, error)
	RemoveUserFromAllGroups(ctx context.Context, req *group.RemoveUserFromAllGroupsRequest) (*group.RemoveUserFromAllGroupsResponse, error)
	ValidateGroupName(ctx context.Context, req *group.ValidateGroupNameRequest) (*group.ValidateGroupNameResponse, error)
	GetLatestNotificationOverviews(ctx context.Context, req *common.GetLatestNotificationOverviewsRequest) (*common.GetLatestNotificationOverviewsResponse, error)
	AcceptInvite(ctx context.Context, req *group.AcceptInviteRequest) (*group.AcceptInviteResponse, error)
	RejectInvite(ctx context.Context, req *group.RejectInviteRequest) (*group.RejectInviteResponse, error)
}

// ReminderService expected methods of a valid reminder service.
type ReminderService interface {
	CreateReminder(ctx context.Context, r *reminder.CreateReminderRequest) (*reminder.CreateReminderResponse, error)
	GetReminderByID(ctx context.Context, r *reminder.GetReminderByIDRequest) (*reminder.GetReminderByIDResponse, error)
	ListReminders(ctx context.Context, r *reminder.ListRemindersRequest) (*reminder.ListRemindersResponse, error)
	GetRemindersForTargetTypeByUserID(ctx context.Context, r *reminder.GetRemindersForTargetTypeByUserIDRequest) (*reminder.ListRemindersResponse, error)
	GetActiveRemindersForTargetTypeByUserID(ctx context.Context, r *reminder.GetActiveRemindersForTargetTypeByUserIDRequest) (*reminder.ListRemindersResponse, error)
	UpdateReminderByID(ctx context.Context, r *reminder.UpdateReminderByIDRequest) (*reminder.UpdateReminderByIDResponse, error)
	DeleteReminderByID(ctx context.Context, r *reminder.DeleteReminderByIDRequest) error
	DisableReminderByID(ctx context.Context, r *reminder.DisableReminderByIDRequest) (*reminder.UpdateReminderByIDResponse, error)
	GetReminderStats(ctx context.Context, r *reminder.GetReminderStatsRequest) (*reminder.GetReminderStatsResponse, error)
	GetDueReminders(ctx context.Context, r *reminder.GetDueRemindersRequest) (*reminder.GetDueRemindersResponse, error)
}

// StreakService expected methods of a valid streaker service.
type StreakService interface {
	RecordStreak(ctx context.Context, r *streaker.RecordStreakRequest) (*streaker.RecordStreakResponse, error)
	GetCurrentCount(ctx context.Context, r *streaker.GetCurrentCountRequest) (*streaker.GetCurrentCountResponse, error)
	GetLongestStreak(ctx context.Context, r *streaker.GetLongestStreakRequest) (*streaker.GetLongestStreakResponse, error)
	GetNumberOfStreaks(ctx context.Context, r *streaker.GetNumberOfStreaksRequest) (*streaker.GetNumberOfStreaksResponse, error)
	ListStreaks(ctx context.Context, r *streaker.ListStreaksRequest) (*streaker.ListStreaksResponse, error)
}

// VisionService exposes raw vision operations for user-facing enrichment.
type VisionService interface {
	CreateVision(ctx context.Context, r *vision.CreateVisionRequest) (*vision.VisionResponse, error)
	GetVisionByNanoID(ctx context.Context, r *vision.GetVisionByNanoIDRequest) (*vision.VisionResponse, error)
	GetVisions(ctx context.Context, r *vision.GetVisionsRequest) (*vision.GetVisionsResponse, error)
	GetVisionConfig(ctx context.Context) (*vision.GetVisionConfigResponse, error)
	UpdateVision(ctx context.Context, r *vision.UpdateVisionRequest) (*vision.VisionResponse, error)
	UpdateVisionStatus(ctx context.Context, r *vision.UpdateVisionStatusRequest) (*vision.VisionResponse, error)
	DeleteVision(ctx context.Context, r *vision.DeleteVisionRequest) (*vision.DeleteVisionResponse, error)
	SetVisionVote(ctx context.Context, r *vision.SetVisionVoteRequest) (*vision.VisionResponse, error)
	RemoveVisionVote(ctx context.Context, r *vision.RemoveVisionVoteRequest) (*vision.VisionResponse, error)
	AddVisionComment(ctx context.Context, r *vision.AddVisionCommentRequest) (*vision.VisionResponse, error)
	SetVisionCommentVote(ctx context.Context, r *vision.SetVisionCommentVoteRequest) (*vision.VisionResponse, error)
	RemoveVisionCommentVote(ctx context.Context, r *vision.RemoveVisionCommentVoteRequest) (*vision.VisionResponse, error)
}

// NotifierService expected methods of a valid notifier service.
type NotifierService interface {
	RegisterAddress(ctx context.Context, r *notifier.RegisterAddressRequest) (*notifier.RegisterAddressResponse, error)
	GetActiveAddressesByUserID(ctx context.Context, r *notifier.GetActiveNotificationAddressesRequest) (*notifier.GetActiveNotificationAddressesResponse, error)
	ListUserAddresses(ctx context.Context, r *notifier.ListNotificationAddressesRequest) (*notifier.ListNotificationAddressesResponse, error)
	ListAddresses(ctx context.Context, r *notifier.ListNotificationAddressesRequest) (*notifier.ListNotificationAddressesResponse, error)
	DeleteAddress(ctx context.Context, r *notifier.DeleteNotificationAddressRequest) error
	GetPreferences(ctx context.Context, r *notifier.GetNotificationPreferencesRequest) (*notifier.GetNotificationPreferencesResponse, error)
	UpdatePreferences(ctx context.Context, r *notifier.UpdateNotificationPreferencesRequest) (*notifier.UpdateNotificationPreferencesResponse, error)
	GetConfig(ctx context.Context, r *notifier.GetNotifierConfigRequest) (*notifier.GetNotifierConfigResponse, error)
	NotifyUser(ctx context.Context, r *notifier.NotifyUserRequest) (*notifier.NotifyUserResponse, error)
	NotifyUsers(ctx context.Context, r *notifier.NotifyUsersRequest) (*notifier.NotifyUsersResponse, error)
}

// Service holds and manages usermanager business logic
type Service struct {
	UserService      UserService
	ApiTokenService  ApiTokenService
	AuditService     AuditService
	ContacterService ContacterService
	GroupService     GroupService
	NotifierService  NotifierService
	ReminderService  ReminderService
	StreakService    StreakService
	VisionService    VisionService
}

// NewServiceRequest holds all expected dependencies for an usermanager service
type NewServiceRequest struct {

	// UserService handles updating user information
	UserService UserService

	// ApiTokenService handles api token actions
	ApiTokenService ApiTokenService

	// AuditService handles audit actions
	AuditService AuditService

	// ContacterService handles comms actions
	ContacterService ContacterService
}

// NewService creates usermanager service
func NewService(r *NewServiceRequest) *Service {
	return &Service{
		UserService:      r.UserService,
		ApiTokenService:  r.ApiTokenService,
		AuditService:     r.AuditService,
		ContacterService: r.ContacterService,
	}
}

// WithGroupService adds group service integration
func (s *Service) WithGroupService(groupSvc GroupService) *Service {
	s.GroupService = groupSvc
	return s
}

// WithReminderService adds reminder service integration.
func (s *Service) WithReminderService(reminderSvc ReminderService) *Service {
	s.ReminderService = reminderSvc
	return s
}

// WithStreakService adds streaker service integration.
func (s *Service) WithStreakService(streakSvc StreakService) *Service {
	s.StreakService = streakSvc
	return s
}

// WithNotifierService adds notifier service integration.
func (s *Service) WithNotifierService(notifierSvc NotifierService) *Service {
	s.NotifierService = notifierSvc
	return s
}

// WithVisionService adds vision feedback and roadmap integration.
func (s *Service) WithVisionService(visionSvc VisionService) *Service {
	s.VisionService = visionSvc
	return s
}

// UpdateUserProfile handles the business logic of updating the requesting user's profile
func (s *Service) UpdateUserProfile(ctx context.Context, r *UpdateUserProfileRequest) (*UpdateUserProfileResponse, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/usermanager", "update-user-profile")
	logger.Debug("handling-update-user-profile-request")

	serviceResponse, err := s.UserService.UpdateUser(ctx, &userv2.UpdateUserRequest{
		ID:        r.UserId,
		FirstName: r.FirstName,
		LastName:  r.LastName,
	})
	if err != nil {
		return nil, err
	}

	return &UpdateUserProfileResponse{
		UpdateUserResponse: serviceResponse,
	}, nil
}

// GetUserMicroProfile handles the business logic of fetching the requesting user's micro profile
func (s *Service) GetUserMicroProfile(ctx context.Context, r *GetUserMicroProfileRequest) (*GetUserMicroProfileResponse, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/usermanager", "get-user-micro-profile")
	logger.Debug("handling-get-user-micro-profile-request")

	serviceResponse, err := s.UserService.GetUserMicroProfile(ctx, &userv2.GetUserMicroProfileRequest{
		ID: r.UserId,
	})
	if err != nil {
		return nil, err
	}

	return &GetUserMicroProfileResponse{
		GetUserMicroProfileResponse: serviceResponse,
	}, nil
}

// GetUserByID handles the business logic of fetching a user by ID.
func (s *Service) GetUserByID(ctx context.Context, r *GetUserByIDRequest) (*GetUserByIDResponse, error) {

	logger := logger.AcquirePackageFrom(ctx, "external/usermanager")

	logger.Debug("fetching-user-by-id", zap.String("user-id", r.ID))

	requestedUser, err := s.UserService.GetUserByID(ctx, r.GetUserByIDRequest)
	if err != nil {
		return nil, err
	}

	if requestedUser.User.GetUserId() != r.UserId {
		logger.Warn("user-attempting-to-access-another-user-by-id", zap.String("requesting-user-id", r.UserId), zap.String("requested-user-id", r.ID))

		requestingUser, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{ID: r.UserId})
		if err != nil {
			return nil, err
		}

		if !requestingUser.User.IsAdmin() {
			logger.Warn("non-admin-user-attempting-to-access-another-user-by-id", zap.String("user-id", r.UserId))
			return nil, userv2.ErrUnauthorisedAccess
		}
	}

	return &GetUserByIDResponse{
		User: requestedUser.User,
	}, nil
}

// GetUsers handles the business logic of fetching users.
func (s *Service) GetUsers(ctx context.Context, r *GetUsersRequest) (*GetUsersResponse, error) {

	logger := logger.AcquirePackageFrom(ctx, "external/usermanager")

	logger.Debug("fetching-users", zap.Any("filters", safeLogValue(r.GetUsersRequest)))
	requestingUser, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{ID: r.UserId})
	if err != nil {
		return nil, err
	}

	if !requestingUser.User.IsAdmin() {

		// If the user is not an admin, we must make sure that a group id is provided,
		// and that the user is a member of the group specified in the filter (directly or indirectly through group hierarchy)
		// then we must pull all of the members from the root group of the specified group, get their user ids and pass it to the
		// r.GetUsersRequest.IDsFilter to ensure that the user can only see users that are in the same group (or sub-group) as them.
		// If these conditions are not met, we return an empty list to avoid unauthorised access, and log the attempt.
		if r.GroupID == "" {
			logger.Warn("non-admin-user-attempting-to-access-users-without-group-filter", zap.String("user-id", r.UserId))
			return &GetUsersResponse{
				GetUsersResponse: &userv2.GetUsersResponse{
					Users: []userv2.UniversalUser{},
				},
			}, nil
		}

		userGroupAccessMap, err := s.GroupService.GetUserGroupAccessMap(ctx, r.UserId)
		if err != nil {
			return nil, err
		}

		userGroupAccess, ok := userGroupAccessMap[r.GroupID]
		if !ok || !userGroupAccess.IsAccessible {
			logger.Warn("non-admin-user-attempting-to-access-users-for-a-group-they-do-not-have-access-to", zap.String("user-id", r.UserId), zap.String("group-id", r.GroupID))
			return &GetUsersResponse{
				GetUsersResponse: &userv2.GetUsersResponse{
					Users: []userv2.UniversalUser{},
				},
			}, nil
		}

		// User is not an admin but has access to the group specified in the filter, we will fetch the group details to get the root group id,
		// then we will fetch all of the descendant groups of the root group to get all of the group ids that should be included in the filter, and add them to the IDsFilter of the request
		groupDetails, err := s.GroupService.GetGroupByID(ctx, &group.GetGroupByIDRequest{ID: r.GroupID})
		if err != nil {
			return nil, err
		}

		var rootGroupID string
		if len(groupDetails.Group.Lineage) > 0 {
			rootGroupID = groupDetails.Group.Lineage[0]
		} else {
			rootGroupID = groupDetails.Group.ID
		}

		rootGroup, err := s.GroupService.GetGroupByID(ctx, &group.GetGroupByIDRequest{ID: rootGroupID})
		if err != nil {
			return nil, err
		}

		r.GetUsersRequest.IDsFilter = rootGroup.Group.GetUserMemberIDs()

	}

	serviceResponse, err := s.UserService.GetUsers(ctx, r.GetUsersRequest)
	if err != nil {
		return nil, err
	}

	return &GetUsersResponse{
		GetUsersResponse: serviceResponse,
	}, nil
}

// GetUserProfile handles the business logic of fetching the requesting user's profile
func (s *Service) GetUserProfile(ctx context.Context, r *GetUserProfileRequest) (*GetUserProfileResponse, error) {

	logger := logger.AcquirePackageFrom(ctx, "external/usermanager")

	logger.Debug("fetching-user-profile", zap.String("user-id", r.UserId))

	serviceResponse, err := s.UserService.GetUserProfile(ctx, &userv2.GetUserProfileRequest{
		ID: r.UserId,
	})
	if err != nil {
		return nil, err
	}

	if serviceResponse.Profile.ID != r.UserId {
		logger.Warn("user-attempting-to-access-another-user-profile", zap.String("requesting-user-id", r.UserId), zap.String("requested-user-id", r.ID))

		requestingUser, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{ID: r.UserId})
		if err != nil {
			return nil, err
		}

		if !requestingUser.User.IsAdmin() {
			logger.Warn("non-admin-user-attempting-to-access-another-user-profile", zap.String("user-id", r.UserId))
			return nil, userv2.ErrUnauthorisedAccess
		}
	}

	return &GetUserProfileResponse{
		GetUserProfileResponse: serviceResponse,
	}, nil
}

// DeleteUserPermanently handles the business logic of deleting user and all of their resource on the platform
// TODO: Add audit logs, add more resource types
func (s *Service) DeleteUserPermanently(ctx context.Context, r *DeleteUserPermanentlyRequest) error {

	var logger = logger.AcquirePackageFrom(ctx, "external/usermanager")
	var err error
	targetUserID := strings.TrimSpace(r.ID)
	if targetUserID == "" {
		logger.Warn("delete-user-permanently-request-with-empty-user-id", zap.String("requesting-user-id", r.UserId))
		return errors.New(userv2.ErrInvalidUserID.Error())
	}

	logger.Warn("wiping-user-and-resources-from-platform-started", zap.String("user-id", targetUserID))

	requestedUser, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{ID: targetUserID})
	if err != nil {
		return err
	}

	requestingUserEmail := ""

	if requestedUser.User.GetUserId() != r.UserId {
		logger.Warn("user-attempting-to-delete-another-user", zap.String("requesting-user-id", r.UserId), zap.String("requested-user-id", targetUserID))

		requestingUser, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{ID: r.UserId})
		if err != nil {
			return err
		}

		if !requestingUser.User.IsAdmin() {
			logger.Warn("non-admin-user-attempting-to-delete-another-user", zap.String("user-id", r.UserId))
			return userv2.ErrUnauthorisedAccess
		}

		requestingUserEmail = strings.TrimSpace(requestingUser.User.GetUserEmail())
	}

	if s.AuditService != nil {
		reason := strings.TrimSpace(r.Reason)
		targetUserEmail := strings.TrimSpace(requestedUser.User.GetUserEmail())
		requestedBySelf := strings.TrimSpace(r.UserId) == targetUserID
		if requestingUserEmail == "" && requestedBySelf {
			requestingUserEmail = targetUserEmail
		}

		auditDetails := map[string]interface{}{
			"requesting_user_id": r.UserId,
			"target_user_id":     targetUserID,
			"requested_by_self":  requestedBySelf,
		}
		if targetUserEmail != "" {
			auditDetails["target_user_email"] = targetUserEmail
		}
		if requestingUserEmail != "" {
			auditDetails["requesting_user_email"] = requestingUserEmail
		}
		if reason != "" {
			auditDetails["reason"] = reason
		}

		auditErr := s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			ActorId:    r.UserId,
			Action:     "user.account.delete.requested",
			TargetId:   targetUserID,
			TargetType: audit.TargetTypeUser,
			Domain:     "usermanager",
			Details:    auditDetails,
		})
		if auditErr != nil {
			logger.Warn(
				"failed-to-log-delete-user-permanently-audit-event",
				zap.Error(auditErr),
				zap.String("requesting-user-id", r.UserId),
				zap.String("requested-user-id", targetUserID),
			)
		}
	}

	if s.GroupService != nil {
		logger.Info("initiate-wiping-user-membership-from-root-groups", zap.String("user-id", targetUserID))

		removeResp, removeErr := s.GroupService.RemoveUserFromAllGroups(ctx, &group.RemoveUserFromAllGroupsRequest{UserID: targetUserID})
		if removeErr != nil {
			return removeErr
		}

		affectedRootGroups := 0
		if removeResp != nil {
			affectedRootGroups = removeResp.TotalRootGroupsAffected
		}

		logger.Info(
			"completed-wiping-user-membership-from-root-groups",
			zap.String("user-id", targetUserID),
			zap.Int("root-groups-count", affectedRootGroups),
		)
	}

	logger.Info("initiate-wiping-user-account", zap.String("user-id", targetUserID))
	err = s.UserService.DeleteUser(ctx, &userv2.DeleteUserRequest{ID: targetUserID})
	if err != nil {
		return err
	}
	logger.Info("completed-wiping-user-account", zap.String("user-id", targetUserID))

	logger.Info("initiate-wiping-user-owned-api-tokens", zap.String("user-id", targetUserID))
	err = s.ApiTokenService.DeleteApiTokensByOwnerId(ctx, targetUserID)
	if err != nil {
		return err
	}
	logger.Info("completed-wiping-user-owned-api-tokens", zap.String("user-id", targetUserID))

	logger.Info("wiping-user-and-resources-from-platform-completed", zap.String("user-id", targetUserID))

	return nil
}

// CreateComms handles the logic of creating a comms
func (s *Service) CreateComms(ctx context.Context, req *CreateCommsRequest) (*CreateCommsResponse, error) {

	var (
		logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/usermanager")
	)

	logger.Info("initiating-create-comms-request", zap.Any("request", safeLogValue(req)))

	createdCommsResponse, err := s.ContacterService.CreateComms(ctx, req.CreateCommsRequest)
	if err != nil {
		logger.Error("failed-to-create-comms-error-creating-comms", zap.Any("request", safeLogValue(req)), zap.Error(err))
		return &CreateCommsResponse{}, err
	}

	return &CreateCommsResponse{
		Comms: createdCommsResponse.Comms,
	}, nil
}

// GetAvailableCommsTypes returns the contact categories configured by the
// underlying contacter service.
func (s *Service) GetAvailableCommsTypes(ctx context.Context) (*GetAvailableCommsTypesResponse, error) {
	response, err := s.ContacterService.GetAvailableCommsTypes(ctx)
	if err != nil {
		return &GetAvailableCommsTypesResponse{}, err
	}

	return &GetAvailableCommsTypesResponse{
		CommsTypes: response.CommsTypes,
	}, nil
}

// GetComms handles the logic of getting a comms
func (s *Service) GetComms(ctx context.Context, req *GetCommsRequest) (*GetCommsResponse, error) {

	var (
		logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/usermanager")

		response = GetCommsResponse{
			Comms: []contacter.Comms{},
		}
	)

	logger.Info("initiating-get-comms-request", zap.Any("request", safeLogValue(req)))

	commsResponse, err := s.ContacterService.GetComms(ctx, req.GetCommsRequest)
	if err != nil {
		logger.Error("failed-to-get-comms-error-getting-comms", zap.Any("request", safeLogValue(req)), zap.Error(err))
		return &GetCommsResponse{}, err
	}

	response.Comms = commsResponse.Comms
	response.Meta = commsResponse.GetMetaData()

	return &response, nil
}

// UpdateComms handles the logic of updating a comms
func (s *Service) UpdateComms(ctx context.Context, req *UpdateCommsRequest) (*UpdateCommsResponse, error) {

	var (
		logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/usermanager")
	)

	logger.Info("initiating-update-comms-request", zap.Any("request", safeLogValue(req)))

	updateCommsResponse, err := s.ContacterService.UpdateComms(ctx, req.UpdateCommsRequest)
	if err != nil {
		logger.Error("failed-to-update-comms-error-updating-comms", zap.Any("request", safeLogValue(req)), zap.Error(err))
		return &UpdateCommsResponse{}, err
	}

	return &UpdateCommsResponse{
		Comms: updateCommsResponse.Comms,
	}, nil
}

// GetCommsStats handles the logic of getting comms stats
func (s *Service) GetCommsStats(ctx context.Context, req *GetCommsStatsRequest) (*GetCommsStatsResponse, error) {

	var (
		logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/usermanager")
	)

	logger.Info("initiating-get-comms-stats-request", zap.Any("request", safeLogValue(req)))

	statsResponse, err := s.ContacterService.GetCommsStats(ctx, req.GetCommsStatsRequest)
	if err != nil {
		logger.Error("failed-to-get-comms-stats-error-getting-comms-stats", zap.Any("request", safeLogValue(req)), zap.Error(err))
		return &GetCommsStatsResponse{}, err
	}

	return &GetCommsStatsResponse{
		Stats: statsResponse.CommsStats,
	}, nil
}
