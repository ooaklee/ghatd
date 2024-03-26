package user

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.mongodb.org/mongo-driver/bson"
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
	GetUsers(ctx context.Context, queryFilter bson.D, requestFilter *bson.D) ([]User, error)
	GetUserByID(ctx context.Context, id string) (*User, error)
	GetUserByNanoId(ctx context.Context, nanoId string) (*User, error)
	UpdateUser(ctx context.Context, user *User) (*User, error)
	DeleteUserByID(ctx context.Context, id string) error
	GetUserByEmail(ctx context.Context, email string, logError bool) (*User, error)
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

	userResponse, err := s.GetUserByID(ctx, &GetUserByIDRequest{
		ID: r.ID,
	})
	if err != nil {
		return nil, err
	}

	// Analyse user biling detaiils
	s.analyseUsersBillingAssessmentData(ctx, &userResponse.User)

	return &GetMicroProfileResponse{
		MicroProfile: UserMicroProfile{
			ID:     userResponse.User.ID,
			Roles:  userResponse.User.Roles,
			Status: userResponse.User.Status,
		},
	}, nil
}

// GetProfile returns user with matching ID's profile
func (s *Service) GetProfile(ctx context.Context, r *GetProfileRequest) (*GetProfileResponse, error) {

	userResponse, err := s.GetUserByID(ctx, &GetUserByIDRequest{
		ID: r.ID,
	})
	if err != nil {
		return nil, err
	}

	// Analyse user biling detaiils
	s.analyseUsersBillingAssessmentData(ctx, &userResponse.User)

	return &GetProfileResponse{
		Profile: UserProfile{
			ID:            userResponse.User.ID,
			FirstName:     userResponse.User.FirstName,
			LastName:      userResponse.User.LastName,
			Roles:         userResponse.User.Roles,
			Status:        userResponse.User.Status,
			Email:         userResponse.User.Email,
			EmailVerified: userResponse.User.Verified.EmailVerified,
		},
	}, nil
}

// analyseUsersBillingAssessmentData is checking to see user has billing assigned, if any
// roles assigned are due removals, and action their profile accordingly and update on repository
func (s *Service) analyseUsersBillingAssessmentData(ctx context.Context, user *User) *User {

	var rolesToRemove []string
	log := logger.AcquireFrom(ctx)

	// check if user has billing details assigned
	if !user.IsBillied() {
		return user
	}

	// verify user's billing associations are not assigned, if so note for deletion
	for role, expirationDateAsText := range user.Meta.BillingAssessmentAt {
		expiration, err := time.Parse("2006-01-02T00:00:00", expirationDateAsText)
		if err != nil {
			log.Warn("unable-to-parse-billing-association-expiry-date", zap.String("role-id", role), zap.String("expiry-date", expirationDateAsText), zap.String("user-id", user.ID))
			continue
		}

		timeNow := time.Now()

		if timeNow.After(expiration) || timeNow.Equal(expiration) {
			rolesToRemove = append(rolesToRemove, role)
		}
	}

	// make sure there are roles to remove
	if len(rolesToRemove) < 1 {
		return user
	}

	// remove role from user roles
	for _, role := range rolesToRemove {

		// remove billing meta
		user.ClearBillingAssessmentDate(role)

		// check to see if role to remove exists
		if !toolbox.StringInSlice(role, user.Roles) {
			log.Warn("role-to-remove-not-assigned-to-user", zap.String("role-id", role), zap.String("user-id", user.ID))
			continue
		}

		// remove from user if role is assigned
		user.Roles = toolbox.RemoveStringFromSlice(role, user.Roles)
	}

	// Update user object in the repository, if it fails just log a message and carry on by passing newly structured user object
	_, err := s.UpdateUser(ctx, &UpdateUserRequest{User: user})
	if err != nil {
		log.Error("failed-to-update-user-roles-during-billing-assessment", zap.String("roles-id", strings.Join(rolesToRemove, ", ")), zap.String("user-id", user.ID))
	}

	return user
}

// GetUserByEmail returns an user if it matches email
func (s *Service) GetUserByEmail(ctx context.Context, r *GetUserByEmailRequest) (*GetUserByEmailResponse, error) {
	response := &GetUserByEmailResponse{}

	user, err := s.UserRespository.GetUserByEmail(ctx, normaliseUserEmail(r.Email), true)
	if err != nil {
		return response, err
	}

	response.User = *user

	return response, nil
}

// DeleteUser attempts to delete the user with matching ID in repository
func (s *Service) DeleteUser(ctx context.Context, r *DeleteUserRequest) error {

	log := logger.AcquireFrom(ctx)

	userToDelete, err := s.UserRespository.GetUserByID(ctx, r.ID)
	if err != nil {
		return err
	}

	err = s.UserRespository.DeleteUserByID(ctx, r.ID)

	// audit log user delete
	if err == nil {
		auditEvent := audit.UserAccountDelete
		auditErr := s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			ActorId:    audit.AuditActorIdSystem,
			Action:     auditEvent,
			TargetId:   r.ID,
			TargetType: audit.User,
			Domain:     "user",
			Details: &audit.UserAccountDeleteEventDetails{
				UserEmail:     userToDelete.Email,
				UserFirstName: userToDelete.FirstName,
			},
		})

		if auditErr != nil {
			log.Warn("failed-to-log-event", zap.String("actor-id", audit.AuditActorIdSystem), zap.String("user-id", r.ID), zap.String("event-type", string(auditEvent)))
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

	switch r.User {
	case nil:
		persistentUser, err = s.UserRespository.GetUserByID(ctx, r.ID)
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
	response := &GetUserByIDResponse{}

	user, err := s.UserRespository.GetUserByNanoId(ctx, id)
	if err != nil {
		return response, err
	}

	response.User = *user

	return response, nil
}

// GetUserByID returns an user if it matches id
func (s *Service) GetUserByID(ctx context.Context, r *GetUserByIDRequest) (*GetUserByIDResponse, error) {
	response := &GetUserByIDResponse{}

	user, err := s.UserRespository.GetUserByID(ctx, r.ID)
	if err != nil {
		return response, err
	}

	response.User = *user

	return response, nil
}

// GetUsers attempts to create user in repository. Return error if any failures occurs
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

	if r.Random {
		return s.generateRandomUserResponse(ctx)
	}

	sortFilter := s.generateGetUsersOrderSortFilter(r.Order)

	findQuery := s.generateGetUsersOrderQueryFilter(r)

	users, err := s.UserRespository.GetUsers(ctx, findQuery, sortFilter)
	if err != nil {
		return &GetUsersResponse{}, err
	}

	return s.generateGetUsersResponse(ctx, r, users)

}

// GetUsersPagination is handling making the call to centralised pagination
// logic to paginate on passed API Tokens resources
func (s *Service) GetUsersPagination(ctx context.Context, resource []User, perPage, page int) (*GetUsersPaginationResponse, error) {

	var resourceToInterfaceSlice []interface{}
	castedResources := []User{}
	log := logger.AcquireFrom(ctx)

	// convert resource slice to interface clice
	for _, element := range resource {
		resourceToInterfaceSlice = append(resourceToInterfaceSlice, element)
	}

	// Call pagination logic
	paginatedResource, err := toolbox.GetResourcePagination(ctx, &toolbox.GetResourcePaginationRequest{
		PerPage: perPage,
		Page:    page,
	}, resourceToInterfaceSlice)

	if err != nil {
		return nil, err
	}

	// convert paginated resource slice to correct type
	for _, resource := range paginatedResource.Resources {
		castedResource, ok := resource.(User)
		if !ok {
			log.Error("error-unable-to-cast-paginated-user-resource")
			continue
		}
		castedResources = append(castedResources, castedResource)
	}

	return &GetUsersPaginationResponse{
		Resources:       castedResources,
		Total:           paginatedResource.Total,
		TotalPages:      paginatedResource.TotalPages,
		ResourcePerPage: paginatedResource.ResourcePerPage,
		Page:            paginatedResource.Page,
	}, nil

}

// generateGetUsersResponse returns appropiate response based on client request & users pulled
// from repository
func (s *Service) generateGetUsersResponse(ctx context.Context, r *GetUsersRequest, users []User) (*GetUsersResponse, error) {

	paginatedUsers, err := s.GetUsersPagination(ctx, users, r.PerPage, r.Page)
	if err != nil {
		return &GetUsersResponse{}, err
	}

	return &GetUsersResponse{
		Total:        paginatedUsers.Total,
		TotalPages:   paginatedUsers.TotalPages,
		Users:        paginatedUsers.Resources,
		Page:         paginatedUsers.Page,
		UsersPerPage: paginatedUsers.ResourcePerPage,
	}, nil

}

// generateGetUsersOrderQueryFilter returns filter that describes how users should be
// filtered
func (s *Service) generateGetUsersOrderQueryFilter(r *GetUsersRequest) bson.D {

	findQuery := bson.D{}

	if r.FirstName != "" {
		findQuery = append(findQuery, bson.E{Key: UserRespositoryFieldPathFirstName, Value: bson.M{"$in": []string{normaliseUserNames(r.FirstName)}}})
	}

	if r.LastName != "" {
		findQuery = append(findQuery, bson.E{Key: UserRespositoryFieldPathLastName, Value: normaliseUserNames(r.LastName)})
	}

	if r.Status != "" {
		findQuery = append(findQuery, bson.E{Key: UserRespositoryFieldPathStatus, Value: bson.M{"$in": []string{toolbox.StringStandardisedToUpper(r.Status)}}})
	}

	if r.IsAdmin {
		findQuery = append(findQuery, bson.E{Key: UserRespositoryFieldPathRoles, Value: bson.M{"$in": []string{UserRoleAdmin}}})
	}

	if r.Email != "" {
		findQuery = append(findQuery, bson.E{Key: UserRespositoryFieldPathEmail, Value: normaliseUserEmail(r.Email)})
	}

	return findQuery

}

// generateGetUsersOrderSortFilter returns filter that describes how users should be sorted
// when returned from repository
func (s *Service) generateGetUsersOrderSortFilter(orderBy string) *bson.D {
	sortFilter := bson.D{}

	switch orderBy {
	case GetUserOrderCreatedAtAsc:
		sortFilter = append(sortFilter, bson.E{Key: UserRespositoryFieldPathCreatedAt, Value: 1})
	case GetUserOrderCreatedAtDesc:
		sortFilter = append(sortFilter, bson.E{Key: UserRespositoryFieldPathCreatedAt, Value: -1})

	case GetUserOrderLastLoginAtAsc:
		sortFilter = append(sortFilter, bson.E{Key: UserRespositoryFieldPathLastLoginAt, Value: 1})
	case GetUserOrderLastLoginAtDesc:
		sortFilter = append(sortFilter, bson.E{Key: UserRespositoryFieldPathLastLoginAt, Value: -1})

	case GetUserOrderActivatedAtAsc:
		sortFilter = append(sortFilter, bson.E{Key: UserRespositoryFieldPathActivatedAt, Value: 1})
	case GetUserOrderActivatedAtDesc:
		sortFilter = append(sortFilter, bson.E{Key: UserRespositoryFieldPathActivatedAt, Value: -1})

	case GetUserOrderStatusChangedAtAsc:
		sortFilter = append(sortFilter, bson.E{Key: UserRespositoryFieldPathStatusChangedAt, Value: 1})
	case GetUserOrderStatusChangedAtDesc:
		sortFilter = append(sortFilter, bson.E{Key: UserRespositoryFieldPathStatusChangedAt, Value: -1})

	case GetUserOrderLastFreshLoginAtAsc:
		sortFilter = append(sortFilter, bson.E{Key: UserRespositoryFieldPathLastFreshLoginAt, Value: 1})
	case GetUserOrderLastFreshLoginAtDesc:
		sortFilter = append(sortFilter, bson.E{Key: UserRespositoryFieldPathLastFreshLoginAt, Value: -1})

	case GetUserOrderEmailVerifiedAtAsc:
		sortFilter = append(sortFilter, bson.E{Key: UserRespositoryFieldPathVerifiedAt, Value: 1})
	case GetUserOrderEmailVerifiedAtDesc:
		sortFilter = append(sortFilter, bson.E{Key: UserRespositoryFieldPathVerifiedAt, Value: -1})

	default:
		sortFilter = append(sortFilter, bson.E{Key: UserRespositoryFieldPathCreatedAt, Value: -1})
	}

	return &sortFilter
}

// generateRandomUserResponse returns a random user from repository
func (s *Service) generateRandomUserResponse(ctx context.Context) (*GetUsersResponse, error) {
	users, err := s.UserRespository.GetSampleUser(ctx)
	if err != nil {
		return &GetUsersResponse{}, err
	}

	return &GetUsersResponse{
		Total:        1,
		Users:        users,
		TotalPages:   1,
		Page:         1,
		UsersPerPage: 1,
		Random:       true,
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
	return strings.Title(s)
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
