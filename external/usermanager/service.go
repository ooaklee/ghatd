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
	// GetUserMicroProfile returns the micro profile for the user identified by r.
	// The usermanager service passes the requesting actor's ID as the lookup key
	// and wraps the returned response; errors from the underlying user service are
	// propagated.
	GetUserMicroProfile(ctx context.Context, r *userv2.GetUserMicroProfileRequest) (*userv2.GetUserMicroProfileResponse, error)
	// GetUserProfile returns the full profile for the user identified by r. The
	// usermanager service allows self-access, and returns ErrUnauthorisedAccess
	// when a non-admin actor requests another user's profile; underlying errors are
	// propagated.
	GetUserProfile(ctx context.Context, r *userv2.GetUserProfileRequest) (*userv2.GetUserProfileResponse, error)
	// GetUserByID returns the user record for the requested ID. The usermanager
	// service allows actors to fetch themselves and returns ErrUnauthorisedAccess
	// when a non-admin actor requests another user; underlying errors are
	// propagated.
	GetUserByID(ctx context.Context, r *userv2.GetUserByIDRequest) (*userv2.GetUserByIDResponse, error)
	// GetUsers returns users matching r's filters. The usermanager service
	// restricts non-admin actors to an accessible group filter, scoping results to
	// that group hierarchy's members; admins receive the unfiltered result set.
	GetUsers(ctx context.Context, r *userv2.GetUsersRequest) (*userv2.GetUsersResponse, error)
	// GetUsersByIDs supplies shared lookup mechanics; UMS owns its projections
	// and may retain successful batches only for optional response enrichment.
	GetUsersByIDs(ctx context.Context, r *userv2.GetUsersByIDsRequest) (*userv2.GetUsersByIDsResponse, error)
	// GetUserByEmail returns the user record matching the email address carried in
	// r, or an error if the lookup fails. Part of the UserService port consumed by
	// the usermanager service.
	GetUserByEmail(ctx context.Context, r *userv2.GetUserByEmailRequest) (*userv2.GetUserByEmailResponse, error)
	// UpdateUser applies the changes described in r to the target user and returns
	// the updated user response, or an error if the update fails. Part of the
	// UserService port consumed by the usermanager service.
	UpdateUser(ctx context.Context, r *userv2.UpdateUserRequest) (*userv2.UpdateUserResponse, error)
	// DeleteUser deletes the user identified by r and returns an error if deletion
	// fails. Part of the UserService port consumed by the usermanager service.
	DeleteUser(ctx context.Context, r *userv2.DeleteUserRequest) error
}

// ApiTokenService expected methods of a valid api token service
type ApiTokenService interface {
	// DeleteApiTokensByOwnerId deletes the API tokens owned by ownerId, returning
	// an error if deletion fails. Part of the ApiTokenService port consumed by the
	// usermanager service.
	DeleteApiTokensByOwnerId(ctx context.Context, ownerId string) error
	// GetTotalApiTokens returns the count of API tokens matching the criteria in r,
	// or an error if counting fails. Part of the ApiTokenService port consumed by
	// the usermanager service.
	GetTotalApiTokens(ctx context.Context, r *apitoken.GetTotalApiTokensRequest) (int64, error)
}

// AuditService expected methods of a valid audit service
type AuditService interface {
	// LogAuditEvent records the audit event described in r, returning an error if
	// logging fails. Part of the AuditService port consumed by the usermanager
	// service.
	LogAuditEvent(ctx context.Context, r *audit.LogAuditEventRequest) error
	// GetTotalAuditLogEvents returns the count of audit log events matching the
	// criteria in r, or an error if counting fails. Part of the AuditService port
	// consumed by the usermanager service.
	GetTotalAuditLogEvents(ctx context.Context, r *audit.GetTotalAuditLogEventsRequest) (int64, error)
}

// ContacterService expected methods of a valid contacter service
type ContacterService interface {
	// CreateComms creates a comms resource from req. The usermanager service
	// delegates to the contacter service and returns the created comms; the handler
	// responds with its creation receipt or an error.
	CreateComms(ctx context.Context, req *contacter.CreateCommsRequest) (*contacter.CreateCommsResponse, error)
	// GetComms returns comms records matching req's filters, with optional
	// metadata. The usermanager service delegates to the contacter service and
	// copies the comms list and metadata into the response.
	GetComms(ctx context.Context, req *contacter.GetCommsRequest) (*contacter.GetCommsResponse, error)
	// UpdateComms applies the changes in req to a comms resource. The usermanager
	// service delegates to the contacter service and returns the updated comms,
	// propagating any error.
	UpdateComms(ctx context.Context, req *contacter.UpdateCommsRequest) (*contacter.UpdateCommsResponse, error)
	// GetCommsStats returns aggregate statistics for comms matching req's filters.
	// The usermanager service delegates to the contacter service and maps the
	// returned stats into its response.
	GetCommsStats(ctx context.Context, req *contacter.GetCommsStatsRequest) (*contacter.GetCommsStatsResponse, error)
	// GetAvailableCommsTypes returns the contact categories configured by the
	// contacter service. The usermanager service passes them through; the handler
	// serves this as public configuration discovery without exposing comms records.
	GetAvailableCommsTypes(ctx context.Context) (*contacter.GetAvailableCommsTypesResponse, error)
}

// GroupService expected methods of a valid group service
type GroupService interface {
	// GetGroups returns groups matching the filters in r, or an error if the lookup
	// fails. Part of the GroupService port consumed by the usermanager service.
	GetGroups(ctx context.Context, r *group.GetGroupsRequest) (*group.GetGroupsResponse, error)
	// GetGroupByID returns the group identified by r, or an error if the lookup
	// fails. Part of the GroupService port consumed by the usermanager service.
	GetGroupByID(ctx context.Context, r *group.GetGroupByIDRequest) (*group.GetGroupByIDResponse, error)
	// GetGroupByNanoID returns the group identified by r's nano ID. The usermanager
	// service requires the group service to be enabled and otherwise passes the
	// request through unchanged.
	GetGroupByNanoID(ctx context.Context, r *group.GetGroupByNanoIDRequest) (*group.GetGroupByNanoIDResponse, error)
	// GetGroupMembers returns the members of the group identified by r. The
	// usermanager service requires the group service to be enabled and otherwise
	// passes the request through unchanged.
	GetGroupMembers(ctx context.Context, r *group.GetGroupMembersRequest) (*group.GetGroupMembersResponse, error)
	// AddMember adds a member to a group as described by r and returns the
	// resulting response, or an error if the addition fails. Part of the
	// GroupService port.
	AddMember(ctx context.Context, r *group.AddMemberRequest) (*group.AddMemberResponse, error)
	// RemoveMember removes a member from a group as described by r and returns the
	// resulting response, or an error if the removal fails. Part of the
	// GroupService port.
	RemoveMember(ctx context.Context, r *group.RemoveMemberRequest) (*group.RemoveMemberResponse, error)
	// UpdateMemberRole changes a group member's role as described by req and
	// returns the resulting response, or an error if the update fails. Part of the
	// GroupService port.
	UpdateMemberRole(ctx context.Context, req *group.UpdateMemberRoleRequest) (*group.UpdateMemberRoleResponse, error)
	// UpdateOwner transfers a group's ownership as described by req and returns the
	// resulting response, or an error if the transfer fails. Part of the
	// GroupService port.
	UpdateOwner(ctx context.Context, req *group.UpdateOwnerRequest) (*group.UpdateOwnerResponse, error)
	// UpdateGroup updates an existing group as described by req. The usermanager
	// service permits admins, and non-admins with effective admin-level access to
	// the group, before delegating to the group service.
	UpdateGroup(ctx context.Context, req *group.UpdateGroupRequest) (*group.UpdateGroupResponse, error)
	// CreateGroup creates a new group as described by req. The usermanager service
	// permits admins, and non-admins with admin access to a specified parent group,
	// before delegating creation to the group service.
	CreateGroup(ctx context.Context, req *group.CreateGroupRequest) (*group.CreateGroupResponse, error)
	// GetGroupDescendants returns descendant groups of the group identified in req.
	// The usermanager service gates non-admin requesters on group accessibility,
	// sets the acting user, and delegates to the group service.
	GetGroupDescendants(ctx context.Context, req *group.GetGroupDescendantsRequest) (*group.GetGroupDescendantsResponse, error)
	// GetGroupsByUserID returns groups for the requested user. The usermanager
	// service allows self-service and requires admin status when the actor queries
	// another user, before delegating to the group service.
	GetGroupsByUserID(ctx context.Context, req *group.GetGroupsByUserIDRequest) (*group.GetGroupsByUserIDResponse, error)
	// GetGroupsAwaitingAnswerForInvitationsByMemberID returns groups whose
	// invitations await an answer for the member identified in req, or an error if
	// the lookup fails. Part of the GroupService port.
	GetGroupsAwaitingAnswerForInvitationsByMemberID(ctx context.Context, req *group.GetGroupsAwaitingAnswerForInvitationsByMemberIDRequest) (*group.GetGroupsAwaitingAnswerForInvitationsByMemberIDResponse, error)
	// GetUserGroupAccessMap returns, for the given userID, a map of group IDs to
	// access summaries describing the user's accessibility and admin status per
	// group. Part of the GroupService port.
	GetUserGroupAccessMap(ctx context.Context, userID string) (map[string]group.UserGroupAccessSummary, error)
	// GetGroupsConfig returns the group service's configuration capabilities. The
	// usermanager service requires the group service to be enabled and passes an
	// empty request through to it.
	GetGroupsConfig(ctx context.Context, req *group.GetGroupsConfigRequest) (*group.GetGroupsConfigResponse, error)
	// GetGroupLineage returns the lineage of the group identified in req. The
	// usermanager service gates non-admin requesters on group accessibility, sets
	// the acting user, and delegates to the group service.
	GetGroupLineage(ctx context.Context, req *group.GetGroupLineageRequest) (*group.GetGroupLineageResponse, error)
	// DeleteGroup deletes a group as described by req. The usermanager service
	// allows admins to choose hard or soft delete; non-admin owners are restricted
	// to hard delete, and deletion is attributed to the requester.
	DeleteGroup(ctx context.Context, req *group.DeleteGroupRequest) (*group.DeleteGroupResponse, error)
	// RemoveUserFromAllGroups removes the user identified in req from every group
	// they belong to, returning the resulting response or an error. Part of the
	// GroupService port.
	RemoveUserFromAllGroups(ctx context.Context, req *group.RemoveUserFromAllGroupsRequest) (*group.RemoveUserFromAllGroupsResponse, error)
	// ValidateGroupName checks a proposed group name via the group service. The
	// usermanager service lets admins validate directly; non-admins must have
	// access to the supplied parent group, when one is given.
	ValidateGroupName(ctx context.Context, req *group.ValidateGroupNameRequest) (*group.ValidateGroupNameResponse, error)
	// GetLatestNotificationOverviews returns recent notification overviews. The
	// usermanager service resolves the recipient from the trusted actor for
	// self-service queries; AdminView requires an active admin and may select
	// another account.
	GetLatestNotificationOverviews(ctx context.Context, req *common.GetLatestNotificationOverviewsRequest) (*common.GetLatestNotificationOverviewsResponse, error)
	// AcceptInvite accepts a group invitation as described by req and returns the
	// resulting response, or an error if acceptance fails. Part of the GroupService
	// port.
	AcceptInvite(ctx context.Context, req *group.AcceptInviteRequest) (*group.AcceptInviteResponse, error)
	// RejectInvite rejects a group invitation as described by req and returns the
	// resulting response, or an error if rejection fails. Part of the GroupService
	// port.
	RejectInvite(ctx context.Context, req *group.RejectInviteRequest) (*group.RejectInviteResponse, error)
}

// ReminderService expected methods of a valid reminder service.
type ReminderService interface {
	// CreateReminder creates a reminder as described by r. The usermanager service
	// requires the reminder service to be enabled, then delegates and wraps the
	// created reminder response.
	CreateReminder(ctx context.Context, r *reminder.CreateReminderRequest) (*reminder.CreateReminderResponse, error)
	// GetReminderByID returns the reminder identified by r. The usermanager service
	// validates the requester; admins may fetch any reminder while non-admins are
	// scoped to their own user ID.
	GetReminderByID(ctx context.Context, r *reminder.GetReminderByIDRequest) (*reminder.GetReminderByIDResponse, error)
	// ListReminders returns reminders matching r's filters. The usermanager service
	// validates the requester and scopes results to the authenticated user, or
	// across users when the requester is an admin.
	ListReminders(ctx context.Context, r *reminder.ListRemindersRequest) (*reminder.ListRemindersResponse, error)
	// GetRemindersForTargetTypeByUserID returns reminders for the user and target
	// type described in r, or an error if the lookup fails. Part of the
	// ReminderService port.
	GetRemindersForTargetTypeByUserID(ctx context.Context, r *reminder.GetRemindersForTargetTypeByUserIDRequest) (*reminder.ListRemindersResponse, error)
	// GetActiveRemindersForTargetTypeByUserID returns active reminders for the user
	// and target type described in r, or an error if the lookup fails. Part of the
	// ReminderService port.
	GetActiveRemindersForTargetTypeByUserID(ctx context.Context, r *reminder.GetActiveRemindersForTargetTypeByUserIDRequest) (*reminder.ListRemindersResponse, error)
	// UpdateReminderByID updates the reminder identified in r. The usermanager
	// service validates the requester before delegating the update to the reminder
	// service and wrapping the response.
	UpdateReminderByID(ctx context.Context, r *reminder.UpdateReminderByIDRequest) (*reminder.UpdateReminderByIDResponse, error)
	// DeleteReminderByID deletes the reminder identified in r, returning an error
	// on failure. The usermanager service validates the requester and scopes
	// deletion to the authenticated user's reminders.
	DeleteReminderByID(ctx context.Context, r *reminder.DeleteReminderByIDRequest) error
	// DisableReminderByID disables the reminder identified in r. The usermanager
	// service validates the requester, scopes the operation to the authenticated
	// user, and returns the updated reminder.
	DisableReminderByID(ctx context.Context, r *reminder.DisableReminderByIDRequest) (*reminder.UpdateReminderByIDResponse, error)
	// GetReminderStats returns aggregate reminder statistics. The usermanager
	// service validates the requester and applies admin-based scoping to user
	// filters before delegating to the reminder service.
	GetReminderStats(ctx context.Context, r *reminder.GetReminderStatsRequest) (*reminder.GetReminderStatsResponse, error)
	// GetDueReminders returns reminders ready for scheduler dispatch. The
	// usermanager service validates the requester, applies admin-based scoping to
	// user filters, and forwards the due-before cutoff and limit.
	GetDueReminders(ctx context.Context, r *reminder.GetDueRemindersRequest) (*reminder.GetDueRemindersResponse, error)
}

// StreakService expected methods of a valid streaker service.
type StreakService interface {
	// RecordStreak records a streak via the streaker backend; the usermanager
	// Service binds the authenticated ActorID as owner/creator before delegating
	// and returns the recorded streak response.
	RecordStreak(ctx context.Context, r *streaker.RecordStreakRequest) (*streaker.RecordStreakResponse, error)
	// GetCurrentCount returns the streaker current-count response for the given
	// GetCurrentCountRequest.
	GetCurrentCount(ctx context.Context, r *streaker.GetCurrentCountRequest) (*streaker.GetCurrentCountResponse, error)
	// GetLongestStreak returns the longest streak response matching the request
	// filters; the usermanager Service scopes the owner for the authenticated
	// requester before delegating.
	GetLongestStreak(ctx context.Context, r *streaker.GetLongestStreakRequest) (*streaker.GetLongestStreakResponse, error)
	// GetNumberOfStreaks returns the count of streak entries matching the request
	// filters; the usermanager Service scopes the owner to the authenticated
	// requester before delegating.
	GetNumberOfStreaks(ctx context.Context, r *streaker.GetNumberOfStreaksRequest) (*streaker.GetNumberOfStreaksResponse, error)
	// ListStreaks returns streak entries matching the request; the usermanager
	// Service scopes the owner to the authenticated requester and defaults the
	// period type before delegating.
	ListStreaks(ctx context.Context, r *streaker.ListStreaksRequest) (*streaker.ListStreaksResponse, error)
}

// VisionService exposes raw vision operations for user-facing enrichment.
type VisionService interface {
	// CreateVision stores authenticated feedback or a bug report; the usermanager
	// Service delegates to the vision backend and returns the enriched vision
	// response.
	CreateVision(ctx context.Context, r *vision.CreateVisionRequest) (*vision.VisionResponse, error)
	// GetVisionByNanoID returns the vision addressed by public NanoID; the
	// usermanager Service projects it with public user summaries for privacy-safe
	// viewing.
	GetVisionByNanoID(ctx context.Context, r *vision.GetVisionByNanoIDRequest) (*vision.VisionResponse, error)
	// GetVisions returns a page of vision entries matching the request; the
	// usermanager Service projects each with associated public users and a total.
	GetVisions(ctx context.Context, r *vision.GetVisionsRequest) (*vision.GetVisionsResponse, error)
	// GetVisionConfig returns the client-safe vision capabilities by delegating to
	// the configured vision backend.
	GetVisionConfig(ctx context.Context) (*vision.GetVisionConfigResponse, error)
	// UpdateVision applies descriptive-field edits to a vision; the usermanager
	// Service authorizes the owner or administrator, excludes internal metadata,
	// and returns the enriched result.
	UpdateVision(ctx context.Context, r *vision.UpdateVisionRequest) (*vision.VisionResponse, error)
	// UpdateVisionStatus applies a roadmap status transition; the usermanager
	// Service delegates to the vision backend and returns the enriched vision
	// response.
	UpdateVisionStatus(ctx context.Context, r *vision.UpdateVisionStatusRequest) (*vision.VisionResponse, error)
	// DeleteVision deletes a vision by request; the usermanager Service authorizes
	// the owner or administrator before delegating permanent deletion.
	DeleteVision(ctx context.Context, r *vision.DeleteVisionRequest) (*vision.DeleteVisionResponse, error)
	// SetVisionVote records a vote on a vision; the usermanager Service delegates
	// to the vision backend and returns the enriched vision response.
	SetVisionVote(ctx context.Context, r *vision.SetVisionVoteRequest) (*vision.VisionResponse, error)
	// RemoveVisionVote removes a vote from a vision; the usermanager Service
	// delegates to the vision backend and returns the enriched vision response.
	RemoveVisionVote(ctx context.Context, r *vision.RemoveVisionVoteRequest) (*vision.VisionResponse, error)
	// AddVisionComment appends a comment to a vision; the usermanager Service
	// delegates storage to the vision backend and returns the enriched vision
	// response.
	AddVisionComment(ctx context.Context, r *vision.AddVisionCommentRequest) (*vision.VisionResponse, error)
	// SetVisionCommentVote records a vote on a vision comment; the usermanager
	// Service delegates to the vision backend and returns the enriched vision
	// response.
	SetVisionCommentVote(ctx context.Context, r *vision.SetVisionCommentVoteRequest) (*vision.VisionResponse, error)
	// RemoveVisionCommentVote removes a vote from a vision comment; the usermanager
	// Service delegates to the vision backend and returns the enriched vision
	// response.
	RemoveVisionCommentVote(ctx context.Context, r *vision.RemoveVisionCommentVoteRequest) (*vision.VisionResponse, error)
}

// NotifierService expected methods of a valid notifier service.
type NotifierService interface {
	// RegisterAddress registers a notification address using the supplied notifier
	// RegisterAddressRequest and returns the registration response.
	RegisterAddress(ctx context.Context, r *notifier.RegisterAddressRequest) (*notifier.RegisterAddressResponse, error)
	// GetActiveAddressesByUserID returns the active notification addresses for the
	// user identified by the GetActiveNotificationAddressesRequest.
	GetActiveAddressesByUserID(ctx context.Context, r *notifier.GetActiveNotificationAddressesRequest) (*notifier.GetActiveNotificationAddressesResponse, error)
	// ListUserAddresses returns notification addresses for a user as specified by
	// the ListNotificationAddressesRequest.
	ListUserAddresses(ctx context.Context, r *notifier.ListNotificationAddressesRequest) (*notifier.ListNotificationAddressesResponse, error)
	// ListAddresses returns notification addresses according to the
	// ListNotificationAddressesRequest filters.
	ListAddresses(ctx context.Context, r *notifier.ListNotificationAddressesRequest) (*notifier.ListNotificationAddressesResponse, error)
	// DeleteAddress deletes the notification address targeted by the
	// DeleteNotificationAddressRequest, returning only an error.
	DeleteAddress(ctx context.Context, r *notifier.DeleteNotificationAddressRequest) error
	// GetPreferences returns the notification preferences matching the
	// GetNotificationPreferencesRequest.
	GetPreferences(ctx context.Context, r *notifier.GetNotificationPreferencesRequest) (*notifier.GetNotificationPreferencesResponse, error)
	// UpdatePreferences applies notification preference changes from the
	// UpdateNotificationPreferencesRequest and returns the updated preferences.
	UpdatePreferences(ctx context.Context, r *notifier.UpdateNotificationPreferencesRequest) (*notifier.UpdateNotificationPreferencesResponse, error)
	// GetConfig returns the notifier configuration described by the
	// GetNotifierConfigRequest.
	GetConfig(ctx context.Context, r *notifier.GetNotifierConfigRequest) (*notifier.GetNotifierConfigResponse, error)
	// NotifyUser sends a notification to a target user's active addresses; the
	// usermanager Service delegates to the notifier backend and may return results
	// alongside an error.
	NotifyUser(ctx context.Context, r *notifier.NotifyUserRequest) (*notifier.NotifyUserResponse, error)
	// NotifyUsers dispatches notifications to multiple users; the usermanager
	// Service delegates to the notifier backend, which honours per-user preferences
	// and optional channel filters.
	NotifyUsers(ctx context.Context, r *notifier.NotifyUsersRequest) (*notifier.NotifyUsersResponse, error)
}

// Service holds and manages usermanager business logic
type Service struct {
	// administratorAuthorizer rechecks live session authority for administrative writes.
	administratorAuthorizer AdministratorAuthorizer
	UserService             UserService
	ApiTokenService         ApiTokenService
	AuditService            AuditService
	ContacterService        ContacterService
	GroupService            GroupService
	NotifierService         NotifierService
	ReminderService         ReminderService
	StreakService           StreakService
	VisionService           VisionService
	// CommsVotingService delegates contact membership and shared vote mechanics.
	CommsVotingService CommsVotingService
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

// GetUserMicroProfile handles the business logic of fetching the requesting user's micro profile
func (s *Service) GetUserMicroProfile(ctx context.Context, r *GetUserMicroProfileRequest) (*GetUserMicroProfileResponse, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/usermanager", "get-user-micro-profile")
	logger.Debug("handling-get-user-micro-profile-request")

	serviceResponse, err := s.UserService.GetUserMicroProfile(ctx, &userv2.GetUserMicroProfileRequest{
		ID: r.ActorID,
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

	if requestedUser.User.GetUserId() != r.ActorID {
		logger.Warn("user-attempting-to-access-another-user-by-id", zap.String("requesting-user-id", r.ActorID), zap.String("requested-user-id", r.ID))

		requestingUser, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{ID: r.ActorID})
		if err != nil {
			return nil, err
		}

		if !requestingUser.User.IsAdmin() {
			logger.Warn("non-admin-user-attempting-to-access-another-user-by-id", zap.String("user-id", r.ActorID))
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
	requestingUser, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{ID: r.ActorID})
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
			logger.Warn("non-admin-user-attempting-to-access-users-without-group-filter", zap.String("user-id", r.ActorID))
			return &GetUsersResponse{
				GetUsersResponse: &userv2.GetUsersResponse{
					Users: []userv2.UniversalUser{},
				},
			}, nil
		}

		userGroupAccessMap, err := s.GroupService.GetUserGroupAccessMap(ctx, r.ActorID)
		if err != nil {
			return nil, err
		}

		userGroupAccess, ok := userGroupAccessMap[r.GroupID]
		if !ok || !userGroupAccess.IsAccessible {
			logger.Warn("non-admin-user-attempting-to-access-users-for-a-group-they-do-not-have-access-to", zap.String("user-id", r.ActorID), zap.String("group-id", r.GroupID))
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

	logger.Debug("fetching-user-profile", zap.String("user-id", r.ActorID))

	serviceResponse, err := s.UserService.GetUserProfile(ctx, &userv2.GetUserProfileRequest{
		ID: r.ActorID,
	})
	if err != nil {
		return nil, err
	}

	if serviceResponse.Profile.ID != r.ActorID {
		logger.Warn("user-attempting-to-access-another-user-profile", zap.String("requesting-user-id", r.ActorID), zap.String("requested-user-id", r.ID))

		requestingUser, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{ID: r.ActorID})
		if err != nil {
			return nil, err
		}

		if !requestingUser.User.IsAdmin() {
			logger.Warn("non-admin-user-attempting-to-access-another-user-profile", zap.String("user-id", r.ActorID))
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
		logger.Warn("delete-user-permanently-request-with-empty-user-id", zap.String("requesting-user-id", r.ActorID))
		return errors.New(userv2.ErrInvalidUserID.Error())
	}

	logger.Warn("wiping-user-and-resources-from-platform-started", zap.String("user-id", targetUserID))

	requestedUser, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{ID: targetUserID})
	if err != nil {
		return err
	}

	requestingUserEmail := ""

	if requestedUser.User.GetUserId() != r.ActorID {
		logger.Warn("user-attempting-to-delete-another-user", zap.String("requesting-user-id", r.ActorID), zap.String("requested-user-id", targetUserID))

		requestingUser, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{ID: r.ActorID})
		if err != nil {
			return err
		}

		if !requestingUser.User.IsAdmin() {
			logger.Warn("non-admin-user-attempting-to-delete-another-user", zap.String("user-id", r.ActorID))
			return userv2.ErrUnauthorisedAccess
		}

		requestingUserEmail = strings.TrimSpace(requestingUser.User.GetUserEmail())
	}

	if s.AuditService != nil {
		reason := strings.TrimSpace(r.Reason)
		targetUserEmail := strings.TrimSpace(requestedUser.User.GetUserEmail())
		requestedBySelf := strings.TrimSpace(r.ActorID) == targetUserID
		if requestingUserEmail == "" && requestedBySelf {
			requestingUserEmail = targetUserEmail
		}

		auditDetails := map[string]interface{}{
			"requesting_user_id": r.ActorID,
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
			ActorId:    r.ActorID,
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
				zap.String("requesting-user-id", r.ActorID),
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
