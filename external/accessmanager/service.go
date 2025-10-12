package accessmanager

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/emailmanager"
	"github.com/ooaklee/ghatd/external/ephemeral"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/oauth"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ooaklee/ghatd/external/user"
	"go.uber.org/zap"
)

// User expected methods of a valid user
type User interface {
	GetAttributeByJsonPath(jsonPath string) (any, error)
	IsAdmin() bool
	UpdateStatus(desiredStatus string) (*user.User, error)
}

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
	FetchAuth(ctx context.Context, accessDetails ephemeral.TokenDetailsAccess) (string, error)
	DeleteAuth(ctx context.Context, tokenID string) (int64, error)
	AddRequestCountEntry(ctx context.Context, clientIp string) error
	DeleteAllTokenExceptedSpecified(ctx context.Context, userId string, exemptionTokenIds []string) error
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
	GetUserByNanoId(ctx context.Context, id string) (*user.GetUserByIDResponse, error)
	GetUserByID(ctx context.Context, r *user.GetUserByIdRequest) (*user.GetUserByIDResponse, error)
	GetUserByEmail(ctx context.Context, r *user.GetUserByEmailRequest) (*user.GetUserByEmailResponse, error)
	UpdateUser(ctx context.Context, user *user.UpdateUserRequest) (*user.UpdateUserResponse, error)
	CreateUser(ctx context.Context, r *user.CreateUserRequest) (*user.CreateUserResponse, error)
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

// Service holds and manages accessmanager service business logic
type Service struct {
	EphemeralStore        EphemeralStore
	AuditService          AuditService
	EmailManager          EmailManager
	BillingService        BillingService
	AuthService           AuthService
	UserService           UserService
	ApitokenService       ApitokenService
	OauthServices         []OauthService
	StaticPlaceholderUuid string
}

// NewServiceRequest holds all expected dependencies for an accessmanager service
type NewServiceRequest struct {
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

// UpdateUserEmail updates the email address of a user. It performs the following steps:
// 1. Checks if the requesting user is the same as the target user or if the requesting user is an admin.
// 2. Retrieves the current email address of the target user.
// 3. Checks if the new email address is the same as the current email address.
// 4. Checks if the new email address is already in use by another user.
// 5. Sends an email notification to the current email address about the email change request.
// 6. Updates the target user's email address and status.
// 7. Logs out all other sessions of the target user.
// 8. Sends a verification email to the new email address.
// 9. Logs an audit event for the email change.
// The function returns a boolean indicating whether the user needs to be signed out of the platform, and an error if any.
func (s *Service) UpdateUserEmail(ctx context.Context, r *UpdateUserEmailRequest) (bool, error) {

	var (
		log *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)

		// whether a change has taken place in this method which means that the user needs to be
		// signed off of the client they are currently using to make the request
		signUserOutOfPlatform bool = false

		requestingUser User
	)

	// check that the user id the same as the target user id or the user is an admin
	if r.UserId != r.TargetUserId {
		// check if the user is an admin
		userByIdResponse, err := s.UserService.GetUserByID(ctx, &user.GetUserByIdRequest{
			Id: r.UserId,
		})
		if err != nil {
			log.Error("ams/failed-to-get-requesting-user-by-id", zap.Error(err))
			return signUserOutOfPlatform, err
		}

		requestingUser = &userByIdResponse.User

		if !requestingUser.IsAdmin() {
			log.Warn("ams/non-admin-user-attempted-to-update-another-user-email", zap.String("user-id", r.UserId), zap.String("target-user-id", r.TargetUserId))
			return signUserOutOfPlatform, errors.New(ErrKeyForbiddenUnableToAction)
		}
	}

	// check if the user's old email is the same as the new email (error with no neeed to signout)
	userByIdResponse, err := s.UserService.GetUserByID(ctx, &user.GetUserByIdRequest{
		Id: r.TargetUserId,
	})
	if err != nil {
		log.Error("ams/failed-to-get-target-user-by-id", zap.Error(err))
		return signUserOutOfPlatform, err
	}

	targetUser := &userByIdResponse.User

	// if above ok, take copy the user's old email
	emailPathValue, err := targetUser.GetAttributeByJsonPath("$.email")
	if err != nil {
		log.Error("ams/failed-to-get-target-user-email-using-json", zap.String("target-user-id", r.TargetUserId), zap.Error(err))
		return signUserOutOfPlatform, err
	}

	emailPathValueString, ok := emailPathValue.(string)
	if !ok || emailPathValueString == "" {
		log.Error("ams/failed-to-get-target-user-email-as-string", zap.String("target-user-id", r.TargetUserId))
		return signUserOutOfPlatform, errors.New(ErrKeyConflictingUserState)
	}

	standardiseExistingEmail := toolbox.StringStandardisedToLower(emailPathValueString)
	standardiseNewEmail := toolbox.StringStandardisedToLower(r.Email)
	if standardiseExistingEmail == standardiseNewEmail {
		log.Warn("ams/user-attempted-to-update-email-to-same-email", zap.String("user-id", r.UserId), zap.String("target-user-id", r.TargetUserId))
		return signUserOutOfPlatform, errors.New(ErrKeyConflictingUserState)
	}

	// check if the new email is already in use
	userByEmailResponse, newEmailInUseErr := s.UserService.GetUserByEmail(ctx, &user.GetUserByEmailRequest{
		Email: r.Email,
	})
	if newEmailInUseErr != nil && newEmailInUseErr.Error() != user.ErrKeyResourceNotFound {
		log.Error("ams/failed-to-verify-whether-new-email-already-in-use", zap.String("user-id", r.UserId), zap.String("target-user-id", r.TargetUserId), zap.Error(err))
		return signUserOutOfPlatform, newEmailInUseErr
	}
	if newEmailInUseErr == nil {
		log.Warn("ams/new-email-already-in-use", zap.String("existing-user-id", userByEmailResponse.User.GetUserId()), zap.String("target-user-id", r.TargetUserId), zap.String("user-id", r.UserId))
		return signUserOutOfPlatform, errors.New(ErrKeyConflictingUserState)
	}

	// if here, then the new email is not in use

	// send email to old email
	emailBodyToNotifyExistingEmail := fmt.Sprintf(UpdateUserEmailOldEmailNotificationBodyTmpl, standardiseExistingEmail, standardiseNewEmail, targetUser.GetUserId())

	err = s.EmailManager.SendCustomEmail(ctx, &emailmanager.SendCustomEmailRequest{
		EmailSubject:  "Email Change Request Received",
		EmailPreview:  "A request to change your account email is being processed",
		EmailTo:       standardiseExistingEmail,
		EmailBody:     emailBodyToNotifyExistingEmail,
		WithFooter:    true,
		UserId:        targetUser.GetUserId(),
		RecipientType: string(audit.User),
	})
	if err != nil {
		log.Error("ams/unable-to-send-change-of-email-request-notification-email-for-to-old-email:", zap.String("user-id", r.TargetUserId))
		return signUserOutOfPlatform, err
	}

	// update the target users' account with the new email, make sure the email is unique
	// and unverify the email on the account
	_, err = targetUser.UpdateStatus(user.AccountStatusValidOriginKeyEmailChange)
	if err != nil {
		log.Warn("ams/unable-to-update-status-of-user-to-provisioned-after-email-change", zap.String("user-id", r.UserId), zap.String("target-user-id", r.TargetUserId), zap.Error(err))
		return signUserOutOfPlatform, errors.New(ErrKeyConflictingUserState)
	}

	// set the new email
	targetUser.Email = toolbox.StringStandardisedToLower(r.Email)
	targetUser.SetUpdatedAtTimeToNow()

	// update the user
	_, err = s.UserService.UpdateUser(ctx, &user.UpdateUserRequest{
		User: targetUser,
	})
	if err != nil {
		log.Error("ams/failed-to-update-user-with-new-email", zap.String("user-id", r.UserId), zap.String("target-user-id", r.TargetUserId), zap.Error(err))
		return signUserOutOfPlatform, err
	}

	// From here, we need to sign out of platform
	// on failure/ success
	signUserOutOfPlatform = true

	// stop all other sessions (ignore errors)
	wipeOldSessionsErr := s.LogoutUserOthers(ctx, &LogoutUserOthersRequest{
		UserId:       r.TargetUserId,
		RefreshToken: r.RefreshToken,
		AuthToken:    r.AuthToken,
	})
	if wipeOldSessionsErr != nil {
		log.Error("ams/failed-to-logout-users-other-sessions-for-email-change-request", zap.String("user-id", r.UserId), zap.String("target-user-id", r.TargetUserId), zap.Error(wipeOldSessionsErr))
	}
	if wipeOldSessionsErr == nil {
		log.Info("ams/logged-out-users-other-sessions-for-email-change-request", zap.String("user-id", r.UserId), zap.String("target-user-id", r.TargetUserId))
	}

	// log out of the current session (ignore errors)
	logOutCurrentSessionErr := s.LogoutUser(ctx, r.Request)
	if logOutCurrentSessionErr != nil {
		log.Error("ams/failed-to-logout-user-current-sessions-for-email-change-request", zap.String("user-id", r.UserId), zap.String("target-user-id", r.TargetUserId), zap.Error(logOutCurrentSessionErr))
	}
	if logOutCurrentSessionErr == nil {
		log.Info("ams/logged-out-user-current-sessions-for-email-change-request", zap.String("user-id", r.UserId), zap.String("target-user-id", r.TargetUserId))
	}

	// send a verification email to the new email address
	log.Info(fmt.Sprintf("ams/initiate-verification-email-for-user-with-changed-email: %s", targetUser.GetUserId()))
	_, err = s.CreateEmailVerificationToken(ctx, &CreateEmailVerificationTokenRequest{
		User:       *targetUser,
		RequestUrl: "",
	})
	if err != nil {
		log.Error(fmt.Sprintf("ams/error-failed-to-initiate-verification-email-for-user-with-changed-emai: %s", targetUser.GetUserId()))
		return signUserOutOfPlatform, err
	}

	auditEvent := audit.UserAccountChangeEmail
	auditErr := s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
		ActorId:    audit.AuditActorIdSystem,
		Action:     auditEvent,
		TargetId:   targetUser.GetUserId(),
		TargetType: audit.User,
		Domain:     "accessmanager",
		Details: map[string]interface{}{
			"email_old": standardiseExistingEmail,
			"email_new": standardiseNewEmail,
		},
	})

	if auditErr != nil {
		log.Warn("ams/failed-to-log-event", zap.String("actor-id", audit.AuditActorIdSystem), zap.String("user-id", r.UserId), zap.String("target-user-id", r.TargetUserId), zap.String("event-type", string(auditEvent)))
	}

	return signUserOutOfPlatform, nil
}

// LogoutUserOthers handles logic of managing the user's other log in session
func (s *Service) LogoutUserOthers(ctx context.Context, r *LogoutUserOthersRequest) error {

	var accessTokenId string
	var refreshTokenId string

	// Check if ID returns valid user
	requestingUser, err := s.UserService.GetUserByID(ctx, &user.GetUserByIdRequest{
		Id: r.UserId,
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

// OauthCallback handles logic of managing the callback of a provider
func (s *Service) OauthCallback(ctx context.Context, r *OauthCallbackRequest) (*OauthCallbackResponse, error) {

	var log *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	if len(s.OauthServices) == 0 {
		log.Error("no-oauth-provider-passed-to-access-manager-but-oauth-callback-requested", zap.String("requested-provider", r.Provider))
		return nil, errors.New("ErrKeyNoOauthProvidersDetected")
	}

	for _, provider := range s.OauthServices {

		if r.Provider != provider.ProviderGetName() {
			log.Info("skipping-oauth-provider-does-not-match-requested", zap.String("requested-provider", r.Provider), zap.String("sourced-provider", provider.ProviderGetName()))
			continue
		}

		// get protection token (state) from cookie
		var fetchedProtectionStateTokenCookie *http.Cookie

		// create variable to hold unencoded redirect url
		var detectedUnencodedRedirectUrl string

		for _, requestCookie := range r.RequestCookies {
			if requestCookie.Name != provider.ProviderGetCookieKey() {
				continue
			}

			fetchedProtectionStateTokenCookie = requestCookie
			break
		}

		if fetchedProtectionStateTokenCookie == nil {
			return nil, errors.New("ErrKeyProviderCookieNotFound")
		}

		// Compare the protection token (state) from cookie with the one passed in
		// the request
		providerCookieKey, providerRequestAuthenticated := provider.ProviderVerifyRequestIsAuthentic(r.UrlUri, fetchedProtectionStateTokenCookie)

		if !providerRequestAuthenticated {
			return &OauthCallbackResponse{
				ProviderStateCookieKey: providerCookieKey,
			}, errors.New("ErrKeyProviderInvalidProtectionStateToken")
		}

		// check if redirect url passed
		splitProtectionStateTokenCookieValue := strings.Split(fetchedProtectionStateTokenCookie.Value, ".")
		if len(splitProtectionStateTokenCookieValue) > 1 {

			decoded64RequestUrl, err := base64.StdEncoding.DecodeString(splitProtectionStateTokenCookieValue[1])
			if err != nil {
				log.Warn("failed-to-decode-detected-request-url-uri-for-sso-callback", zap.String("encoded-request-url", splitProtectionStateTokenCookieValue[1]))
			}

			if err == nil && string(decoded64RequestUrl) != "" {
				detectedUnencodedRedirectUrl = string(decoded64RequestUrl)
			}
		}

		// get user data
		providerUserInfo, err := provider.ProviderGetUserData(ctx, r.UrlUri)
		if err != nil {
			return &OauthCallbackResponse{
				ProviderStateCookieKey: providerCookieKey,
			}, err
		}

		// Manage flow with user information
		persistentUserResponse, err := s.UserService.GetUserByEmail(ctx, &user.GetUserByEmailRequest{Email: providerUserInfo.GetUserEmail()})
		// Check if there is an error outside of user not being found
		if persistentUserResponse == nil && err.Error() != user.ErrKeyResourceNotFound {
			return &OauthCallbackResponse{
				ProviderStateCookieKey: providerCookieKey,
			}, err
		}

		// Handle if user exists, generate auth tokens
		if err == nil {
			persistentUser := persistentUserResponse.User

			tokenDetails, err := s.AuthService.CreateToken(ctx, &persistentUser)
			if err != nil {
				return &OauthCallbackResponse{
					ProviderStateCookieKey: providerCookieKey,
				}, err
			}

			// update users logged in time
			persistentUser.SetLastLoginAtTimeToNow().SetLastFreshLoginAtTimeToNow()

			// If user is verified by provider but not our platform, we should trust provider
			if !persistentUser.Verified.EmailVerified && providerUserInfo.IsUserEmailVerifiedByProvider() {

				log.Info("provider-login-user-email-verified-based-on-provider-records", zap.String("user-id", persistentUser.ID))
				persistentUser.Verified.EmailVerified = providerUserInfo.IsUserEmailVerifiedByProvider()
				persistentUser.Verified.EmailVerifiedAt = toolbox.TimeNowUTC()
			}

			UpdateUserResponse, err := s.UserService.UpdateUser(ctx, &user.UpdateUserRequest{User: &persistentUser})
			if err != nil {
				log.Error("provider-login-user-update-failed-after-successful-login-initiation", zap.String("user-id:", persistentUser.ID))
				return &OauthCallbackResponse{
					ProviderStateCookieKey: providerCookieKey,
				}, err
			}

			err = s.EphemeralStore.CreateAuth(ctx, UpdateUserResponse.User.ID, tokenDetails)
			if err != nil {
				log.Error("provider-login-ephemeral-store-failed-after-successful-login-initiation", zap.String("user-id:", persistentUser.ID))
				return &OauthCallbackResponse{
					ProviderStateCookieKey: providerCookieKey,
				}, err
			}

			// audit log sso login
			auditEvent := audit.UserLoginSso
			auditErr := s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
				ActorId:    audit.AuditActorIdSystem,
				Action:     auditEvent,
				TargetId:   persistentUser.ID,
				TargetType: audit.User,
				Domain:     "accessmanager",
				Details: audit.UserSsoEventDetails{
					SsoProvider: r.Provider,
				},
			})

			if auditErr != nil {
				log.Warn("failed-to-log-event", zap.String("actor-id", audit.AuditActorIdSystem), zap.String("user-id", persistentUser.ID), zap.String("event-type", string(auditEvent)))
			}

			return &OauthCallbackResponse{
				RequestUrl:             detectedUnencodedRedirectUrl,
				ProviderStateCookieKey: providerCookieKey,
				AccessToken:            tokenDetails.AccessToken,
				RefreshToken:           tokenDetails.RefreshToken,
				AccessTokenExpiresAt:   tokenDetails.AtExpires,
				RefreshTokenExpiresAt:  tokenDetails.RtExpires,
			}, nil
		} else { // if not, create user, generate token

			newUserResp, err := s.CreateUser(ctx, &CreateUserRequest{
				DisableVerificationEmail: true,
				FirstName:                providerUserInfo.GetUserFirstName(),
				LastName:                 providerUserInfo.GetUserLastName(),
				Email:                    providerUserInfo.GetUserEmail(),
			})
			if err != nil {
				log.Error("provider-signup-user-creation-failed-after-successful-login-initiation", zap.String("user-email:", providerUserInfo.GetUserEmail()))
				return &OauthCallbackResponse{
					ProviderStateCookieKey: providerCookieKey,
				}, err
			}

			// If user is verified by provider but not our platform, we should trust provider
			if !newUserResp.User.Verified.EmailVerified && providerUserInfo.IsUserEmailVerifiedByProvider() {

				log.Info("provider-signup-user-email-verified-based-on-provider-records", zap.String("user-id", newUserResp.User.ID))
				newUserResp.User.Verified.EmailVerified = providerUserInfo.IsUserEmailVerifiedByProvider()
				newUserResp.User.Verified.EmailVerifiedAt = toolbox.TimeNowUTC()

				// Update user with verification information
				updatedUser, err := s.UserService.UpdateUser(ctx, &user.UpdateUserRequest{
					User: &newUserResp.User,
				})
				if err != nil {
					log.Error(fmt.Sprintf("ams/error-failed-to-save-new-user-verficaiton-by-provider: %s", newUserResp.User.ID))
				}

				log.Info(fmt.Sprintf("ams/successfully-saved-new-user-verficaiton-by-provider: %s", updatedUser.User.ID))

			}

			// audit log new sso user
			auditEvent := audit.UserAccountNewSso
			auditErr := s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
				ActorId:    audit.AuditActorIdSystem,
				Action:     auditEvent,
				TargetId:   newUserResp.User.ID,
				TargetType: audit.User,
				Domain:     "accessmanager",
				Details: audit.UserSsoEventDetails{
					SsoProvider: r.Provider,
				},
			})

			if auditErr != nil {
				log.Warn("failed-to-log-event", zap.String("actor-id", audit.AuditActorIdSystem), zap.String("user-id", newUserResp.User.ID), zap.String("event-type", string(auditEvent)))
			}

			log.Info(fmt.Sprintf("ams/initiate-new-user-tokens: %s", newUserResp.User.ID))

			accessToken, accessTokenExpiresAt, refreshToken, refreshTokenExpiresAt, err := s.UserEmailVerificationRevisions(ctx, &UserEmailVerificationRevisionsRequest{
				UserID: newUserResp.User.ID})
			if err != nil {
				return nil, err
			}

			// audit log sso login
			auditEvent = audit.UserLoginSso
			auditErr = s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
				ActorId:    audit.AuditActorIdSystem,
				Action:     auditEvent,
				TargetId:   newUserResp.User.ID,
				TargetType: audit.User,
				Domain:     "accessmanager",
				Details: audit.UserSsoEventDetails{
					SsoProvider: r.Provider,
				},
			})

			if auditErr != nil {
				log.Warn("failed-to-log-event", zap.String("actor-id", audit.AuditActorIdSystem), zap.String("user-id", newUserResp.User.ID), zap.String("event-type", string(auditEvent)))
			}

			return &OauthCallbackResponse{
				RequestUrl:             detectedUnencodedRedirectUrl,
				ProviderStateCookieKey: providerCookieKey,
				AccessToken:            accessToken,
				RefreshToken:           refreshToken,
				AccessTokenExpiresAt:   accessTokenExpiresAt,
				RefreshTokenExpiresAt:  refreshTokenExpiresAt,
			}, nil

		}
	}

	return nil, errors.New("ErrKeyProvidersPassedNotFound")
}

// OauthLogin handles logic of managing the initialisation of provider url
func (s *Service) OauthLogin(ctx context.Context, r *OauthLoginRequest) (*OauthLoginResponse, error) {

	var log *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	if len(s.OauthServices) == 0 {
		log.Error("no-oauth-provider-passed-to-access-manager-but-oauth-login-requested", zap.String("requested-provider", r.Provider))
		return nil, errors.New("ErrKeyNoOauthProvidersDetected")
	}

	for _, provider := range s.OauthServices {

		if r.Provider != provider.ProviderGetName() {
			log.Info("skipping-oauth-provider-does-not-match-requested", zap.String("requested-provider", r.Provider), zap.String("sourced-provider", provider.ProviderGetName()))
			continue
		}

		// generate protection token (state) query
		protectionStateToken := provider.ProviderGenerateProtectionToken()

		// append base64 redirect url
		if r.RequestUrl != "" {

			encoded64RequestUrl := base64.StdEncoding.EncodeToString([]byte(r.RequestUrl))

			protectionStateToken += fmt.Sprintf(`.%s`, encoded64RequestUrl)
		}

		// Create cookie for holding oauth state
		oauthCookie := http.Cookie{
			// 20 minutes expiry
			Expires: time.Now().Add(20 * time.Minute),
			Name:    provider.ProviderGetCookieKey(),
			Value:   protectionStateToken,
		}

		return &OauthLoginResponse{
			CookieCore:          &oauthCookie,
			ProviderAuthCodeUrl: provider.ProviderGenerateAuthCodeUrl(protectionStateToken),
		}, nil

	}

	return nil, errors.New("ErrKeyProvidersPassedNotFound")
}

// GetSpecificUserAPITokens retrieves API token for a specific user
// TODO: Create tests
func (s *Service) GetSpecificUserAPITokens(ctx context.Context, r *GetSpecificUserAPITokensRequest) (*GetSpecificUserAPITokensResponse, error) {

	userApiTokenResponse, err := s.ApitokenService.GetAPITokensFor(ctx, &apitoken.GetAPITokensForRequest{
		ID:            r.UserID,
		Order:         r.Order,
		PerPage:       r.PerPage,
		Page:          r.Page,
		Description:   r.Description,
		Status:        r.Status,
		Meta:          r.Meta,
		OnlyEphemeral: r.OnlyEphemeral,
		OnlyPermanent: r.OnlyPermanent,
	})
	if err != nil {
		return nil, err
	}

	return &GetSpecificUserAPITokensResponse{
		UserAPITokens:    userApiTokenResponse.APITokens,
		Total:            userApiTokenResponse.Total,
		TotalPages:       userApiTokenResponse.TotalPages,
		Page:             userApiTokenResponse.Page,
		ResourcesPerPage: userApiTokenResponse.APITokensPerPage,
	}, nil
}

// GetUserAPITokenThreshold retrieves the thresholds applied to the user r. API tokens
func (s *Service) GetUserAPITokenThreshold(ctx context.Context, r *GetUserAPITokenThresholdRequest) (*GetUserAPITokenThresholdResponse, error) {

	// Check if user exist
	userResponse, err := s.UserService.GetUserByID(ctx, &user.GetUserByIdRequest{
		Id: r.UserId,
	})
	if err != nil {
		return nil, err
	}

	persistentUser := userResponse.User

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

// UpdateUserAPITokenStatus updates status on specified API token
// TODO: Create tests
func (s *Service) UpdateUserAPITokenStatus(ctx context.Context, r *UserAPITokenStatusRequest) error {
	switch r.Status {
	case apitoken.UserTokenStatusKeyActive:
		return s.ApitokenService.ActivateAPIToken(ctx, &apitoken.ActivateAPITokenRequest{
			ID: r.APITokenID})
	default:
		return s.ApitokenService.RevokeAPIToken(ctx, &apitoken.RevokeAPITokenRequest{
			ID: r.APITokenID,
		})
	}
}

// DeleteUserAPIToken delete specified API token for user
// TODO: Create tests
func (s *Service) DeleteUserAPIToken(ctx context.Context, r *DeleteUserAPITokenRequest) error {
	// Check if user exist
	_, err := s.UserService.GetUserByID(ctx, &user.GetUserByIdRequest{
		Id: r.UserID,
	})
	if err != nil {
		return err
	}

	// get all user api tokens
	userApiTokens, err := s.GetSpecificUserAPITokens(ctx, &GetSpecificUserAPITokensRequest{
		UserID: r.UserID,
		GetAPITokensForRequest: &apitoken.GetAPITokensForRequest{
			PerPage: 100, // Fetch all user tokens
		},
	})
	if err != nil {
		return err
	}

	// If the user has tokens, check if the requested token is associated with the user
	if len(userApiTokens.UserAPITokens) > 0 {

		for _, token := range userApiTokens.UserAPITokens {
			if token.ID == r.APITokenID {
				return s.ApitokenService.DeleteAPIToken(ctx,
					&apitoken.DeleteAPITokenRequest{
						UserID:     r.UserID,
						APITokenID: r.APITokenID,
					})
			}
		}

	}

	return errors.New(ErrKeyAPITokenNotAssociatedWithUser)
}

// CreateUserAPIToken generates API token for user
// TODO: Create tests
func (s *Service) CreateUserAPIToken(ctx context.Context, r *CreateUserAPITokenRequest) (*CreateUserAPITokenResponse, error) {
	var log *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	// Check if user exist
	userResponse, err := s.UserService.GetUserByID(ctx, &user.GetUserByIdRequest{
		Id: r.UserID,
	})
	if err != nil {
		return nil, err
	}

	persistentUser := userResponse.User

	// Pull user role so we can get their limits
	userHighestRankingRole := common.GetUsersHighestRankedRole(persistentUser.Roles)
	getUserRoleThresholdAllocation := common.UserRolesThresholds.RolesDetails[common.UserRole(userHighestRankingRole)]
	if !toolbox.StringInSlice(userHighestRankingRole, persistentUser.Roles) {
		log.Error("highest-role-pulled-is-not-found-in-user-allocated-role", zap.String("user-id", persistentUser.ID), zap.String("role-pulled", userHighestRankingRole), zap.String("user-roles", strings.Join(persistentUser.Roles, ", ")))
	}

	// get user's apitokens count
	userPermanentTokenCount, userEphemeralTokenCount, err := s.getUserApiTokensCountByType(ctx, persistentUser.ID)
	if err != nil {
		return nil, err
	}

	// Make sure user doesn't have more than allowed tokens already
	if r.Ttl == 0 && (int64(userPermanentTokenCount) >= getUserRoleThresholdAllocation.LongLivedUserTokenLimit) {
		return nil, errors.New(ErrKeyPermanentAPITokenLimitReached)
	}

	if r.Ttl > 0 && (int64(userEphemeralTokenCount) >= getUserRoleThresholdAllocation.ShortLivedUserTokenLimit) {
		return nil, errors.New(ErrKeyEphemeralAPITokenLimitReached)
	}

	// TODO: Make sure request honor role's increments etc.
	err = s.verifyRequestIsWithinUserRoleTokenConstraints(ctx, persistentUser.ID, r.Ttl, &getUserRoleThresholdAllocation)
	if err != nil {
		return nil, err
	}

	// Generate token
	apiTokenResponse, err := s.ApitokenService.CreateAPIToken(ctx, &apitoken.CreateAPITokenRequest{
		UserID:     persistentUser.ID,
		UserNanoId: persistentUser.NanoId,
		TokenTtl:   r.Ttl,
	})
	if err != nil {
		return nil, err
	}

	return &CreateUserAPITokenResponse{
		UserAPIToken: apiTokenResponse.APIToken,
	}, nil
}

// verifyRequestIsWithinUserRoleTokenConstraints is taking the token time and making sure it's within the user's
// allocated thresholds
func (s *Service) verifyRequestIsWithinUserRoleTokenConstraints(ctx context.Context, userId string, tokenTtl int64, userRoleThresholds *common.UserRoleThresholds) error {

	var log *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	if tokenTtl == 0 {
		return nil
	}

	if tokenTtl < userRoleThresholds.ShortLivedMinimumAllowedTime {
		log.Error("failed-to-create-user-ephemeral-token", zap.String("failure-reason", "ttl-too-short"), zap.String("user-id", userId), zap.Int64("requested-ttl", tokenTtl), zap.Int64("user-role-rank", userRoleThresholds.Ranking))
		return errors.New(ErrKeyCreateUserAPITokenRequestTtlTooShort)
	}

	if tokenTtl > userRoleThresholds.ShortLivedMaximumAllowedTime {
		log.Error("failed-to-create-user-ephemeral-token", zap.String("failure-reason", "ttl-too-long"), zap.String("user-id", userId), zap.Int64("requested-ttl", tokenTtl), zap.Int64("user-role-rank", userRoleThresholds.Ranking))
		return errors.New(ErrKeyCreateUserAPITokenRequestTtlTooLong)
	}

	if tokenTtl%userRoleThresholds.ShortLivedMinimumIncrements != 0 {
		log.Error("failed-to-create-user-ephemeral-token", zap.String("failure-reason", "ttl-outside-allowed-increment"), zap.String("user-id", userId), zap.Int64("requested-ttl", tokenTtl), zap.Int64("user-role-rank", userRoleThresholds.Ranking))
		return errors.New(ErrKeyCreateUserAPITokenRequestTtlOutsideAllowedIncrement)
	}

	return nil
}

// getUserApiTokensCountByType is handling getting user's token and returning
// how many are Permanent or Ephemeral
func (s *Service) getUserApiTokensCountByType(ctx context.Context, userId string) (int, int, error) {

	var (
		userPermanentToken int
		userEphemeralToken int
		log                *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)
	)

	// get user api tokens
	userApiTokens, err := s.ApitokenService.GetAPITokensFor(ctx, &apitoken.GetAPITokensForRequest{
		PerPage: 100,
		Page:    1,
		ID:      userId,
	})

	if err != nil {
		log.Error("error-fetching-user-auth-tokens", zap.Error(err))
		return 0, 0, err
	}

	// get token types
	for _, apiToken := range userApiTokens.APITokens {

		if apiToken.IsShortLivedToken() {
			userEphemeralToken++
			continue
		}

		userPermanentToken++
		continue
	}

	return userPermanentToken, userEphemeralToken, nil
}

// MiddlewareAdminAPITokenRequired handles the business/ cross logic of ensuring
// that the request is passed with a valid admin client ID and secret
func (s *Service) MiddlewareAdminAPITokenRequired(r *http.Request) (string, error) {

	var log *zap.Logger = logger.AcquireFrom(r.Context()).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	tokenRequester, err := s.ApitokenService.ExtractValidateUserAPITokenMetadata(r.Context(), r)
	if err != nil {
		return "", err
	}

	// If we made it here, it means we've found a matching token
	// get user for matching token
	// could probably also check liveness here one time
	persistentUserResponse, err := s.UserService.GetUserByNanoId(r.Context(), tokenRequester.NanoId)
	if err != nil {
		return "", err
	}

	// Set token requester user Id to make backwards compatible
	tokenRequester.UserID = persistentUserResponse.User.ID

	if !persistentUserResponse.User.IsAdmin() {
		log.Warn("unauthorized-admin-access-attempted", zap.String("user-id", persistentUserResponse.User.GetUserId()))
		return "", errors.New(ErrKeyUnauthorizedAdminAccessAttempted)
	}

	// Check if user it active
	if persistentUserResponse.User.Status != user.AccountStatusKeyActive {
		log.Warn("unauthorized-non-active-status", zap.String("user-id", tokenRequester.UserID), zap.String("token-id", tokenRequester.UserAPIToken))
		return "", errors.New(ErrKeyUnauthorizedNonActiveStatus)
	}

	// Update last used time on token
	err = s.ApitokenService.UpdateAPITokenLastUsedAt(r.Context(), &apitoken.UpdateAPITokenLastUsedAtRequest{
		APITokenEncoded: tokenRequester.UserAPITokenEncoded,
		ClientID:        tokenRequester.UserID,
	})
	if err != nil {
		log.Warn("failed-updating-token-last-used-at", zap.String("user-id", tokenRequester.UserID), zap.String("token-id", tokenRequester.UserAPIToken))
	}

	log.Info("validated-token-request", zap.String("user-id", tokenRequester.UserID), zap.String("token-id", tokenRequester.UserAPIToken))

	return tokenRequester.UserID, nil
}

// MiddlewareValidAPITokenRequired handles the business/ cross logic of ensuring
// that the request is passed with a valid client ID and secret
// TODO: Create tests
func (s *Service) MiddlewareValidAPITokenRequired(r *http.Request) (string, error) {

	var log *zap.Logger = logger.AcquireFrom(r.Context()).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	tokenRequester, err := s.ApitokenService.ExtractValidateUserAPITokenMetadata(r.Context(), r)
	if err != nil {
		return "", err
	}

	// If we made it here, it means we've found a matching token
	// get user for matching token
	// could probably also check liveness here one time
	persistentUserResponse, err := s.UserService.GetUserByNanoId(r.Context(), tokenRequester.NanoId)
	if err != nil {
		return "", err
	}

	// Set token requester user Id to make backwards compatible
	tokenRequester.UserID = persistentUserResponse.User.ID

	// Check if user it active
	if persistentUserResponse.User.Status != user.AccountStatusKeyActive {
		log.Warn("unauthorized-non-active-status", zap.String("user-id", tokenRequester.UserID), zap.String("token-id", tokenRequester.UserAPIToken))
		return "", errors.New(ErrKeyUnauthorizedNonActiveStatus)
	}

	// Update last used time on token
	err = s.ApitokenService.UpdateAPITokenLastUsedAt(r.Context(), &apitoken.UpdateAPITokenLastUsedAtRequest{
		APITokenEncoded: tokenRequester.UserAPITokenEncoded,
		ClientID:        tokenRequester.UserID,
	})
	if err != nil {
		log.Warn("failed-updating-token-last-used-at", zap.String("user-id", tokenRequester.UserID), zap.String("token-id", tokenRequester.UserAPIToken))
	}

	log.Info("validated-token-request", zap.String("user-id", tokenRequester.UserID), zap.String("token-id", tokenRequester.UserAPIToken))

	return tokenRequester.UserID, nil
}

// MiddlewareJWTRequired handles the business/ cross logic of ensuring
// that the request is passed with a valid, non-expired token
// TODO: Create tests
func (s *Service) MiddlewareJWTRequired(r *http.Request) (string, error) {
	var log *zap.Logger = logger.AcquireFrom(r.Context()).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	tokenAuth, err := s.AuthService.ExtractTokenMetadata(r.Context(), r)
	if err != nil {
		return "", err
	}

	_, err = s.EphemeralStore.FetchAuth(r.Context(), tokenAuth)
	if err != nil {
		log.Warn("unauthorized-token-not-found", zap.String("user-id", tokenAuth.UserID))
		return "", errors.New(ErrKeyUnauthorizedTokenNotFoundInStore)
	}

	return tokenAuth.UserID, nil
}

// MiddlewareActiveJWTRequired handles the business/ cross logic of ensuring that the request is passed with a
// valid token, and the user is in an `ACTIVE` state (status)
// TODO: Create tests
func (s *Service) MiddlewareActiveJWTRequired(r *http.Request) (string, error) {
	tokenAuth, err := s.AuthService.ExtractTokenMetadata(r.Context(), r)
	if err != nil {
		return "", err
	}

	return s.checkActivenessOfUser(r.Context(), tokenAuth)
}

// MiddlewareAdminJWTRequired handles the business/ cross logic of making sure the token passed is
// that of a platform admin, for middleware
// TODO: Create tests
func (s *Service) MiddlewareAdminJWTRequired(r *http.Request) (string, error) {
	var log *zap.Logger = logger.AcquireFrom(r.Context()).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	tokenAuth, err := s.AuthService.ExtractTokenMetadata(r.Context(), r)
	if err != nil {
		return "", err
	}

	if !tokenAuth.IsAdmin {
		log.Warn("unauthorized-admin-access-attempted", zap.String("user-id", tokenAuth.UserID))
		return "", errors.New(ErrKeyUnauthorizedAdminAccessAttempted)
	}

	// Check when user was `ACTIVE` when access token was generated
	if !tokenAuth.IsAuthorized {
		log.Warn("unauthorized-non-active-status", zap.String("user-id", tokenAuth.UserID))
		return "", errors.New(ErrKeyUnauthorizedNonActiveStatus)
	}

	_, err = s.EphemeralStore.FetchAuth(r.Context(), tokenAuth)
	if err != nil {
		log.Warn("unauthorized-token-not-found", zap.String("user-id", tokenAuth.UserID))
		return "", errors.New(ErrKeyUnauthorizedTokenNotFoundInStore)
	}

	return tokenAuth.UserID, nil
}

// MiddlewareRateLimitOrActiveJWTRequired handles the business/ cross logic of ensuring
// that the request has not exceeded its rate limit, and any unathed request is given the
// default annoymous user ID.
// Otherwise, if a bearer token is detected, typical check is carried out to ensure that the
// passed token is valid, non-expired token
// TODO: Create tests
func (s *Service) MiddlewareRateLimitOrActiveJWTRequired(r *http.Request) (string, error) {

	tokenAuth, err := s.AuthService.ExtractTokenMetadata(r.Context(), r)
	if err != nil && err.Error() == auth.ErrKeyNoBearerHeaderFound {
		// Register request count for unauth user (note IP used)
		if ephErr := s.EphemeralStore.AddRequestCountEntry(r.Context(), getValidRequestorIP(r)); ephErr != nil {
			return "", ephErr
		}

		return s.StaticPlaceholderUuid, nil
	}

	if err != nil {
		return "", err
	}

	return s.checkActivenessOfUser(r.Context(), tokenAuth)
}

// checkActivenessOfUser validates whether the user's account was in an active state at time of
// token creation
func (s *Service) checkActivenessOfUser(ctx context.Context, tokenAuth *auth.TokenAccessDetails) (string, error) {
	var log *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	// Check when user was `ACTIVE` when access token was generated
	if !s.isUserLiveStatusActive(ctx, tokenAuth.UserID) {
		log.Warn("unauthorized-non-active-status", zap.String("user-id", tokenAuth.UserID))
		return "", errors.New(ErrKeyUnauthorizedNonActiveStatus)
	}

	return tokenAuth.UserID, nil
}

// LogoutUser handles the logic of signing user off of platform. Delete token(s) from ephemeral store
// TODO: Investigate best way to also delete corresponding refresh token
// TODO: Create tests
func (s *Service) LogoutUser(ctx context.Context, r *http.Request) error {
	var log *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	accessTokenDetails, err := s.AuthService.ExtractTokenMetadata(ctx, r)
	if err != nil {
		return err
	}

	deleted, err := s.DeleteAuth(ctx, toolbox.CombinedUuidFormat(accessTokenDetails.UserID, accessTokenDetails.AccessUUID))
	if err != nil {
		log.Error("ephemeral-delete-failed-after-successful-access-token-retrival", zap.String("user-id:", accessTokenDetails.UserID), zap.Error(err))
		return err
	}

	if deleted == 0 {
		log.Error("ephemeral-delete-failed-after-successful-access-token-retrival", zap.String("user-id:", accessTokenDetails.UserID))
		return errors.New(ErrKeyUnauthorizedAccessTokenCacheDeletionFailure)
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
		log.Warn("failed-to-log-event", zap.String("actor-id", audit.AuditActorIdSystem), zap.String("user-id", accessTokenDetails.UserID), zap.String("event-type", string(auditEvent)))
	}

	return nil
}

// RefreshToken handles the logic of creating a new pair of tokens as well as the relevent sanity
// checks
// TODO: Create tests
func (s *Service) RefreshToken(ctx context.Context, r *RefreshTokenRequest) (*RefreshTokenResponse, error) {
	var log *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	tokenUser, refreshTokenUuid, err := s.RemoveRefreshTokenWithCookieValue(ctx, r.RefreshToken)
	if err != nil {
		return nil, err
	}

	// check if access token is present and clean up along with it
	if r.AccessToken != "" {

		log.Info("access-token-present-in-refresh-token-request", zap.String("user-id", tokenUser.GetUserId()), zap.String("refresh-token", refreshTokenUuid))

		err := s.RemoveAccessTokenWithCookieValue(ctx, tokenUser.GetUserId(), r.AccessToken)
		if err != nil {
			log.Warn("access-token-failed-to-delete-after-successful-refresh-token-clean-up", zap.String("user-id", tokenUser.GetUserId()), zap.Error(err))
		} else {
			log.Info("access-token-deleted-after-successful-refresh-token-clean-up", zap.String("user-id", tokenUser.GetUserId()), zap.String("refresh-token", refreshTokenUuid))
		}

	}

	// Create new pair of refresh and access tokens
	newTokensDetails, err := s.AuthService.CreateToken(ctx, tokenUser)
	if err != nil {
		return nil, err
	}

	// Save the tokens to ephemeralstore
	err = s.EphemeralStore.CreateAuth(ctx, tokenUser.GetUserId(), newTokensDetails)
	if err != nil {
		log.Error("ephemeral-store-failed-after-successful-refresh-token-regeneration", zap.String("user-id:", tokenUser.GetUserId()), zap.Error(err))
		return nil, err
	}

	return &RefreshTokenResponse{
		AccessToken:           newTokensDetails.AccessToken,
		RefreshToken:          newTokensDetails.RefreshToken,
		AccessTokenExpiresAt:  newTokensDetails.AtExpires,
		RefreshTokenExpiresAt: newTokensDetails.RtExpires,
	}, nil
}

// RemoveAccessTokenWithCookieValue removes access token with the given cookie value
func (s *Service) RemoveAccessTokenWithCookieValue(ctx context.Context, userId, accessTokenCookieValue string) error {
	var log *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	log.Info("processing-access-token-removal-by-cookie-value")

	tempRequest := &http.Request{
		Header: http.Header{},
	}

	tempRequest.Header["Authorization"] = []string{"Bearer " + accessTokenCookieValue}

	accessTokenDetails, err := s.AuthService.ExtractTokenMetadata(ctx, tempRequest)
	if err != nil {
		log.Error("failed-to-extract-access-token-details-from-cookie-value", zap.Error(err))
		return err
	}

	deleted, err := s.EphemeralStore.DeleteAuth(ctx, toolbox.CombinedUuidFormat(userId, accessTokenDetails.AccessUUID))
	if err != nil || deleted == 0 {
		log.Warn("access-token-removal-failed", zap.String("user-id", userId), zap.String("access-token", accessTokenDetails.AccessUUID), zap.Error(err))
		return err
	}

	log.Info("access-token-successfully-removed", zap.String("user-id", userId), zap.String("refresh-token", accessTokenDetails.AccessUUID))

	return nil
}

// RemoveRefreshTokenWithCookieValue removes refresh token with the given cookie valu
// returns the user id of the refresh token and an error if any
func (s *Service) RemoveRefreshTokenWithCookieValue(ctx context.Context, refreshTokenCookieValue string) (auth.UserModel, string, error) {

	var (
		userId           string
		refreshTokenUuid string
		log              *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
			zap.AddStacktrace(zap.DPanicLevel),
		)
	)

	log.Info("processing-refresh-token-removal-by-cookie-value")

	// Check validity
	refreshToken, err := s.AuthService.CheckRefreshTokenIsValid(ctx, refreshTokenCookieValue)
	if err != nil {
		log.Error("failed-to-check-if-refresh-token-is-valid", zap.Error(err))
		return nil, refreshTokenUuid, err
	}

	// Get token details
	refreshTokenDetails, err := s.AuthService.GetRefreshTokenUUID(ctx, refreshToken)
	if err != nil {
		log.Error("failed-to-get-refresh-token-by-its-uuid", zap.Error(err))
		return nil, refreshTokenUuid, err
	}

	refreshTokenUuid = refreshTokenDetails.RefreshUUID

	// Get user details
	persistentUserResponse, err := s.UserService.GetUserByID(ctx, &user.GetUserByIdRequest{
		Id: refreshTokenDetails.UserID})
	if err != nil {
		log.Error("unable-to-find-user-for-refresh-token-by-its-provided-user-uuid", zap.Error(err))
		return nil, refreshTokenUuid, err
	}

	userId = persistentUserResponse.User.ID

	// Delete previous refresh token matching key (<userID>:<tokenUUID>)
	deleted, err := s.EphemeralStore.DeleteAuth(ctx, toolbox.CombinedUuidFormat(userId, refreshTokenDetails.RefreshUUID))
	if err != nil || deleted == 0 {
		log.Error("ephemeral-delete-failed-after-successful-refresh-token-validation", zap.String("user-id:", userId), zap.Error(err))
		return nil, refreshTokenUuid, errors.New(ErrKeyUnauthorizedRefreshTokenCacheDeletionFailure)
	}

	log.Info("refresh-token-successfully-removed", zap.String("user-id", userId), zap.String("refresh-token", refreshTokenDetails.RefreshUUID))
	return &persistentUserResponse.User, refreshTokenUuid, nil
}

// LoginUser handies verifying initial login token token, and  actioning all surrounding steps in
// login flow
// TODO: Create tests
func (s *Service) LoginUser(ctx context.Context, r *LoginUserRequest) (*LoginUserResponse, error) {

	var log *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	initiateLoginTokenDetails, err := s.TokenAsStringValidator(ctx, &TokenAsStringValidatorRequest{
		Token: r.Token})
	if err != nil {
		return nil, err
	}

	// Check if ID returns valid user
	gIDResponse, err := s.UserService.GetUserByID(ctx, &user.GetUserByIdRequest{
		Id: initiateLoginTokenDetails.UserID,
	})
	if err != nil {
		return nil, err
	}

	persistentUser := gIDResponse.User

	tokenDetails, err := s.AuthService.CreateToken(ctx, &persistentUser)
	if err != nil {
		return nil, err
	}

	// update users logged in time
	persistentUser.SetLastLoginAtTimeToNow().SetLastFreshLoginAtTimeToNow()

	UpdateUserResponse, err := s.UserService.UpdateUser(ctx, &user.UpdateUserRequest{User: &persistentUser})
	if err != nil {
		log.Error("system-update-failed-after-successful-login-initiation", zap.String("user-id:", persistentUser.ID))
		return nil, err
	}

	err = s.EphemeralStore.CreateAuth(ctx, UpdateUserResponse.User.ID, tokenDetails)
	if err != nil {
		log.Error("ephemeral-store-failed-after-successful-login-initiation", zap.String("user-id:", persistentUser.ID))
		return nil, err
	}

	// Invalidate initiate login token
	_, _ = s.DeleteAuth(ctx, toolbox.CombinedUuidFormat(UpdateUserResponse.User.ID, initiateLoginTokenDetails.TokenID))

	auditEvent := audit.UserLogin
	auditErr := s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{
		ActorId:    audit.AuditActorIdSystem,
		Action:     auditEvent,
		TargetId:   UpdateUserResponse.User.ID,
		TargetType: audit.User,
		Domain:     "accessmanager",
	})

	if auditErr != nil {
		log.Warn("failed-to-log-event", zap.String("actor-id", audit.AuditActorIdSystem), zap.String("user-id", UpdateUserResponse.User.ID), zap.String("event-type", string(auditEvent)))
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
	return s.EphemeralStore.DeleteAuth(ctx, tokenID)
}

// CreateInitalLoginOrVerificationTokenEmail handles sending user specific emails (intial login / verification) dependent on user's
// account status
// TODO: Create tests
// TODO: Add logic to send email when dashboard access is attempted by non
// admin user
func (s *Service) CreateInitalLoginOrVerificationTokenEmail(ctx context.Context, r *CreateInitalLoginOrVerificationTokenEmailRequest) error {

	var log *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	persistentUserResponse, err := s.UserService.GetUserByEmail(ctx, &user.GetUserByEmailRequest{Email: r.Email})
	if err != nil {
		return err
	}

	switch persistentUserResponse.User.Status {
	case user.AccountStatusKeyActive:
		_, err = s.CreateInitalLoginToken(ctx, &persistentUserResponse.User, r.Dashboard, r.RequestUrl)
		if err != nil {
			return err
		}
	case user.AccountStatusKeyProvisioned:
		_, err = s.CreateEmailVerificationToken(ctx, &CreateEmailVerificationTokenRequest{
			User:               persistentUserResponse.User,
			IsDashboardRequest: r.Dashboard,
			RequestUrl:         r.RequestUrl,
		})
		if err != nil {
			return err
		}
	default:
		// TODO: Send custom email with actions
		log.Error("requested-user-in-unexpected-state", zap.String("user-id", persistentUserResponse.User.ID))
		return errors.New(ErrKeyUserStatusUncaught)
	}

	return nil
}

// ValidateEmailVerificationCode handles updating the system to illustrate a successful email verification
// TODO: Create tests
func (s *Service) ValidateEmailVerificationCode(ctx context.Context, r *ValidateEmailVerificationCodeRequest) (*ValidateEmailVerificationCodeResponse, error) {

	verifiedTokenDetails, err := s.TokenAsStringValidator(ctx, &TokenAsStringValidatorRequest{
		Token: r.Token})
	if err != nil {
		return nil, err
	}

	accessToken, accessTokenExpiresAt, refreshToken, refreshTokenExpiresAt, err := s.UserEmailVerificationRevisions(ctx, &UserEmailVerificationRevisionsRequest{
		UserID: verifiedTokenDetails.UserID})
	if err != nil {
		return nil, err
	}

	// Invalidate ephemeral token (one time click)
	_, _ = s.DeleteAuth(ctx, verifiedTokenDetails.TokenID)

	return &ValidateEmailVerificationCodeResponse{
		AccessToken:           accessToken,
		AccessTokenExpiresAt:  accessTokenExpiresAt,
		RefreshToken:          refreshToken,
		RefreshTokenExpiresAt: refreshTokenExpiresAt,
	}, nil

}

// UserEmailVerificationRevisions handles updating the system to illustrate a successful email verification
// TODO: Create tests
func (s *Service) UserEmailVerificationRevisions(ctx context.Context, r *UserEmailVerificationRevisionsRequest) (accessToken string, accessTokenExpiresAt int64, refreshToken string, refreshTokenExpiresAt int64, err error) {

	var log *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	persistentUserResponse, err := s.UserService.GetUserByID(ctx, &user.GetUserByIdRequest{Id: r.UserID})
	if err != nil {
		return "", 0, "", 0, err
	}

	persistentUser := persistentUserResponse.User

	// Update user's verificaiton data, metadata and state
	revisionedUser, err := persistentUser.SetLastLoginAtTimeToNow().VerifyEmailNow().UpdateStatus(user.AccountStatusKeyActive)
	if err != nil {
		log.Error("user-status-update-failed-after-successful-email-verification", zap.String("user-id:", r.UserID))
		return "", 0, "", 0, err
	}

	UpdateUserResponse, err := s.UserService.UpdateUser(ctx, &user.UpdateUserRequest{User: revisionedUser})
	if err != nil {
		log.Error("system-update-failed-after-successful-email-verification", zap.String("user-id:", r.UserID))
		return "", 0, "", 0, err
	}

	newTokenDetails, err := s.AuthService.CreateToken(ctx, &UpdateUserResponse.User)
	if err != nil {
		log.Error("token-creation-failed-after-successful-email-verification", zap.String("user-id:", r.UserID))
		return "", 0, "", 0, err
	}

	err = s.EphemeralStore.CreateAuth(ctx, r.UserID, newTokenDetails)
	if err != nil {
		log.Error("ephemeral-store-failed-after-successful-email-verification", zap.String("user-id:", r.UserID))
		return "", 0, "", 0, err
	}

	return newTokenDetails.AccessToken, newTokenDetails.AtExpires, newTokenDetails.RefreshToken, newTokenDetails.RtExpires, nil

}

// TokenAsStringValidator actions the validation process on tokens that aren't passed through the
// conventional method (headers)
// TODO: Create tests
func (s *Service) TokenAsStringValidator(ctx context.Context, r *TokenAsStringValidatorRequest) (*TokenAsStringValidatorResponse, error) {
	var log *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	token, err := s.AuthService.ParseAccessTokenFromString(ctx, r.Token)
	if err != nil {
		return nil, err
	}

	td, err := s.AuthService.CheckAccessTokenValidityGetDetails(ctx, token)
	if err != nil {
		return nil, err
	}

	// Check to make sure ephemeral token in persistent storage
	_, err = s.EphemeralStore.FetchAuth(ctx, td)
	if err != nil {
		log.Warn("unauthorized-token-not-found", zap.String("user-id", td.UserID))
		return nil, errors.New(ErrKeyUnauthorizedTokenNotFoundInStore)
	}

	return &TokenAsStringValidatorResponse{
		UserID:  td.UserID,
		TokenID: td.AccessUUID,
	}, nil

}

// CreateUser creates a new user based on the passed request if possible, and sends verification
// email otherwise errors.
// TODO: Create tests
func (s *Service) CreateUser(ctx context.Context, r *CreateUserRequest) (*CreateUserResponse, error) {

	var log *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	response := &CreateUserResponse{}

	newUser, err := s.UserService.CreateUser(ctx, &user.CreateUserRequest{
		FirstName: r.FirstName,
		LastName:  r.LastName,
		Email:     r.Email,
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
		log.Warn("failed-to-log-event", zap.String("actor-id", audit.AuditActorIdSystem), zap.String("user-id", response.User.ID), zap.String("event-type", string(auditEvent)))
	}

	// handle associating pre-registered subscriptions if any
	if s.BillingService != nil {
		log.Info("checking-for-pre-registered-subscriptions", zap.String("user-id", newUser.User.ID), zap.String("user-email", newUser.User.Email))
		unassociatedSubResp, err := s.BillingService.GetUnassociatedSubscriptions(ctx, &billing.GetUnassociatedSubscriptionsRequest{
			Email: newUser.User.Email,
			Limit: 50,
		})
		if err != nil {
			log.Error("failed-to-check-for-pre-registered-subscriptions", zap.String("user-id", newUser.User.ID), zap.Error(err))
		} else {
			if len(unassociatedSubResp.Subscriptions) > 0 {
				log.Info("found-pre-registered-subscriptions", zap.String("user-id", newUser.User.ID), zap.Int("subscription-count", len(unassociatedSubResp.Subscriptions)))
				_, subscriptionAssociationErr := s.BillingService.AssociateSubscriptionsWithUser(ctx, &billing.AssociateSubscriptionsWithUserRequest{
					UserID: newUser.User.ID,
					Email:  newUser.User.Email,
				})
				if subscriptionAssociationErr != nil {
					log.Error("failed-to-associate-pre-registered-subscriptions", zap.String("user-id", newUser.User.ID), zap.Error(subscriptionAssociationErr))
				} else {
					log.Info("successfully-associated-pre-registered-subscriptions", zap.String("user-id", newUser.User.ID), zap.Int("subscription-count", len(unassociatedSubResp.Subscriptions)))
				}
			} else {
				log.Info("no-pre-registered-subscriptions-found", zap.String("user-id", newUser.User.ID))
			}
		}

		log.Info("checking-for-pre-registered-billing-events", zap.String("user-id", newUser.User.ID), zap.String("user-email", newUser.User.Email))
		unassociatedBillingEventsResp, err := s.BillingService.GetUnassociatedBillingEvents(ctx, &billing.GetUnassociatedBillingEventsRequest{
			Email: newUser.User.Email,
			Limit: 100,
		})
		if err != nil {
			log.Error("failed-to-check-for-pre-registered-billing-events", zap.String("user-id", newUser.User.ID), zap.Error(err))
		} else {
			if len(unassociatedBillingEventsResp.BillingEvents) > 0 {
				log.Info("found-pre-registered-billing-events", zap.String("user-id", newUser.User.ID), zap.Int("billing-event-count", len(unassociatedBillingEventsResp.BillingEvents)))
				_, billingEventAssociationErr := s.BillingService.AssociateBillingEventsWithUser(ctx, &billing.AssociateBillingEventsWithUserRequest{
					UserID: newUser.User.ID,
					Email:  newUser.User.Email,
				})
				if billingEventAssociationErr != nil {
					log.Error("failed-to-associate-pre-registered-billing-events", zap.String("user-id", newUser.User.ID), zap.Error(billingEventAssociationErr))
				} else {
					log.Info("successfully-associated-pre-registered-billing-events", zap.String("user-id", newUser.User.ID), zap.Int("billing-event-count", len(unassociatedBillingEventsResp.BillingEvents)))
				}
			} else {
				log.Info("no-pre-registered-billing-events-found", zap.String("user-id", newUser.User.ID))
			}
		}
	}

	// handle verification email if not disabled
	if !r.DisableVerificationEmail {
		log.Info(fmt.Sprintf("ams/initiate-new-user-verification-email: %s", newUser.User.ID))
		_, err = s.CreateEmailVerificationToken(ctx, &CreateEmailVerificationTokenRequest{
			User:       response.User,
			RequestUrl: r.RequestUrl,
		})
		if err != nil {
			log.Error(fmt.Sprintf("ams/error-failed-to-initiate-new-user-verification-email: %s", newUser.User.ID))
			return response, err
		}

		log.Info(fmt.Sprintf("ams/successfully-initiated-new-user-verification-email: %s", newUser.User.ID))
	}

	log.Info(fmt.Sprintf("ams/completed-new-user-creation: %s", newUser.User.ID))

	return response, nil
}

// CreateInitalLoginToken creates token used to initiate login flow for user passed
// TODO: Create tests
func (s *Service) CreateInitalLoginToken(ctx context.Context, user *user.User, isDashboardRequest bool, requestUrl string) (string, error) {

	var log *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	tokenDetails, err := s.AuthService.CreateInitalToken(ctx, user)
	if err != nil {
		log.Error("unable-to-generate-initiate-login-email-token:", zap.String("user-id", user.ID))
		return "", err
	}

	err = s.EphemeralStore.StoreToken(ctx, tokenDetails.EphemeralUUID, user.ID, tokenDetails.EtTTL)
	if err != nil {
		log.Error("unable-to-store-token-in-ephemeral-store:", zap.String("user-id", user.ID))
		return "", err
	}

	// Beging email sending process
	err = s.EmailManager.SendLoginEmail(ctx, &emailmanager.SendLoginEmailRequest{
		Email:              user.Email,
		Token:              tokenDetails.EphemeralToken,
		IsDashboardRequest: isDashboardRequest,
		RequestUrl:         requestUrl,
		UserId:             user.ID,
	})
	if err != nil {
		log.Error("unable-to-send-initiate-login-email:", zap.String("user-id", user.ID))
		return "", err
	}

	return tokenDetails.EphemeralToken, nil
}

// CreateEmailVerificationToken create token used to validate email associated to user's accounts
func (s *Service) CreateEmailVerificationToken(ctx context.Context, r *CreateEmailVerificationTokenRequest) (string, error) {

	var log *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)
	user := &r.User

	tokenDetails, err := s.AuthService.CreateEmailVerificationToken(ctx, user)
	if err != nil {
		log.Error("unable-to-generate-verification-email-token:", zap.String("user-id", user.ID))
		return "", err
	}

	err = s.EphemeralStore.StoreToken(ctx, tokenDetails.EmailVerificationUUID, user.ID, tokenDetails.EvTTL)
	if err != nil {
		log.Error("unable-to-store-token-in-ephemeral-store:", zap.String("user-id", user.ID))
		return "", err
	}

	// Beging email sending process
	err = s.EmailManager.SendVerificationEmail(ctx, &emailmanager.SendVerificationEmailRequest{
		FirstName:          user.FirstName,
		LastName:           user.LastName,
		Email:              user.Email,
		Token:              tokenDetails.EmailVerificationToken,
		IsDashboardRequest: r.IsDashboardRequest,
		RequestUrl:         r.RequestUrl,
		UserId:             user.ID,
	})
	if err != nil {
		log.Error("unable-to-send-verification-email:", zap.String("user-id", user.ID))
		return "", err
	}

	return tokenDetails.EmailVerificationToken, nil
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

// isUserLiveStatusActive returns whether user matching passed user ID
// has an `ACTIVE` user status
func (s *Service) isUserLiveStatusActive(ctx context.Context, userID string) bool {
	var log *zap.Logger = logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	persistentUserResponse, err := s.UserService.GetUserByID(ctx,
		&user.GetUserByIdRequest{Id: userID},
	)
	if err != nil {
		log.Warn("live-status-check-failure", zap.String("user-id", userID), zap.Error(err))
		return false
	}

	if persistentUserResponse.User.Status == user.AccountStatusKeyActive {
		return true
	}

	return false
}
