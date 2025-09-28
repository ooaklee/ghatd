package user

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.uber.org/zap"
)

// AuditService expected methods of a valid audit service
type AuditService interface {
	LogAuditEvent(ctx context.Context, r *audit.LogAuditEventRequest) error
}

// UserRespository expected methods of a valid user repository
type UserRespository interface {
	CreateUser(ctx context.Context, user *User) (*User, error)
	GetSampleUser(ctx context.Context) ([]User, error)
	GetUsers(ctx context.Context, req *GetUsersRequest) ([]User, error)
	GetUserByID(ctx context.Context, id string) (*User, error)
	GetUserByNanoId(ctx context.Context, nanoId string) (*User, error)
	UpdateUser(ctx context.Context, user *User) (*User, error)
	DeleteUserByID(ctx context.Context, id string) error
	GetUserByEmail(ctx context.Context, email string, logError bool) (*User, error)
	GetTotalUsers(ctx context.Context, firstNameFilter, lastNameFilter, emailFilter, status string, onlyAdmin bool) (int64, error)
}

// Service holds and manages user business logic
type Service struct {
	UserRespository            UserRespository
	AuditService               AuditService
	autoAdminEmailAddressRegex string
}

// NewService created user service
func NewService(userRespository UserRespository, auditService AuditService, autoAdminEmailAddressRegex string) *Service {
	return &Service{
		UserRespository:            userRespository,
		AuditService:               auditService,
		autoAdminEmailAddressRegex: autoAdminEmailAddressRegex,
	}
}

// GetMicroProfile returns user with matching ID's micro profile
func (s *Service) GetMicroProfile(ctx context.Context, r *GetMicroProfileRequest) (*GetMicroProfileResponse, error) {

	userResponse, err := s.GetUserByID(ctx, &GetUserByIdRequest{
		Id: r.Id,
	})
	if err != nil {
		return nil, err
	}

	return &GetMicroProfileResponse{
		MicroProfile: *userResponse.User.GetAsMicroProfile(),
	}, nil
}

// GetProfile returns user with matching ID's profile
func (s *Service) GetProfile(ctx context.Context, r *GetProfileRequest) (*GetProfileResponse, error) {

	userResponse, err := s.GetUserByID(ctx, &GetUserByIdRequest{
		Id: r.Id,
	})
	if err != nil {
		return nil, err
	}

	return &GetProfileResponse{
		Profile: *userResponse.User.GetAsProfile(),
	}, nil
}

// GetUserByEmail returns an user if it matches email
func (s *Service) GetUserByEmail(ctx context.Context, r *GetUserByEmailRequest) (*GetUserByEmailResponse, error) {

	user, err := s.UserRespository.GetUserByEmail(ctx, normaliseUserEmail(r.Email), true)
	if err != nil {
		return nil, err
	}

	return &GetUserByEmailResponse{
		User: *user,
	}, nil
}

// DeleteUser attempts to delete the user with matching ID in repository
func (s *Service) DeleteUser(ctx context.Context, r *DeleteUserRequest) error {

	log := logger.AcquireFrom(ctx)

	userToDelete, err := s.UserRespository.GetUserByID(ctx, r.Id)
	if err != nil {
		return err
	}

	err = s.UserRespository.DeleteUserByID(ctx, r.Id)

	// audit log user delete
	if err == nil {
		auditEvent := audit.UserAccountDelete
		auditErr := s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			ActorId:    audit.AuditActorIdSystem,
			Action:     auditEvent,
			TargetId:   r.Id,
			TargetType: audit.User,
			Domain:     "user",
			Details: &audit.UserAccountDeleteEventDetails{
				UserEmail:     userToDelete.Email,
				UserFirstName: userToDelete.FirstName,
			},
		})

		if auditErr != nil {
			log.Warn("failed-to-log-event", zap.String("actor-id", audit.AuditActorIdSystem), zap.String("user-id", r.Id), zap.String("event-type", string(auditEvent)))
		}
	}

	return err
}

// UpdateUser attempts to update the user with matching ID in repository
func (s *Service) UpdateUser(ctx context.Context, r *UpdateUserRequest) (*UpdateUserResponse, error) {

	var (
		persistentUser *User
		err            error
	)

	// check
	if r.User == nil && r.FirstName == "" && r.LastName == "" || r.User == nil && r.FirstName != "" && len(r.FirstName) == 2 || r.User == nil && r.LastName != "" && len(r.LastName) == 2 {
		return nil, errors.New(ErrKeyInvalidUserBody)
	}

	switch r.User {
	case nil:
		persistentUser, err = s.UserRespository.GetUserByID(ctx, r.Id)
		if err != nil {
			return nil, err
		}

		persistentUser, err = updateUserWithRequest(persistentUser, r)
		if err != nil {
			return &UpdateUserResponse{
				User: *persistentUser,
			}, nil
		}
	default:
		persistentUser = r.User
	}

	updatedUser, err := s.UserRespository.UpdateUser(ctx, persistentUser)
	if err != nil {
		return nil, err
	}

	return &UpdateUserResponse{
		User: *updatedUser,
	}, nil
}

// GetUserByNanoId is returning a user if they have matching nano id
func (s *Service) GetUserByNanoId(ctx context.Context, id string) (*GetUserByIDResponse, error) {

	user, err := s.UserRespository.GetUserByNanoId(ctx, id)
	if err != nil {
		return nil, err
	}

	return &GetUserByIDResponse{
		User: *user,
	}, nil
}

// GetUserByID returns an user if it matches id
func (s *Service) GetUserByID(ctx context.Context, r *GetUserByIdRequest) (*GetUserByIDResponse, error) {

	user, err := s.UserRespository.GetUserByID(ctx, r.Id)
	if err != nil {
		return nil, err
	}

	return &GetUserByIDResponse{
		User: *user,
	}, nil
}

// CreateUser attempts to create user in repository. Return error if any failures occurs
func (s *Service) CreateUser(ctx context.Context, r *CreateUserRequest) (*CreateUserResponse, error) {

	var isAutoAdminEmail bool
	log := logger.AcquireFrom(ctx)

	user := User{
		FirstName: normaliseUserNames(r.FirstName),
		LastName:  normaliseUserNames(r.LastName),
		Email:     normaliseUserEmail(r.Email),
	}

	// Make sure default role is an empty array
	user.Roles = []string{}

	// Add logic to allow specified emails to auto admin
	if s.autoAdminEmailAddressRegex != "" {
		boasiEmailRegex := regexp.MustCompile(s.autoAdminEmailAddressRegex)
		isAutoAdminEmail = boasiEmailRegex.Match([]byte(user.Email))
		if isAutoAdminEmail {
			log.Info("assigning-admin-role-to-user-role", zap.String("team-member-email", user.Email))
			user.Roles = append(user.Roles, UserRoleAdmin)
		}
	}

	persistentUser, err := s.UserRespository.CreateUser(ctx, &user)
	if err != nil {
		return nil, err
	}

	if s.autoAdminEmailAddressRegex != "" && isAutoAdminEmail {
		log.Warn("user-created-with-admin-role", zap.String("team-member-email", user.Email), zap.String("user-id", user.ID))
	}

	return &CreateUserResponse{
		User: *persistentUser,
	}, nil

}

// GetUsers returns the users matching request in repository
func (s *Service) GetUsers(ctx context.Context, r *GetUsersRequest) (*GetUsersResponse, error) {

	var (
		logger *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)
	)

	// default
	if r.Order == "" {
		r.Order = "created_at_desc"
	}

	if r.PerPage == 0 {
		r.PerPage = 25
	}

	if r.Page == 0 {
		r.Page = 1
	}

	// get count of all users
	totalUsers, err := s.UserRespository.GetTotalUsers(ctx, r.FirstName, r.LastName, r.Email, r.Status, r.IsAdmin)
	if err != nil {
		logger.Error("failed-get-users-request--error-getting-total-users", zap.Any("request", r), zap.Error(err))
		return nil, err
	}

	r.TotalCount = int(totalUsers)
	logger.Debug("handling-get-users-request--total-users-found", zap.Int64("total", totalUsers), zap.Any("request", r))

	users, err := s.UserRespository.GetUsers(ctx, r)
	if err != nil {
		logger.Error("failed-get-users-request--error-getting-users", zap.Any("request", r), zap.Error(err))
		return nil, err
	}

	// handle page pagination
	paginatedResponse, err := toolbox.Paginate(ctx, &toolbox.PaginationRequest{PerPage: r.PerPage, Page: r.Page}, users, r.TotalCount)
	if err != nil {
		return nil, err
	}

	return &GetUsersResponse{
		Total:      paginatedResponse.Total,
		TotalPages: paginatedResponse.TotalPages,
		Users:      paginatedResponse.Resources,
		Page:       paginatedResponse.Page,
		PerPage:    paginatedResponse.ResourcePerPage,
	}, nil

}

// normaliseUserEmail returns normalised email string after ensuring
// email is set to lower case
func normaliseUserEmail(email string) string {
	s := toolbox.StringRemoveMultiSpace(strings.TrimSpace(email))
	return strings.ToLower(s)
}

// normaliseUserNames returns normalised name string after ensuring
// it is trimmed and first name capitalised (title)
func normaliseUserNames(email string) string {
	s := toolbox.StringRemoveMultiSpace(strings.TrimSpace(email))
	return toolbox.StringConvertToTitleCase(s)
}

// updateUserWithRequest updates passed user with valid, data if a difference is detected.
// Otherwise, if request matches an error is returned.
// If possible, try to not go to DB as early as possible
func updateUserWithRequest(user *User, request *UpdateUserRequest) (*User, error) {

	newFirstName := normaliseUserNames(request.FirstName)
	newLastName := normaliseUserNames(request.LastName)

	if user.FirstName == newFirstName && user.LastName == newLastName || user.FirstName == newFirstName && newLastName == "" || user.LastName == newLastName && newFirstName == "" {
		return user, errors.New(ErrKeyNoChangesDetected)
	}

	if newFirstName != "" {
		user.FirstName = newFirstName
	}

	if newLastName != "" {
		user.LastName = newLastName
	}

	return user, nil
}
