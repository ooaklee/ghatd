package accessmanager

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"

	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/emailmanager"
	"github.com/ooaklee/ghatd/external/ephemeral"
	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/oauth"
	"github.com/ooaklee/ghatd/external/toolbox"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
)

// AuditService expected methods of a valid audit service
type AuditService interface {
	LogAuditEvent(ctx context.Context, r *audit.LogAuditEventRequest) error
}

// OauthService expected methods of a valid oauth service
type OauthService interface {
	ProviderGetName() string
	ProviderGenerateProtectionToken() string
	ProviderGetCookieKey() string
	ProviderGetUserData(ctx context.Context, requestUriEntries url.Values) (oauth.OauthUserInfo, error)
	ProviderGenerateAuthCodeUrl(protectionToken string) string
	ProviderVerifyRequestIsAuthentic(requestUriEntries url.Values, protectionCookien *http.Cookie) (string, bool)
}

// EphemeralStore expected methods of a valid ephemeral storage
type EphemeralStore interface {
	CreateAuth(ctx context.Context, userID string, tokenDetails ephemeral.TokenDetailsAuth) error
	StoreToken(ctx context.Context, accessTokenUUID string, userID string, ttl time.Duration) error
	// FetchAuth returns the live session owner. Expected absence uses
	// ephemeral.ErrAuthNotFound (legacy redis.Nil is accepted); other errors
	// represent operational failures and must not masquerade as revocation.
	FetchAuth(ctx context.Context, accessDetails ephemeral.TokenDetailsAccess) (string, error)
	DeleteAuth(ctx context.Context, tokenID string) (int64, error)
	// AcquireRefreshTokenRotationLock claims the right to rotate one refresh token.
	AcquireRefreshTokenRotationLock(ctx context.Context, userID, refreshTokenUUID string, ttl time.Duration) (bool, error)
	// ReleaseRefreshTokenRotationLock releases a previously claimed refresh rotation lock.
	ReleaseRefreshTokenRotationLock(ctx context.Context, userID, refreshTokenUUID string) (int64, error)
	// StoreRefreshTokenRotationResult stores a short-lived replay payload for duplicate refreshes.
	StoreRefreshTokenRotationResult(ctx context.Context, userID, refreshTokenUUID string, result *ephemeral.RefreshTokenRotationResult, ttl time.Duration) error
	// GetRefreshTokenRotationResult fetches a short-lived replay payload for duplicate refreshes.
	GetRefreshTokenRotationResult(ctx context.Context, userID, refreshTokenUUID string) (*ephemeral.RefreshTokenRotationResult, error)
	// AcquireLoginEmailCooldown claims the login-email send window for one user/context.
	AcquireLoginEmailCooldown(ctx context.Context, userID string, isDashboardRequest bool, requestURL string, ttl time.Duration) (bool, error)
	// ReleaseLoginEmailCooldown releases a login-email send window after a failed send setup.
	ReleaseLoginEmailCooldown(ctx context.Context, userID string, isDashboardRequest bool, requestURL string) (int64, error)
	AddRequestCountEntry(ctx context.Context, clientIp string) error
	DeleteAllTokenExceptedSpecified(ctx context.Context, userId string, exemptionTokenIds []string) error
	CodeExists(ctx context.Context, code string) (bool, error)
	StoreCode(ctx context.Context, code string, ttl time.Duration) error
	StoreCodeMapping(ctx context.Context, code, token string, ttl time.Duration) error
	GetCodeMapping(ctx context.Context, code string) (string, error)
}

// EmailManager expected methods of a valid email manager
type EmailManager interface {
	SendCustomEmail(ctx context.Context, req *emailmanager.SendCustomEmailRequest) error
	SendLoginEmail(ctx context.Context, req *emailmanager.SendLoginEmailRequest) error
	SendVerificationEmail(ctx context.Context, req *emailmanager.SendVerificationEmailRequest) error
}

// AuthService expected methods of a valid auth service
type AuthService interface {
	CreateInitalToken(ctx context.Context, user auth.UserModel) (*auth.TokenDetails, error)
	CreateToken(ctx context.Context, user auth.UserModel) (*auth.TokenDetails, error)
	ExtractTokenMetadata(ctx context.Context, r *http.Request) (*auth.TokenAccessDetails, error)
	CheckRefreshTokenIsValid(ctx context.Context, t string) (*jwt.Token, error)
	GetRefreshTokenUUID(ctx context.Context, token *jwt.Token) (*auth.TokenRefreshDetails, error)
	CheckAccessTokenValidityGetDetails(ctx context.Context, token *jwt.Token) (*auth.TokenAccessDetails, error)
	ParseAccessTokenFromString(ctx context.Context, tokenAsString string) (*jwt.Token, error)
	CreateEmailVerificationToken(ctx context.Context, user auth.UserModel) (*auth.TokenDetails, error)
	ExtractRefreshTokenMetadataByString(ctx context.Context, tokenAsString string) (*auth.TokenRefreshDetails, error)
	ExtractAccessTokenMetadataByString(ctx context.Context, tokenAsString string) (*auth.TokenAccessDetails, error)
}

// UserService expected methods of a valid user service
type UserService interface {
	GetUserByNanoID(ctx context.Context, r *userv2.GetUserByNanoIDRequest) (*userv2.GetUserByNanoIDResponse, error)
	GetUserByID(ctx context.Context, r *userv2.GetUserByIDRequest) (*userv2.GetUserByIDResponse, error)
	GetUserByEmail(ctx context.Context, r *userv2.GetUserByEmailRequest) (*userv2.GetUserByEmailResponse, error)
	UpdateUser(ctx context.Context, r *userv2.UpdateUserRequest) (*userv2.UpdateUserResponse, error)
	CreateUser(ctx context.Context, r *userv2.CreateUserRequest) (*userv2.CreateUserResponse, error)
}

// userByEmailFinder is an optional capability implemented by user/v2 for
// workflows where a missing email is an expected result. Keeping it separate
// preserves compatibility with existing UserService implementations.
type userByEmailFinder interface {
	FindUserByEmail(ctx context.Context, r *userv2.GetUserByEmailRequest) (*userv2.GetUserByEmailResponse, error)
}

// ApitokenService expected methods of a valid apitoken service
type ApitokenService interface {
	ExtractValidateUserAPITokenMetadata(ctx context.Context, r *http.Request) (*apitoken.APITokenRequester, error)
	UpdateAPITokenLastUsedAt(ctx context.Context, r *apitoken.UpdateAPITokenLastUsedAtRequest) error
	CreateAPIToken(ctx context.Context, r *apitoken.CreateAPITokenRequest) (*apitoken.CreateAPITokenResponse, error)
	DeleteAPIToken(ctx context.Context, r *apitoken.DeleteAPITokenRequest) error
	RevokeAPIToken(ctx context.Context, r *apitoken.RevokeAPITokenRequest) error
	ActivateAPIToken(ctx context.Context, r *apitoken.ActivateAPITokenRequest) error
	GetAPITokensFor(ctx context.Context, r *apitoken.GetAPITokensForRequest) (*apitoken.GetAPITokensForResponse, error)
}

// BillingService expected methods of a valid billing service
type BillingService interface {
	GetUnassociatedSubscriptions(ctx context.Context, req *billing.GetUnassociatedSubscriptionsRequest) (*billing.GetUnassociatedSubscriptionsResponse, error)
	AssociateSubscriptionsWithUser(ctx context.Context, req *billing.AssociateSubscriptionsWithUserRequest) (*billing.AssociateSubscriptionsWithUserResponse, error)
	AssociateBillingEventsWithUser(ctx context.Context, req *billing.AssociateBillingEventsWithUserRequest) (*billing.AssociateBillingEventsWithUserResponse, error)
	GetUnassociatedBillingEvents(ctx context.Context, req *billing.GetUnassociatedBillingEventsRequest) (*billing.GetUnassociatedBillingEventsResponse, error)
}

// GroupService expected methods of a valid group service
type GroupService interface {
	GetParentGroupsWithAutoJoinForEmail(ctx context.Context, email string) (*group.GetParentGroupsWithAutoJoinForEmailResponse, error)
	AddMember(ctx context.Context, req *group.AddMemberRequest) (*group.AddMemberResponse, error)
	InviteUser(ctx context.Context, req *group.InviteUserRequest) (*group.InviteUserResponse, error)
}

// Service holds and manages accessmanager service business logic
type Service struct {
	// tokenPolicy is configured once at startup; nil retains legacy role limits
	// during explicit migration. A configured policy never falls back to roles.
	tokenPolicy           TokenCreationPolicy
	oauthConnections      *OAuthConnectionsConfig
	EphemeralStore        EphemeralStore
	AuditService          AuditService
	EmailManager          EmailManager
	BillingService        BillingService
	GroupService          GroupService
	AuthService           AuthService
	UserService           UserService
	ApitokenService       ApitokenService
	OauthServices         []OauthService
	StaticPlaceholderUuid string
}

const (
	// refreshTokenRotationReplayTTL bounds how long a consumed refresh token can replay the winning rotation result.
	refreshTokenRotationReplayTTL = 30 * time.Second
	// refreshTokenRotationLockTTL bounds how long one process may hold the refresh rotation lock.
	refreshTokenRotationLockTTL = 5 * time.Second
	// refreshTokenRotationWaitTimeout bounds how long duplicate refreshes wait for the winning rotation result.
	refreshTokenRotationWaitTimeout = 2 * time.Second
	// refreshTokenRotationWaitInterval controls how often duplicate refreshes poll for the winning rotation result.
	refreshTokenRotationWaitInterval = 25 * time.Millisecond
	// loginEmailCooldownTTL bounds duplicate login-initiation email sends for the same user/context.
	loginEmailCooldownTTL = 60 * time.Second
)

// NewServiceRequest holds all expected dependencies for an accessmanager service
type NewServiceRequest struct {
	// TokenPolicy supplies live transactional inventory limits. Nil preserves
	// legacy non-atomic admission until the host explicitly migrates its grants.
	TokenPolicy TokenCreationPolicy
	// EphemeralStore handles storing tokens in cache
	EphemeralStore EphemeralStore

	// EmailManager handles sending out emails to users
	EmailManager EmailManager

	// AuthService handles creating authentication tokens
	AuthService AuthService

	// UserService handles creating and updating user login, verification etc. information
	UserService UserService

	// ApiTokenService handles creating and updating api tokens
	ApiTokenService ApitokenService

	// OauthService handles managing oauth integration with providers
	OauthServices []OauthService

	// AuditService handles logging platform events
	AuditService AuditService

	// StaticPlaceholderUuid hold a static uuid that will be used for
	StaticPlaceholderUuid string
}

// NewService creates accessmanager service
func NewService(r *NewServiceRequest) *Service {

	return &Service{
		tokenPolicy:           r.TokenPolicy,
		EphemeralStore:        r.EphemeralStore,
		EmailManager:          r.EmailManager,
		AuthService:           r.AuthService,
		UserService:           r.UserService,
		ApitokenService:       r.ApiTokenService,
		OauthServices:         r.OauthServices,
		AuditService:          r.AuditService,
		StaticPlaceholderUuid: r.StaticPlaceholderUuid,
	}
}

// WithBillingService sets the billing service dependency and returns the updated service
func (s *Service) WithBillingService(billingService BillingService) *Service {
	s.BillingService = billingService
	return s
}

// WithGroupService sets the group service dependency and returns the updated service
func (s *Service) WithGroupService(groupService GroupService) *Service {
	s.GroupService = groupService
	return s
}

// LogoutUserOthers handles logic of managing the user's other log in session
func (s *Service) LogoutUserOthers(ctx context.Context, r *LogoutUserOthersRequest) error {
	logger := logger.AcquireOperationFrom(ctx, "external/accessmanager", "logout-user-others")
	logger.Debug("handling-logout-user-others-request")

	var accessTokenId string
	var refreshTokenId string

	// Check if ID returns valid user
	requestingUser, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{
		ID: r.UserId,
	})
	if err != nil {
		return err
	}

	// parse auth token to get token id
	accessToken, err := s.AuthService.ExtractAccessTokenMetadataByString(ctx, r.AuthToken)
	if err != nil {
		return err
	}

	accessTokenId = accessToken.AccessUUID

	// parse refresh token to get token id
	refreshToken, err := s.AuthService.ExtractRefreshTokenMetadataByString(ctx, r.RefreshToken)
	if err != nil {
		return err
	}
	refreshTokenId = refreshToken.RefreshUUID

	// use user id to call ephemerals store's delete method to remove all tokens except current ones
	return s.EphemeralStore.DeleteAllTokenExceptedSpecified(ctx, requestingUser.User.ID, []string{
		toolbox.CombinedUuidFormat(requestingUser.User.ID, accessTokenId), toolbox.CombinedUuidFormat(requestingUser.User.ID, refreshTokenId)})
}

// GetSpecificUserAPITokens authorizes a live self-service owner, snapshots query
// filters, and returns secret-free rows. List/count pagination is observational,
// not an atomic inventory guarantee and never authority to issue another token.
func (s *Service) GetSpecificUserAPITokens(ctx context.Context, r *GetSpecificUserAPITokensRequest) (*GetSpecificUserAPITokensResponse, error) {
	if s == nil || ctx == nil || nilAccessDependency(s.ApitokenService) {
		return nil, apitoken.ErrServiceUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.GetAPITokensForRequest == nil || strings.TrimSpace(r.UserID) == "" {
		return nil, ErrBadRequest
	}
	input, filters := *r, *r.GetAPITokensForRequest
	if err := tokenManagementSession(ctx, input.ActorID, input.UserID); err != nil {
		return nil, err
	}
	if _, err := s.tokenManagementOwner(ctx, input.ActorID); err != nil {
		return nil, err
	}
	userApiTokenResponse, err := s.ApitokenService.GetAPITokensFor(ctx, &apitoken.GetAPITokensForRequest{
		ID:            input.UserID,
		Order:         filters.Order,
		PerPage:       filters.PerPage,
		Page:          filters.Page,
		Description:   filters.Description,
		Status:        filters.Status,
		Meta:          filters.Meta,
		OnlyEphemeral: filters.OnlyEphemeral,
		OnlyPermanent: filters.OnlyPermanent,
	})
	if err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return tokenListResponse(userApiTokenResponse, input.UserID)
}

// GetUserAPITokenThreshold authorizes a live self-service owner and returns
// display limits. Configured policy never falls back to legacy roles; these values
// are an observation, not permission to create a credential later.
func (s *Service) GetUserAPITokenThreshold(ctx context.Context, r *GetUserAPITokenThresholdRequest) (*GetUserAPITokenThresholdResponse, error) {
	if s == nil || ctx == nil {
		return nil, ErrTokenPolicyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || strings.TrimSpace(r.UserID) == "" {
		return nil, ErrBadRequest
	}
	input := *r
	if err := tokenManagementSession(ctx, input.ActorID, input.UserID); err != nil {
		return nil, err
	}
	persistentUser, err := s.tokenManagementOwner(ctx, input.ActorID)
	if err != nil {
		return nil, err
	}
	if s.tokenPolicy != nil {
		return s.policyTokenThreshold(ctx, input.UserID)
	}

	// Pull user role so we can get their limits
	userHighestRankingRole := common.GetUsersHighestRankedRole(persistentUser.Roles)
	getUserRoleThresholdAllocation := common.UserRolesThresholds.RolesDetails[common.UserRole(userHighestRankingRole)]

	return &GetUserAPITokenThresholdResponse{
		PermanentUserTokenLimit:     getUserRoleThresholdAllocation.LongLivedUserTokenLimit,
		EphemeralUserTokenLimit:     getUserRoleThresholdAllocation.ShortLivedUserTokenLimit,
		EphemeralMinimumAllowedTime: getUserRoleThresholdAllocation.ShortLivedMinimumAllowedTime,
		EphemeralMaximumAllowedTime: getUserRoleThresholdAllocation.ShortLivedMaximumAllowedTime,
		EphemeralMinimumIncrements:  getUserRoleThresholdAllocation.ShortLivedMinimumIncrements,
	}, nil

}

// UpdateUserAPITokenStatus changes only the specified owner's credential state.
// Both manager and mapper bind the session caller to the owner; arbitrary status
// values never revoke. Domain failures retain their native error identities.
func (s *Service) UpdateUserAPITokenStatus(ctx context.Context, r *UserAPITokenStatusRequest) error {
	if s == nil || nilAccessDependency(s.ApitokenService) || ctx == nil {
		return apitoken.ErrServiceUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || strings.TrimSpace(r.UserID) == "" || strings.TrimSpace(r.APITokenID) == "" {
		return ErrAPITokenNotAssociatedWithUser
	}
	input := *r
	r = &input
	if err := tokenManagementSession(ctx, r.ActorID, r.UserID); err != nil {
		return err
	}
	if r.Status != apitoken.UserTokenStatusKeyActive && r.Status != apitoken.UserTokenStatusKeyRevoked {
		return apitoken.ErrTokenStatusInvalid
	}
	if _, err := s.tokenManagementOwner(ctx, r.ActorID); err != nil {
		return err
	}
	var err error

	switch r.Status {
	case apitoken.UserTokenStatusKeyActive:
		err = s.ApitokenService.ActivateAPIToken(ctx, &apitoken.ActivateAPITokenRequest{
			UserID: r.UserID,
			ID:     r.APITokenID})
	case apitoken.UserTokenStatusKeyRevoked:
		err = s.ApitokenService.RevokeAPIToken(ctx, &apitoken.RevokeAPITokenRequest{
			UserID: r.UserID,
			ID:     r.APITokenID,
		})
	default:
		return apitoken.ErrTokenStatusInvalid
	}
	if err != nil {
		return err
	}
	return ctx.Err()
}

// DeleteUserAPIToken delegates exact owner-bound deletion without a list scan.
// A verified session and current matching ACTIVE owner are required even for
// trusted in-process callers. Cancellation after a write does not undo deletion.
func (s *Service) DeleteUserAPIToken(ctx context.Context, r *DeleteUserAPITokenRequest) error {
	if s == nil || nilAccessDependency(s.ApitokenService) || ctx == nil {
		return apitoken.ErrServiceUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil || strings.TrimSpace(r.UserID) == "" || strings.TrimSpace(r.APITokenID) == "" {
		return ErrAPITokenNotAssociatedWithUser
	}
	input := *r
	if err := tokenManagementSession(ctx, input.ActorID, input.UserID); err != nil {
		return err
	}
	if _, err := s.tokenManagementOwner(ctx, input.ActorID); err != nil {
		return err
	}
	if err := s.ApitokenService.DeleteAPIToken(ctx, &apitoken.DeleteAPITokenRequest{UserID: input.UserID, APITokenID: input.APITokenID}); err != nil {
		return err
	}
	return ctx.Err()
}

// CreateUserAPIToken issues a credential only for a verified session's live owner.
// Configured policy fences limits and inventory transactionally; nil policy is
// the transitional, non-atomic legacy role path. No secret escapes an observed
// cancellation or failed/uncertain adapter outcome. Neither implies rollback.
func (s *Service) CreateUserAPIToken(ctx context.Context, r *CreateUserAPITokenRequest) (*CreateUserAPITokenResponse, error) {
	if s == nil || ctx == nil {
		return nil, ErrTokenPolicyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || strings.TrimSpace(r.UserID) == "" {
		return nil, ErrBadRequest
	}
	if r.Ttl < 0 {
		return nil, apitoken.ErrInvalidTokenTTL
	}
	request := *r
	r = &request // Keep owner and requested limits stable across callback retries.
	if err := tokenManagementSession(ctx, r.ActorID, r.UserID); err != nil {
		return nil, err
	}
	if s.tokenPolicy != nil {
		return s.createPolicyToken(ctx, r)
	}
	if nilAccessDependency(s.UserService) || nilAccessDependency(s.ApitokenService) {
		return nil, ErrTokenPolicyUnavailable
	}
	var logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/accessmanager")

	persistentUser, err := s.tokenManagementOwner(ctx, r.ActorID)
	if err != nil {
		return nil, err
	}

	// Pull user role so we can get their limits
	userHighestRankingRole := common.GetUsersHighestRankedRole(persistentUser.Roles)
	getUserRoleThresholdAllocation := common.UserRolesThresholds.RolesDetails[common.UserRole(userHighestRankingRole)]
	if !toolbox.StringInSlice(userHighestRankingRole, persistentUser.Roles) {
		logger.Warn("token-policy-legacy-default-role-selected")
	}

	// get user's apitokens count
	userPermanentTokenCount, userEphemeralTokenCount, err := s.getUserApiTokensCountByType(ctx, persistentUser.ID)
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	if err != nil {
		return nil, err
	}

	// Make sure user doesn't have more than allowed tokens already
	if r.Ttl == 0 && (int64(userPermanentTokenCount) >= getUserRoleThresholdAllocation.LongLivedUserTokenLimit) {
		return nil, ErrPermanentAPITokenLimitReached
	}

	if r.Ttl > 0 && (int64(userEphemeralTokenCount) >= getUserRoleThresholdAllocation.ShortLivedUserTokenLimit) {
		return nil, ErrEphemeralAPITokenLimitReached
	}

	// Legacy lifetime constraints remain explicit during policy migration.
	err = s.verifyRequestIsWithinUserRoleTokenConstraints(ctx, persistentUser.ID, r.Ttl, &getUserRoleThresholdAllocation)
	if err != nil {
		return nil, err
	}

	// Generate token
	apiTokenResponse, err := s.ApitokenService.CreateAPIToken(ctx, &apitoken.CreateAPITokenRequest{
		UserID:      persistentUser.ID,
		UserNanoId:  persistentUser.NanoID,
		TokenTtl:    r.Ttl,
		Description: r.Description,
	})
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	if err != nil {
		return nil, err
	}

	return createdTokenResponse(apiTokenResponse, r.UserID)
}

// verifyRequestIsWithinUserRoleTokenConstraints is taking the token time and making sure it's within the user's
// allocated thresholds
func (s *Service) verifyRequestIsWithinUserRoleTokenConstraints(ctx context.Context, userId string, tokenTtl int64, userRoleThresholds *common.UserRoleThresholds) error {

	var logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/accessmanager")

	if tokenTtl == 0 {
		return nil
	}

	if tokenTtl < userRoleThresholds.ShortLivedMinimumAllowedTime {
		logger.Error("failed-to-create-user-ephemeral-token", zap.String("failure-reason", "ttl-too-short"), zap.String("user-id", userId), zap.Int64("requested-ttl", tokenTtl), zap.Int64("user-role-rank", userRoleThresholds.Ranking))
		return ErrCreateUserAPITokenRequestTtlTooShort
	}

	if tokenTtl > userRoleThresholds.ShortLivedMaximumAllowedTime {
		logger.Error("failed-to-create-user-ephemeral-token", zap.String("failure-reason", "ttl-too-long"), zap.String("user-id", userId), zap.Int64("requested-ttl", tokenTtl), zap.Int64("user-role-rank", userRoleThresholds.Ranking))
		return ErrCreateUserAPITokenRequestTtlTooLong
	}

	if userRoleThresholds.ShortLivedMinimumIncrements <= 0 || tokenTtl%userRoleThresholds.ShortLivedMinimumIncrements != 0 {
		logger.Error("failed-to-create-user-ephemeral-token", zap.String("failure-reason", "ttl-outside-allowed-increment"), zap.String("user-id", userId), zap.Int64("requested-ttl", tokenTtl), zap.Int64("user-role-rank", userRoleThresholds.Ranking))
		return ErrCreateUserAPITokenRequestTtlOutsideAllowedIncrement
	}

	return nil
}

// getUserApiTokensCountByType counts exact stored inventory, not a display page.
// Legacy role admission still needs migration to the transactional policy port.
func (s *Service) getUserApiTokensCountByType(ctx context.Context, userID string) (int64, int64, error) {
	inventory, ok := s.ApitokenService.(tokenInventory)
	if !ok {
		return 0, 0, ErrTokenPolicyUnavailable
	}
	count, err := inventory.CountTokenInventory(ctx, userID)
	if err != nil {
		return 0, 0, err
	}
	if count.Permanent < 0 || count.Ephemeral < 0 {
		return 0, 0, ErrTokenPolicyUnavailable
	}
	return count.Permanent, count.Ephemeral, nil
}

// MiddlewareValidAPITokenRequired validates that the request contains a valid API
// token belonging to an active user. Returns the user ID if valid.
func (s *Service) MiddlewareValidAPITokenRequired(r *http.Request) (*MiddlewareAuthedUserResponse, error) {
	return s.authenticateAPIToken(r, false)
}

// MiddlewareAdminAPITokenRequired binds the credential's owner to a current
// active administrator account before publishing verified API identity.
func (s *Service) MiddlewareAdminAPITokenRequired(r *http.Request) (*MiddlewareAuthedUserResponse, error) {
	return s.authenticateAPIToken(r, true)
}

// authenticateAPIToken binds the credential's stored owner to the live account
// resolved by its public prefix before publishing identity or updating usage.
// A custom verifier must supply IsValid, TokenID and UserID, not just a prefix.
func (s *Service) authenticateAPIToken(r *http.Request, requireAdmin bool) (*MiddlewareAuthedUserResponse, error) {
	if s == nil || r == nil || s.ApitokenService == nil || s.UserService == nil {
		return nil, apitoken.ErrServiceUnavailable
	}
	ctx := r.Context()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	tokenRequester, err := s.ApitokenService.ExtractValidateUserAPITokenMetadata(ctx, r)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tokenRequester == nil || !tokenRequester.IsValid || tokenRequester.TokenID == "" || tokenRequester.UserID == "" || tokenRequester.NanoId == "" {
		return nil, auth.ErrUnauthorized
	}

	persistentUserResponse, err := s.UserService.GetUserByNanoID(ctx, &userv2.GetUserByNanoIDRequest{
		NanoID: tokenRequester.NanoId,
	})
	if err != nil {
		return nil, err
	}

	if persistentUserResponse == nil || persistentUserResponse.User == nil || persistentUserResponse.User.GetUserId() != tokenRequester.UserID {
		return nil, auth.ErrUnauthorized
	}
	if requireAdmin && !persistentUserResponse.User.IsAdmin() {
		return nil, ErrUnauthorizedAdminAccessAttempted
	}

	if persistentUserResponse.User.Status != userv2.AccountStatusKeyActive {
		return nil, ErrUnauthorizedNonActiveStatus
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	_ = s.ApitokenService.UpdateAPITokenLastUsedAt(ctx, &apitoken.UpdateAPITokenLastUsedAtRequest{
		TokenID:         tokenRequester.TokenID,
		APITokenEncoded: tokenRequester.UserAPITokenEncoded,
		ClientID:        tokenRequester.UserID,
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return &MiddlewareAuthedUserResponse{
		Authenticated: true,
		UserID:        persistentUserResponse.User.GetUserId(),
		User:          persistentUserResponse.User,
		APIToken:      &apitoken.CredentialDetails{TokenID: tokenRequester.TokenID, UserID: tokenRequester.UserID},
	}, nil
}

// MiddlewareJWTRequired validates that the request contains a valid, non-expired
// JWT token. Returns the user ID if the token is valid and active in the store.
func (s *Service) MiddlewareJWTRequired(r *http.Request) (*MiddlewareAuthedUserResponse, error) {
	if r == nil || s == nil || s.AuthService == nil {
		return nil, ErrSessionVerificationUnavailable
	}
	ctx := r.Context()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	tokenAuth, err := s.AuthService.ExtractTokenMetadata(ctx, r)
	if err != nil {
		return nil, err
	}
	return s.authenticateTokenDetails(ctx, tokenAuth)
}

// AuthenticateSession verifies one explicitly selected bearer without HTTP,
// cookie selection, refresh, or anonymous fallback. It checks signature,
// expiry, live session presence, current user identity and email revision.
// Hosts retain responsibility for account-status, verification, audience and
// resource permissions. Call again before sensitive work or response replay;
// a previously returned result is only a snapshot of current authority.
func (s *Service) AuthenticateSession(ctx context.Context, credential string) (*MiddlewareAuthedUserResponse, error) {
	if ctx == nil || s == nil || s.AuthService == nil {
		return nil, ErrSessionVerificationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if credential == "" {
		return nil, auth.ErrNoBearerHeaderFound
	}
	details, err := s.AuthService.ExtractAccessTokenMetadataByString(ctx, credential)
	if err != nil {
		return nil, err
	}
	return s.authenticateTokenDetails(ctx, details)
}

// authenticateTokenDetails shares live authority checks between the HTTP and
// transport-independent entry points. It never trusts a token's user ID alone.
func (s *Service) authenticateTokenDetails(ctx context.Context, tokenAuth *auth.TokenAccessDetails) (*MiddlewareAuthedUserResponse, error) {
	if ctx == nil || s == nil || s.EphemeralStore == nil || s.UserService == nil {
		return nil, ErrSessionVerificationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !tokenAuth.IsSessionCredential() || tokenAuth.UserID == "" || tokenAuth.AccessUUID == "" {
		return nil, auth.ErrUnauthorized
	}

	storedUserID, err := s.EphemeralStore.FetchAuth(ctx, tokenAuth)
	if err != nil {
		if ephemeral.IsAuthNotFound(err) {
			return nil, ErrUnauthorizedTokenNotFoundInStore
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if storedUserID != tokenAuth.UserID {
		// A successful lookup with an empty or mismatched owner is malformed
		// authority, not evidence of absence that permits a refresh attempt.
		return nil, ErrSessionVerificationUnavailable
	}

	persistentUserResponse, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{ID: tokenAuth.UserID})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if persistentUserResponse == nil || persistentUserResponse.User == nil || persistentUserResponse.User.GetUserId() != tokenAuth.UserID {
		return nil, auth.ErrUnauthorized
	}

	if persistentUserResponse.User.EmailRevision != tokenAuth.EmailRevision {
		return nil, ErrOAuthReauthenticationRequired
	}
	if !auth.MatchesUserType(tokenAuth.UserType, persistentUserResponse.User) {
		return nil, ErrOAuthReauthenticationRequired
	}
	return &MiddlewareAuthedUserResponse{
		Token:         tokenAuth,
		Authenticated: true,
		UserID:        persistentUserResponse.User.GetUserId(),
		User:          persistentUserResponse.User,
	}, nil
}

// MiddlewareActiveJWTRequired verifies the live session and current ACTIVE
// account. A signed authorization flag alone cannot restore a revoked session.
func (s *Service) MiddlewareActiveJWTRequired(r *http.Request) (*MiddlewareAuthedUserResponse, error) {
	current, err := s.MiddlewareJWTRequired(r)
	if err != nil {
		return nil, err
	}
	return requireActiveSession(current)
}

// MiddlewareAdminJWTRequired adds current stored administrator authority to
// live-session and ACTIVE-account checks. Signed IsAdmin/IsAuthorized flags are
// historical metadata, not a substitute for current roles/status. Promotions and
// demotions take effect on the next check without requiring a fresh token.
func (s *Service) MiddlewareAdminJWTRequired(r *http.Request) (*MiddlewareAuthedUserResponse, error) {
	current, err := s.MiddlewareActiveJWTRequired(r)
	if err != nil {
		return nil, err
	}
	if !current.User.IsAdmin() {
		return nil, ErrUnauthorizedAdminAccessAttempted
	}
	return current, nil
}

// MiddlewareRateLimitOrActiveJWTRequired validates authenticated requests via JWT or
// applies rate limiting to unauthenticated requests. Unauthenticated requests are assigned
// a placeholder user ID and tracked by IP address. Returns the user ID or placeholder.
func (s *Service) MiddlewareRateLimitOrActiveJWTRequired(r *http.Request) (*MiddlewareAuthedUserResponse, error) {
	if r == nil || s == nil || s.AuthService == nil || s.EphemeralStore == nil {
		return nil, ErrSessionVerificationUnavailable
	}
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	tokenAuth, err := s.AuthService.ExtractTokenMetadata(r.Context(), r)
	if contextErr := r.Context().Err(); contextErr != nil {
		return nil, contextErr
	}
	if knownSessionCause(err) == auth.ErrNoBearerHeaderFound {
		if ephErr := s.EphemeralStore.AddRequestCountEntry(r.Context(), getValidRequestorIP(r)); ephErr != nil {
			return nil, ephErr
		}
		if err := r.Context().Err(); err != nil {
			return nil, err
		}

		return &MiddlewareAuthedUserResponse{
			Authenticated: false,
			UserID:        s.StaticPlaceholderUuid,
			User: &userv2.UniversalUser{
				ID: s.StaticPlaceholderUuid,
			},
		}, nil
	}

	if err != nil {
		return nil, err
	}

	current, err := s.authenticateTokenDetails(r.Context(), tokenAuth)
	if err != nil {
		return nil, err
	}
	return requireActiveSession(current)
}

// requireActiveSession applies status policy only after the common verifier has
// bound an authenticated session to its current stored owner and identity.
func requireActiveSession(current *MiddlewareAuthedUserResponse) (*MiddlewareAuthedUserResponse, error) {
	if current.User.Status != userv2.AccountStatusKeyActive {
		return nil, ErrUnauthorizedNonActiveStatus
	}
	return current, nil
}

// LogoutUser handles the logic of signing user off of platform. Delete token(s) from ephemeral store
// TODO: Investigate best way to also delete corresponding refresh token
// TODO: Create tests
func (s *Service) LogoutUser(ctx context.Context, r *http.Request) error {
	var logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/accessmanager")

	accessTokenDetails, err := s.AuthService.ExtractTokenMetadata(ctx, r)
	if err != nil {
		return err
	}

	deleted, err := s.DeleteAuth(ctx, toolbox.CombinedUuidFormat(accessTokenDetails.UserID, accessTokenDetails.AccessUUID))
	if err != nil {
		logger.Error("ephemeral-delete-failed-after-successful-access-token-retrival", zap.String("user-id", accessTokenDetails.UserID), zap.Error(err))
		return err
	}

	if deleted == 0 {
		logger.Error("ephemeral-delete-failed-after-successful-access-token-retrival", zap.String("user-id", accessTokenDetails.UserID))
		return ErrUnauthorizedAccessTokenCacheDeletionFailure
	}

	auditEvent := audit.UserLogout
	auditErr := s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
		ActorId:    audit.AuditActorIdSystem,
		Action:     auditEvent,
		TargetId:   accessTokenDetails.UserID,
		TargetType: audit.User,
		Domain:     "accessmanager",
		// TODO: Investifate details on what can we add to make the audit
		// more informative, maybe IP address
	})

	if auditErr != nil {
		logger.Warn("failed-to-log-event", zap.String("actor-id", audit.AuditActorIdSystem), zap.String("user-id", accessTokenDetails.UserID), zap.String("event-type", string(auditEvent)))
	}

	return nil
}

// RefreshToken validates current account identity and rotates a stored refresh
// credential once, tolerating concurrent callers via a bounded replay result.
// Operational failures preserve their causes. A wait timeout does not imply
// credential rejection; an uncertain write may still have consumed the old
// credential. This is not atomic session-family revocation.
func (s *Service) RefreshToken(ctx context.Context, r *RefreshTokenRequest) (*RefreshTokenResponse, error) {
	if ctx == nil || s == nil || s.AuthService == nil || s.UserService == nil || s.EphemeralStore == nil || r == nil {
		return nil, ErrSessionVerificationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.RefreshToken == "" {
		return nil, ErrEmptyRefreshToken
	}
	var logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/accessmanager")

	tokenUser, refreshTokenDetails, err := s.refreshTokenUserFromCookieValue(ctx, r.RefreshToken)
	if err != nil {
		return nil, err
	}

	refreshTokenUuid := refreshTokenDetails.RefreshUUID
	userID := tokenUser.GetUserId()

	if response, err := s.refreshTokenRotationResponse(ctx, userID, refreshTokenUuid); err != nil {
		return nil, err
	} else if response != nil {
		logger.Info("refresh-token-rotation-replayed")
		return response, nil
	}

	lockAcquired, err := s.EphemeralStore.AcquireRefreshTokenRotationLock(ctx, userID, refreshTokenUuid, refreshTokenRotationLockTTL)
	if err != nil {
		logger.Error("refresh-token-rotation-lock-failed")
		return nil, err
	}
	if !lockAcquired {
		response, waitErr := s.waitForRefreshTokenRotationResponse(ctx, userID, refreshTokenUuid)
		if waitErr != nil {
			return nil, waitErr
		}
		if response != nil {
			logger.Info("refresh-token-rotation-replayed-after-wait")
			return response, nil
		}

		logger.Error("refresh-token-rotation-lock-timeout-without-result")
		return nil, ErrRefreshTemporarilyUnavailable
	}
	defer func() {
		if _, releaseErr := s.EphemeralStore.ReleaseRefreshTokenRotationLock(ctx, userID, refreshTokenUuid); releaseErr != nil {
			logger.Warn("refresh-token-rotation-lock-release-failed")
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Delete previous refresh token matching key (<userID>:<tokenUUID>)
	deleted, err := s.EphemeralStore.DeleteAuth(ctx, toolbox.CombinedUuidFormat(userID, refreshTokenUuid))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if deleted == 0 {
		if response, replayErr := s.refreshTokenRotationResponse(ctx, userID, refreshTokenUuid); replayErr != nil {
			return nil, replayErr
		} else if response != nil {
			logger.Info("refresh-token-rotation-replayed-after-delete-miss")
			return response, nil
		}

		// This legacy sentinel means confirmed absence after a successful delete,
		// not an operational deletion failure (which returned its cause above).
		logger.Debug("refresh-token-record-absent")
		return nil, ErrUnauthorizedRefreshTokenCacheDeletionFailure
	}

	logger.Info("refresh-token-successfully-removed")

	// check if access token is present and clean up along with it
	if r.AccessToken != "" {

		logger.Info("access-token-present-in-refresh-token-request")

		err := s.RemoveAccessTokenWithCookieValue(ctx, userID, r.AccessToken)
		if err != nil {
			logger.Warn("access-token-failed-to-delete-after-successful-refresh-token-clean-up")
		} else {
			logger.Info("access-token-deleted-after-successful-refresh-token-clean-up")
		}

	}

	// Create new pair of refresh and access tokens
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	newTokensDetails, err := s.createSessionToken(ctx, tokenUser, refreshTokenDetails.AuthenticationTime)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if newTokensDetails == nil || newTokensDetails.AccessToken == "" || newTokensDetails.RefreshToken == "" || newTokensDetails.AccessUUID == "" || newTokensDetails.RefreshUUID == "" {
		return nil, ErrSessionVerificationUnavailable
	}

	// Save the tokens to ephemeralstore
	err = s.EphemeralStore.CreateAuth(ctx, userID, newTokensDetails)
	if err != nil {
		logger.Error("ephemeral-store-failed-after-successful-refresh-token-regeneration")
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// rotationResult lets duplicate refresh attempts receive the same replacement token pair.
	rotationResult := &ephemeral.RefreshTokenRotationResult{
		AccessToken:           newTokensDetails.AccessToken,
		RefreshToken:          newTokensDetails.RefreshToken,
		AccessTokenExpiresAt:  newTokensDetails.AtExpires,
		RefreshTokenExpiresAt: newTokensDetails.RtExpires,
	}
	if err := s.EphemeralStore.StoreRefreshTokenRotationResult(ctx, userID, refreshTokenUuid, rotationResult, refreshTokenRotationReplayTTL); err != nil {
		logger.Warn("refresh-token-rotation-result-store-failed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return &RefreshTokenResponse{
		AccessToken:           newTokensDetails.AccessToken,
		RefreshToken:          newTokensDetails.RefreshToken,
		AccessTokenExpiresAt:  newTokensDetails.AtExpires,
		RefreshTokenExpiresAt: newTokensDetails.RtExpires,
	}, nil
}

// refreshTokenRotationResponse maps a cached rotation replay payload to the service response.
func (s *Service) refreshTokenRotationResponse(ctx context.Context, userID, refreshTokenUuid string) (*RefreshTokenResponse, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/accessmanager", "refresh-token-rotation-response")
	logger.Debug("handling-refresh-token-rotation-response-request")

	result, err := s.EphemeralStore.GetRefreshTokenRotationResult(ctx, userID, refreshTokenUuid)
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	if err != nil || result == nil {
		return nil, err
	}
	if result.AccessToken == "" || result.RefreshToken == "" {
		return nil, ErrSessionVerificationUnavailable
	}

	return &RefreshTokenResponse{
		AccessToken:           result.AccessToken,
		RefreshToken:          result.RefreshToken,
		AccessTokenExpiresAt:  result.AccessTokenExpiresAt,
		RefreshTokenExpiresAt: result.RefreshTokenExpiresAt,
	}, nil
}

// waitForRefreshTokenRotationResponse polls briefly for the winning refresh rotation result.
func (s *Service) waitForRefreshTokenRotationResponse(ctx context.Context, userID, refreshTokenUuid string) (*RefreshTokenResponse, error) {
	var logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/accessmanager")

	timeout := time.NewTimer(refreshTokenRotationWaitTimeout)
	defer timeout.Stop()

	ticker := time.NewTicker(refreshTokenRotationWaitInterval)
	defer ticker.Stop()

	for {
		response, err := s.refreshTokenRotationResponse(ctx, userID, refreshTokenUuid)
		if response != nil {
			return response, nil
		}
		if err != nil {
			logger.Warn("refresh-token-rotation-result-fetch-failed-during-wait")
			return nil, err
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout.C:
			return nil, ErrRefreshTemporarilyUnavailable
		case <-ticker.C:
		}
	}
}

// RemoveAccessTokenWithCookieValue removes access token with the given cookie value
func (s *Service) RemoveAccessTokenWithCookieValue(ctx context.Context, userId, accessTokenCookieValue string) error {
	if ctx == nil || s == nil || s.AuthService == nil || s.EphemeralStore == nil {
		return ErrSessionVerificationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if userId == "" || accessTokenCookieValue == "" {
		return auth.ErrUnauthorized
	}
	var logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/accessmanager")

	logger.Info("processing-access-token-removal-by-cookie-value")

	accessTokenDetails, err := s.AuthService.ExtractAccessTokenMetadataByString(ctx, accessTokenCookieValue)
	if err != nil {
		logger.Error("failed-to-extract-access-token-details-from-cookie-value")
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if accessTokenDetails == nil || !accessTokenDetails.IsSessionCredential() || accessTokenDetails.AccessUUID == "" || accessTokenDetails.UserID != userId {
		return auth.ErrUnauthorized
	}

	deleted, err := s.EphemeralStore.DeleteAuth(ctx, toolbox.CombinedUuidFormat(userId, accessTokenDetails.AccessUUID))
	if err != nil {
		logger.Warn("access-token-removal-failed")
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if deleted == 0 {
		return ErrUnauthorizedAccessTokenCacheDeletionFailure
	}

	logger.Info("access-token-successfully-removed")

	return nil
}

// refreshTokenUserFromCookieValue validates a refresh cookie and returns its user and token metadata.
func (s *Service) refreshTokenUserFromCookieValue(ctx context.Context, refreshTokenCookieValue string) (auth.UserModel, *auth.TokenRefreshDetails, error) {
	if ctx == nil || s == nil || s.AuthService == nil || s.UserService == nil {
		return nil, nil, ErrSessionVerificationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	var logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/accessmanager")

	logger.Info("processing-refresh-token-by-cookie-value")

	// Check validity
	refreshToken, err := s.AuthService.CheckRefreshTokenIsValid(ctx, refreshTokenCookieValue)
	if err != nil {
		logger.Error("failed-to-check-if-refresh-token-is-valid")
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if refreshToken == nil || !refreshToken.Valid {
		return nil, nil, auth.ErrUnauthorized
	}

	// Get token details
	refreshTokenDetails, err := s.AuthService.GetRefreshTokenUUID(ctx, refreshToken)
	if err != nil {
		logger.Error("failed-to-get-refresh-token-by-its-uuid")
		return nil, nil, err
	}
	if refreshTokenDetails == nil || refreshTokenDetails.RefreshUUID == "" || refreshTokenDetails.UserID == "" {
		return nil, nil, auth.ErrUnauthorized
	}
	if refreshTokenDetails.TokenUse != "" && refreshTokenDetails.TokenUse != auth.TokenUseRefresh {
		return nil, nil, auth.ErrUnauthorized
	}

	// Get user details
	persistentUserResponse, err := s.UserService.GetUserByID(ctx, &userv2.GetUserByIDRequest{
		ID: refreshTokenDetails.UserID})
	if err != nil {
		logger.Error("unable-to-find-user-for-refresh-token-by-its-provided-user-uuid")
		return nil, refreshTokenDetails, err
	}
	if err := ctx.Err(); err != nil {
		return nil, refreshTokenDetails, err
	}

	if persistentUserResponse == nil || persistentUserResponse.User == nil || persistentUserResponse.User.ID != refreshTokenDetails.UserID {
		return nil, refreshTokenDetails, auth.ErrUnauthorized
	}
	if persistentUserResponse.User.EmailRevision != refreshTokenDetails.EmailRevision || !auth.MatchesUserType(refreshTokenDetails.UserType, persistentUserResponse.User) {
		return nil, refreshTokenDetails, ErrOAuthReauthenticationRequired
	}
	return persistentUserResponse.User, refreshTokenDetails, nil
}

// RemoveRefreshTokenWithCookieValue removes refresh token with the given cookie value
// returns the user id of the refresh token and an error if any
func (s *Service) RemoveRefreshTokenWithCookieValue(ctx context.Context, refreshTokenCookieValue string) (auth.UserModel, string, error) {

	var (
		userId           string
		refreshTokenUuid string
		logger           *zap.Logger = logger.AcquirePackageFrom(ctx, "external/accessmanager")
	)

	logger.Info("processing-refresh-token-removal-by-cookie-value")

	tokenUser, refreshTokenDetails, err := s.refreshTokenUserFromCookieValue(ctx, refreshTokenCookieValue)
	if err != nil {
		if refreshTokenDetails != nil {
			refreshTokenUuid = refreshTokenDetails.RefreshUUID
		}
		return nil, refreshTokenUuid, err
	}

	refreshTokenUuid = refreshTokenDetails.RefreshUUID
	userId = tokenUser.GetUserId()

	// Delete previous refresh token matching key (<userID>:<tokenUUID>)
	deleted, err := s.EphemeralStore.DeleteAuth(ctx, toolbox.CombinedUuidFormat(userId, refreshTokenDetails.RefreshUUID))
	if err != nil || deleted == 0 {
		logger.Error("ephemeral-delete-failed-after-successful-refresh-token-validation", zap.String("user-id", userId), zap.Error(err))
		return nil, refreshTokenUuid, ErrUnauthorizedRefreshTokenCacheDeletionFailure
	}

	logger.Info("refresh-token-successfully-removed", zap.String("user-id", userId), zap.String("refresh-token-id", refreshTokenDetails.RefreshUUID))
	return tokenUser, refreshTokenUuid, nil
}

// LoginUser handles an initial login token or code and actions the surrounding
// login flow. A valid email-verification credential also proves ownership of a
// provisioned user's email, so the account is verified and activated before
// session tokens are created.
func (s *Service) LoginUser(ctx context.Context, r *LoginUserRequest) (*LoginUserResponse, error) {
	if r == nil {
		return nil, ErrBadRequest
	}
	if err := s.proofDependencies(ctx); err != nil {
		return nil, err
	}
	var logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/accessmanager")

	token := r.Token
	if r.Code != "" {
		resolvedToken, err := s.resolveTokenFromCode(ctx, r.Code)
		if err != nil {
			return nil, err
		}
		token = resolvedToken
	}

	initiateLoginTokenDetails, err := s.TokenAsStringValidator(ctx, &TokenAsStringValidatorRequest{
		Token: token})
	if err != nil {
		return nil, err
	}
	if purpose := initiateLoginTokenDetails.TokenUse; purpose != auth.TokenUseLogin && purpose != auth.TokenUseEmailVerification {
		return nil, auth.ErrUnauthorized
	}

	persistentUser, err := s.proofAccount(ctx, initiateLoginTokenDetails.UserID, initiateLoginTokenDetails.UserType, initiateLoginTokenDetails.EmailRevision)
	if err != nil {
		return nil, err
	}
	if persistentUser.Status != userv2.AccountStatusKeyActive && persistentUser.Status != userv2.AccountStatusKeyProvisioned {
		return nil, ErrUnauthorizedNonActiveStatus
	}
	// Atomically claim the proof before account writes or minting. A failure
	// after consumption requires a new proof; restoring it would enable replay.
	if err := s.consumeLoginProof(ctx, initiateLoginTokenDetails); err != nil {
		return nil, err
	}

	var tokenDetails *auth.TokenDetails

	switch persistentUser.Status {
	case userv2.AccountStatusKeyProvisioned:
		tokenDetails, err = s.verifyEmailAndCreateSession(ctx, persistentUser)
		if err != nil {
			return nil, err
		}
	case userv2.AccountStatusKeyActive:
		tokenDetails, err = s.createSessionToken(ctx, persistentUser, time.Now())
		if err != nil {
			return nil, err
		}

		// Update the user's login timestamps.
		persistentUser.SetLastLoginAtNow()
		persistentUser.Metadata.LastFreshLoginAt = persistentUser.Metadata.LastLoginAt

		updateUserResponse, updateErr := s.UserService.UpdateUser(ctx, &userv2.UpdateUserRequest{
			User: persistentUser,
		})
		if updateErr != nil {
			logger.Error("system-update-failed-after-successful-login-initiation", zap.String("user-id", persistentUser.ID))
			return nil, updateErr
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if updateUserResponse == nil || updateUserResponse.User == nil || updateUserResponse.User.ID != persistentUser.ID {
			return nil, ErrSessionVerificationUnavailable
		}

		err = s.EphemeralStore.CreateAuth(ctx, updateUserResponse.User.ID, tokenDetails)
		if err != nil {
			logger.Error("ephemeral-store-failed-after-successful-login-initiation", zap.String("user-id", persistentUser.ID))
			return nil, err
		}
	default:
		logger.Warn("login-rejected-for-non-active-user", zap.String("user-id", persistentUser.ID), zap.String("user-status", persistentUser.Status))
		return nil, ErrUnauthorizedNonActiveStatus
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	auditEvent := audit.UserLogin
	var auditErr error
	if s.AuditService != nil {
		auditErr = s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
			ActorId:    audit.AuditActorIdSystem,
			Action:     auditEvent,
			TargetId:   persistentUser.ID,
			TargetType: audit.User,
			Domain:     "accessmanager",
		})
	}

	if auditErr != nil {
		logger.Warn("failed-to-log-event", zap.String("actor-id", audit.AuditActorIdSystem), zap.String("user-id", persistentUser.ID), zap.String("event-type", string(auditEvent)))
	}

	return &LoginUserResponse{
		AccessToken:           tokenDetails.AccessToken,
		RefreshToken:          tokenDetails.RefreshToken,
		AccessTokenExpiresAt:  tokenDetails.AtExpires,
		RefreshTokenExpiresAt: tokenDetails.RtExpires,
	}, nil
}

// DeleteAuth removes token with matching ID metadata from emphemeral storage
// TODO: Create tests
func (s *Service) DeleteAuth(ctx context.Context, tokenID string) (int64, error) {
	logger := logger.AcquireOperationFrom(ctx, "external/accessmanager", "delete-auth")
	logger.Debug("handling-delete-auth-request")

	return s.EphemeralStore.DeleteAuth(ctx, tokenID)
}

// CreateInitalLoginOrVerificationTokenEmail handles sending user specific emails (intial login / verification) dependent on user's
// account status
// TODO: Create tests
// TODO: Add logic to send email when dashboard access is attempted by non
// admin user
func (s *Service) CreateInitalLoginOrVerificationTokenEmail(ctx context.Context, r *CreateInitalLoginOrVerificationTokenEmailRequest) error {

	var logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/accessmanager")

	// This is intentionally a strict lookup: token delivery requires an
	// existing account and a missing user is an actionable failure.
	persistentUserResponse, err := s.UserService.GetUserByEmail(ctx, &userv2.GetUserByEmailRequest{Email: r.Email})
	if err != nil {
		return err
	}

	switch persistentUserResponse.User.Status {
	case userv2.AccountStatusKeyActive:
		_, err = s.CreateInitalLoginToken(ctx, persistentUserResponse.User, r.Dashboard, r.RequestUrl)
		if err != nil {
			return err
		}
	case userv2.AccountStatusKeyProvisioned:
		_, err = s.CreateEmailVerificationToken(ctx, &CreateEmailVerificationTokenRequest{
			User:               persistentUserResponse.User,
			IsDashboardRequest: r.Dashboard,
			RequestUrl:         r.RequestUrl,
		})
		if err != nil {
			return err
		}
	default:
		logger.Error("requested-user-in-unexpected-state", zap.String("user-id", persistentUserResponse.User.ID), zap.String("user-status", persistentUserResponse.User.Status))
		return ErrUserStatusUncaught
	}

	return nil
}

// ValidateEmailVerificationCode handles updating the system to illustrate a successful email verification
// TODO: Create tests
func (s *Service) ValidateEmailVerificationCode(ctx context.Context, r *ValidateEmailVerificationCodeRequest) (*ValidateEmailVerificationCodeResponse, error) {
	if r == nil {
		return nil, ErrBadRequest
	}
	if err := s.proofDependencies(ctx); err != nil {
		return nil, err
	}
	logger := logger.AcquireOperationFrom(ctx, "external/accessmanager", "validate-email-verification-code")
	logger.Debug("handling-validate-email-verification-code-request")

	token := r.Token
	if r.Code != "" {
		resolvedToken, err := s.resolveTokenFromCode(ctx, r.Code)
		if err != nil {
			return nil, err
		}
		token = resolvedToken
	}

	verifiedTokenDetails, err := s.TokenAsStringValidator(ctx, &TokenAsStringValidatorRequest{
		Token: token})
	if err != nil {
		return nil, err
	}
	if verifiedTokenDetails.TokenUse != auth.TokenUseEmailVerification {
		return nil, auth.ErrUnauthorized
	}

	account, err := s.proofAccount(ctx, verifiedTokenDetails.UserID, verifiedTokenDetails.UserType, verifiedTokenDetails.EmailRevision)
	if err != nil {
		return nil, err
	}
	if account.Status != userv2.AccountStatusKeyProvisioned {
		return nil, ErrUserStatusUncaught
	}
	if err := s.consumeLoginProof(ctx, verifiedTokenDetails); err != nil {
		return nil, err
	}
	tokens, err := s.verifyEmailAndCreateSession(ctx, account)
	if err != nil {
		return nil, err
	}

	return &ValidateEmailVerificationCodeResponse{
		AccessToken:           tokens.AccessToken,
		AccessTokenExpiresAt:  tokens.AtExpires,
		RefreshToken:          tokens.RefreshToken,
		RefreshTokenExpiresAt: tokens.RtExpires,
	}, nil

}

// UserEmailVerificationRevisions is a trusted internal command for callers that
// have already verified and consumed an email proof. It is not authentication
// and must not receive an identity from an untrusted request body.
func (s *Service) UserEmailVerificationRevisions(ctx context.Context, r *UserEmailVerificationRevisionsRequest) (accessToken string, accessTokenExpiresAt int64, refreshToken string, refreshTokenExpiresAt int64, err error) {
	if r == nil {
		return "", 0, "", 0, ErrBadRequest
	}
	account, err := s.proofAccount(ctx, r.UserID, r.UserType, r.EmailRevision)
	if err != nil {
		return "", 0, "", 0, err
	}

	tokenDetails, err := s.verifyEmailAndCreateSession(ctx, account)
	if err != nil {
		return "", 0, "", 0, err
	}

	return tokenDetails.AccessToken, tokenDetails.AtExpires, tokenDetails.RefreshToken, tokenDetails.RtExpires, nil
}

// verifyEmailAndCreateSession applies the only implicit account-state
// transition supported by email credentials: PROVISIONED to ACTIVE. Explicitly
// checking the source state prevents old verification credentials from
// reactivating suspended, locked, or deactivated accounts.
func (s *Service) verifyEmailAndCreateSession(ctx context.Context, persistentUser *userv2.UniversalUser) (*auth.TokenDetails, error) {
	if ctx == nil || s == nil || persistentUser == nil {
		return nil, ErrSessionVerificationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	logger := logger.AcquireOperationFrom(ctx, "external/accessmanager", "verify-email-and-create-session")

	if persistentUser.Status != userv2.AccountStatusKeyProvisioned {
		logger.Warn("email-verification-rejected-for-non-provisioned-user", zap.String("user-id", persistentUser.ID), zap.String("user-status", persistentUser.Status))
		return nil, ErrUserStatusUncaught
	}
	if err := s.proofDependencies(ctx); err != nil {
		return nil, err
	}

	// Update the user's verification data, login metadata, and state before
	// creating tokens so the resulting access token is authorised.
	persistentUser.SetLastLoginAtNow()
	persistentUser.Metadata.LastFreshLoginAt = persistentUser.Metadata.LastLoginAt
	persistentUser.VerifyEmail()
	revisionedUser, err := persistentUser.UpdateStatus(userv2.AccountStatusKeyActive)
	if err != nil {
		logger.Error("user-status-update-failed-after-successful-email-verification", zap.String("user-id", persistentUser.ID))
		return nil, err
	}

	updateUserResponse, err := s.UserService.UpdateUser(ctx, &userv2.UpdateUserRequest{
		User: revisionedUser,
	})
	if err != nil {
		logger.Error("system-update-failed-after-successful-email-verification", zap.String("user-id", persistentUser.ID))
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if updateUserResponse == nil || updateUserResponse.User == nil || updateUserResponse.User.ID != persistentUser.ID {
		return nil, ErrSessionVerificationUnavailable
	}

	newTokenDetails, err := s.createSessionToken(ctx, updateUserResponse.User, time.Now())
	if err != nil {
		logger.Error("token-creation-failed-after-successful-email-verification", zap.String("user-id", persistentUser.ID))
		return nil, err
	}

	err = s.EphemeralStore.CreateAuth(ctx, persistentUser.ID, newTokenDetails)
	if err != nil {
		logger.Error("ephemeral-store-failed-after-successful-email-verification", zap.String("user-id", persistentUser.ID))
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return newTokenDetails, nil
}

// TokenAsStringValidator verifies a signed access-family token and its live
// stored owner without consuming it. Callers must check purpose/current account
// and claim one-use proofs before issuing a session. Operational errors remain
// original causes, not credential-absence classifications.
func (s *Service) TokenAsStringValidator(ctx context.Context, r *TokenAsStringValidatorRequest) (*TokenAsStringValidatorResponse, error) {
	if ctx == nil || s == nil || s.AuthService == nil || s.EphemeralStore == nil {
		return nil, ErrSessionVerificationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.Token == "" {
		return nil, ErrBadRequest
	}

	token, err := s.AuthService.ParseAccessTokenFromString(ctx, r.Token)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if token == nil {
		return nil, ErrSessionVerificationUnavailable
	}

	td, err := s.AuthService.CheckAccessTokenValidityGetDetails(ctx, token)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if td == nil || td.UserID == "" || td.AccessUUID == "" {
		return nil, ErrSessionVerificationUnavailable
	}

	// Check to make sure ephemeral token in persistent storage
	owner, err := s.EphemeralStore.FetchAuth(ctx, td)
	if err != nil {
		if ephemeral.IsAuthNotFound(err) {
			return nil, ErrUnauthorizedTokenNotFoundInStore
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if owner != td.UserID {
		return nil, ErrSessionVerificationUnavailable
	}

	return &TokenAsStringValidatorResponse{
		UserType:      td.UserType,
		TokenUse:      td.TokenUse,
		EmailRevision: td.EmailRevision,
		UserID:        td.UserID,
		TokenID:       td.AccessUUID,
	}, nil

}

// CreateUser creates a new user based on the passed request if possible, and sends verification
// email otherwise errors.
// TODO: Create tests
func (s *Service) CreateUser(ctx context.Context, r *CreateUserRequest) (*CreateUserResponse, error) {

	var logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/accessmanager")

	response := &CreateUserResponse{}

	newUser, err := s.UserService.CreateUser(ctx, &userv2.CreateUserRequest{
		FirstName:      r.FirstName,
		LastName:       r.LastName,
		Email:          r.Email,
		GenerateUUID:   true,
		GenerateNanoID: true,
	})
	if err != nil {
		return nil, err
	}

	response.User = newUser.User

	// Audit log user creation
	auditEvent := audit.UserAccountNew
	auditErr := s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
		ActorId:    audit.AuditActorIdSystem,
		Action:     auditEvent,
		TargetId:   response.User.ID,
		TargetType: audit.User,
		Domain:     "accessmanager",
	})
	if auditErr != nil {
		logger.Warn("failed-to-log-event", zap.String("actor-id", audit.AuditActorIdSystem), zap.String("user-id", response.User.ID), zap.String("event-type", string(auditEvent)))
	}

	s.associateNewUser(ctx, newUser.User)

	// handle verification email if not disabled
	if !r.DisableVerificationEmail {
		logger.Info("initiate-new-user-verification-email", zap.String("user-id", newUser.User.ID))
		_, err = s.CreateEmailVerificationToken(ctx, &CreateEmailVerificationTokenRequest{
			User:       response.User,
			RequestUrl: r.RequestUrl,
		})
		if err != nil {
			logger.Error("failed-to-initiate-new-user-verification-email", zap.String("user-id", newUser.User.ID), zap.Error(err))
			return response, err
		}

		logger.Info("successfully-initiated-new-user-verification-email", zap.String("user-id", newUser.User.ID))
	}

	logger.Info("completed-new-user-creation", zap.String("user-id", newUser.User.ID))

	return response, nil
}

// CreateInitalLoginToken creates token used to initiate login flow for user passed
// TODO: Create tests
func (s *Service) CreateInitalLoginToken(ctx context.Context, user *userv2.UniversalUser, isDashboardRequest bool, requestUrl string) (string, error) {

	var logger *zap.Logger = logger.AcquirePackageFrom(ctx, "external/accessmanager")

	// cooldownAcquired is false when a recent accepted login email already exists for this user/context.
	cooldownAcquired, err := s.EphemeralStore.AcquireLoginEmailCooldown(ctx, user.ID, isDashboardRequest, requestUrl, loginEmailCooldownTTL)
	if err != nil {
		logger.Error("unable-to-acquire-login-email-cooldown:", zap.String("user-id", user.ID), zap.Error(err))
		return "", err
	}
	if !cooldownAcquired {
		logger.Info("login-email-cooldown-active", zap.String("user-id", user.ID))
		return "", nil
	}

	// keepCooldown flips to true only after the email send has been accepted by the email manager.
	keepCooldown := false
	defer func() {
		if keepCooldown {
			return
		}

		if _, releaseErr := s.EphemeralStore.ReleaseLoginEmailCooldown(ctx, user.ID, isDashboardRequest, requestUrl); releaseErr != nil {
			logger.Warn("login-email-cooldown-release-failed", zap.String("user-id", user.ID), zap.Error(releaseErr))
		}
	}()

	tokenDetails, err := s.AuthService.CreateInitalToken(ctx, user)
	if err != nil {
		logger.Error("unable-to-generate-initiate-login-email-token:", zap.String("user-id", user.ID))
		return "", err
	}

	err = s.EphemeralStore.StoreToken(ctx, tokenDetails.EphemeralUUID, user.ID, tokenDetails.EtTTL)
	if err != nil {
		logger.Error("unable-to-store-token-in-ephemeral-store:", zap.String("user-id", user.ID))
		return "", err
	}

	loginCode, err := accessmanagerhelpers.GenerateUniqueCode(ctx, s.EphemeralStore, tokenDetails.EtTTL)
	if err != nil {
		logger.Error("unable-to-generate-login-code:", zap.String("user-id", user.ID), zap.Error(err))
		return "", err
	}

	err = s.EphemeralStore.StoreCodeMapping(ctx, loginCode, tokenDetails.EphemeralToken, tokenDetails.EtTTL)
	if err != nil {
		logger.Error("unable-to-store-login-code-mapping:", zap.String("user-id", user.ID), zap.Error(err))
		return "", err
	}

	// Beging email sending process
	err = s.EmailManager.SendLoginEmail(ctx, &emailmanager.SendLoginEmailRequest{
		Email:              user.Email,
		Token:              tokenDetails.EphemeralToken,
		Code:               loginCode,
		IsDashboardRequest: isDashboardRequest,
		RequestUrl:         requestUrl,
		UserId:             user.ID,
	})
	if err != nil {
		logger.Error("unable-to-send-initiate-login-email:", zap.String("user-id", user.ID))
		return "", err
	}

	keepCooldown = true

	return tokenDetails.EphemeralToken, nil
}

// CreateEmailVerificationToken creates and stores a proof for the supplied
// account revision, then asks the mail adapter to deliver it. Earlier steps are
// not rolled back when a later step fails. Native failures remain available to
// the shared manifest; this method never logs raw credentials or diagnostics.
func (s *Service) CreateEmailVerificationToken(ctx context.Context, r *CreateEmailVerificationTokenRequest) (string, error) {
	if ctx == nil || r == nil || r.User == nil || r.User.ID == "" || r.User.Email == "" || r.User.EmailRevision < 0 {
		return "", ErrBadRequest
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s == nil || nilAccessDependency(s.AuthService) || nilAccessDependency(s.EphemeralStore) || nilAccessDependency(s.EmailManager) {
		return "", userv2.ErrEmailChangeUnavailable
	}
	account := *r.User
	if account.PersonalInfo != nil {
		info := *account.PersonalInfo
		account.PersonalInfo = &info
	}
	requestURL, dashboard := r.RequestUrl, r.IsDashboardRequest
	// Keep delivery/ownership scalars separate from the signer-owned model.
	owner, email := account.ID, account.Email
	first, last := "", ""
	if account.PersonalInfo != nil {
		first, last = account.PersonalInfo.FirstName, account.PersonalInfo.LastName
	}
	tokenDetails, err := s.AuthService.CreateEmailVerificationToken(ctx, &account)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if tokenDetails == nil || tokenDetails.EmailVerificationToken == "" || tokenDetails.EmailVerificationUUID == "" || tokenDetails.EvTTL <= 0 {
		return "", userv2.ErrEmailChangeUnavailable
	}
	proof := *tokenDetails
	if err := s.EphemeralStore.StoreToken(ctx, proof.EmailVerificationUUID, owner, proof.EvTTL); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	code, err := accessmanagerhelpers.GenerateUniqueCode(ctx, s.EphemeralStore, proof.EvTTL)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := s.EphemeralStore.StoreCodeMapping(ctx, code, proof.EmailVerificationToken, proof.EvTTL); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	err = s.EmailManager.SendVerificationEmail(ctx, &emailmanager.SendVerificationEmailRequest{FirstName: first, LastName: last, Email: email, Token: proof.EmailVerificationToken, Code: code, IsDashboardRequest: dashboard, RequestUrl: requestURL, UserId: owner})
	if err != nil {
		return "", err
	}
	return proof.EmailVerificationToken, nil
}

// getValidRequestorIP returns the best IP to refernce an requestor by.
// An assumption is made that the request will always be proxied through Cloudflare
func getValidRequestorIP(r *http.Request) string {

	headers := r.Header

	_, ok := headers[common.ClouflareForwardingIPAddressHttpHeader]

	if ok {
		return r.Header.Get(common.ClouflareForwardingIPAddressHttpHeader)
	}

	return r.RemoteAddr
}

// findUserByEmail prefers the optional userByEmailFinder capability when the
// underlying user service implements it, so callers receive expected-absence
// semantics. Otherwise it falls back to the strict GetUserByEmail lookup for
// backward compatibility.
func findUserByEmail(ctx context.Context, userService UserService, req *userv2.GetUserByEmailRequest) (*userv2.GetUserByEmailResponse, error) {
	if finder, ok := userService.(userByEmailFinder); ok {
		return finder.FindUserByEmail(ctx, req)
	}
	return userService.GetUserByEmail(ctx, req)
}

// resolveTokenFromCode looks up the given code in ephemeral storage and returns the
// associated token. It returns an error if the code is not found or has expired.
func (s *Service) resolveTokenFromCode(ctx context.Context, code string) (string, error) {

	logger := logger.AcquirePackageFrom(ctx, "external/accessmanager")

	token, err := s.EphemeralStore.GetCodeMapping(ctx, code)
	if err != nil {
		logger.Warn("code-to-token-mapping-not-found", append(verificationCodeLogFields(code), zap.Error(err))...)
		return "", ErrInvalidVerificationCode
	}

	if token == "" {
		logger.Warn("code-to-token-mapping-returned-empty-token", verificationCodeLogFields(code)...)
		return "", ErrInvalidVerificationCode
	}

	logger.Info("successfully-resolved-code-to-token", verificationCodeLogFields(code)...)

	return token, nil
}

// associateNewUser preserves pre-registration billing and group actions for every signup path.
func (s *Service) associateNewUser(ctx context.Context, newUser *userv2.UniversalUser) {
	logger := logger.AcquirePackageFrom(ctx, "external/accessmanager")
	// handle associating pre-registered subscriptions and billing events if any
	if s.BillingService != nil {
		logger.Info("checking-for-pre-registered-subscriptions", append([]zap.Field{zap.String("user-id", newUser.ID)}, emailLogFields("user-email", newUser.Email)...)...)
		unassociatedSubResp, err := s.BillingService.GetUnassociatedSubscriptions(ctx, &billing.GetUnassociatedSubscriptionsRequest{
			Email: newUser.Email,
			Limit: 50,
		})
		if err != nil {
			logger.Error("failed-to-check-for-pre-registered-subscriptions", zap.String("user-id", newUser.ID), zap.Error(err))
		} else {
			if len(unassociatedSubResp.Subscriptions) > 0 {
				logger.Info("found-pre-registered-subscriptions", zap.String("user-id", newUser.ID), zap.Int("subscription-count", len(unassociatedSubResp.Subscriptions)))
				_, subscriptionAssociationErr := s.BillingService.AssociateSubscriptionsWithUser(ctx, &billing.AssociateSubscriptionsWithUserRequest{
					UserID: newUser.ID,
					Email:  newUser.Email,
				})
				if subscriptionAssociationErr != nil {
					logger.Error("failed-to-associate-pre-registered-subscriptions", zap.String("user-id", newUser.ID), zap.Error(subscriptionAssociationErr))
				} else {
					logger.Info("successfully-associated-pre-registered-subscriptions", zap.String("user-id", newUser.ID), zap.Int("subscription-count", len(unassociatedSubResp.Subscriptions)))
				}
			} else {
				logger.Info("no-pre-registered-subscriptions-found", zap.String("user-id", newUser.ID))
			}
		}

		logger.Info("checking-for-pre-registered-billing-events", append([]zap.Field{zap.String("user-id", newUser.ID)}, emailLogFields("user-email", newUser.Email)...)...)
		unassociatedBillingEventsResp, err := s.BillingService.GetUnassociatedBillingEvents(ctx, &billing.GetUnassociatedBillingEventsRequest{
			Email: newUser.Email,
			Limit: 100,
		})
		if err != nil {
			logger.Error("failed-to-check-for-pre-registered-billing-events", zap.String("user-id", newUser.ID), zap.Error(err))
		} else {
			if len(unassociatedBillingEventsResp.BillingEvents) > 0 {
				logger.Info("found-pre-registered-billing-events", zap.String("user-id", newUser.ID), zap.Int("billing-event-count", len(unassociatedBillingEventsResp.BillingEvents)))
				_, billingEventAssociationErr := s.BillingService.AssociateBillingEventsWithUser(ctx, &billing.AssociateBillingEventsWithUserRequest{
					UserID: newUser.ID,
					Email:  newUser.Email,
				})
				if billingEventAssociationErr != nil {
					logger.Error("failed-to-associate-pre-registered-billing-events", zap.String("user-id", newUser.ID), zap.Error(billingEventAssociationErr))
				} else {
					logger.Info("successfully-associated-pre-registered-billing-events", zap.String("user-id", newUser.ID), zap.Int("billing-event-count", len(unassociatedBillingEventsResp.BillingEvents)))
				}
			} else {
				logger.Info("no-pre-registered-billing-events-found", zap.String("user-id", newUser.ID))
			}
		}
	}

	// handle auto-join/auto-invite group membership if group service is available
	if s.GroupService != nil {
		logger.Info("checking-for-auto-join-groups", append([]zap.Field{zap.String("user-id", newUser.ID)}, emailLogFields("user-email", newUser.Email)...)...)
		autoJoinGroupsResp, err := s.GroupService.GetParentGroupsWithAutoJoinForEmail(ctx, newUser.Email)
		if err != nil {
			logger.Error("failed-to-check-for-auto-join-groups", zap.String("user-id", newUser.ID), zap.Error(err))
		} else {
			if autoJoinGroupsResp != nil && len(autoJoinGroupsResp.Groups) > 0 {
				logger.Info("found-groups-with-auto-join", zap.String("user-id", newUser.ID), zap.Int("group-count", len(autoJoinGroupsResp.Groups)))

				for _, autoJoinGroup := range autoJoinGroupsResp.Groups {
					if autoJoinGroup == nil || autoJoinGroup.Settings == nil {
						continue
					}

					// Determine which action to take based on group settings
					if autoJoinGroup.Settings.AutoJoinByEmailDomainEnabled {
						// Auto-join: add user directly as member
						memberRole := autoJoinGroup.Settings.AutoActionDefaultMemberRole
						if memberRole == "" {
							memberRole = group.MemberRoleMember
						}

						_, memberErr := s.GroupService.AddMember(ctx, &group.AddMemberRequest{
							GroupID:  autoJoinGroup.ID,
							MemberID: newUser.ID,
							Type:     group.MemberTypeUser,
							Role:     memberRole,
						})
						if memberErr != nil {
							logger.Error("failed-to-auto-join-user-to-group", zap.String("user-id", newUser.ID), zap.String("group-id", autoJoinGroup.ID), zap.Error(memberErr))
						} else {
							logger.Info("successfully-auto-joined-user-to-group", zap.String("user-id", newUser.ID), zap.String("group-id", autoJoinGroup.ID))
							// Audit log the auto-join
							s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
								ActorId:    audit.AuditActorIdSystem,
								Action:     "group.member.auto_joined",
								TargetId:   autoJoinGroup.ID,
								TargetType: "group",
								Domain:     "accessmanager",
								Details: map[string]interface{}{
									"user_id":            newUser.ID,
									"user_email":         newUser.Email,
									"auto_action_source": "signup",
								},
							})
						}
					} else if autoJoinGroup.Settings.AutoInviteByEmailDomainEnabled {
						// Auto-invite: create pending invitation for user's email
						memberRole := autoJoinGroup.Settings.AutoActionDefaultMemberRole
						if memberRole == "" {
							memberRole = group.MemberRoleMember
						}

						_, inviteErr := s.GroupService.InviteUser(ctx, &group.InviteUserRequest{
							GroupID:     autoJoinGroup.ID,
							InviteEmail: newUser.Email,
							Role:        memberRole,
							InvitedByID: audit.AuditActorIdSystem,
						})
						if inviteErr != nil {
							logger.Error("failed-to-auto-invite-user-to-group", zap.String("user-id", newUser.ID), zap.String("group-id", autoJoinGroup.ID), zap.Error(inviteErr))
						} else {
							logger.Info("successfully-auto-invited-user-to-group", zap.String("user-id", newUser.ID), zap.String("group-id", autoJoinGroup.ID))
							// Audit log the auto-invite
							s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
								ActorId:    audit.AuditActorIdSystem,
								Action:     "group.member.auto_invited",
								TargetId:   autoJoinGroup.ID,
								TargetType: "group",
								Domain:     "accessmanager",
								Details: map[string]interface{}{
									"user_id":            newUser.ID,
									"user_email":         newUser.Email,
									"auto_action_source": "signup",
								},
							})
						}
					}
				}
			} else {
				logger.Info("no-groups-with-auto-join-found", zap.String("user-id", newUser.ID))
			}
		}
	}

}
