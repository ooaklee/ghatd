package accessmanager_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/emailmanager"
	"github.com/ooaklee/ghatd/external/ephemeral"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
)

// loginEmailEphemeralStoreMock records cooldown, token, and code behavior for login-email tests.
type loginEmailEphemeralStoreMock struct {
	refreshEphemeralStoreMock
	acquireLoginEmailCooldownFunc func(ctx context.Context, userID string, isDashboardRequest bool, requestURL string, ttl time.Duration) (bool, error)
	storeTokenFunc                func(ctx context.Context, accessTokenUUID string, userID string, ttl time.Duration) error
	storeCodeFunc                 func(ctx context.Context, code string, ttl time.Duration) error
	storeCodeMappingFunc          func(ctx context.Context, code, token string, ttl time.Duration) error
	releaseLoginEmailCooldowns    int
}

func (m *loginEmailEphemeralStoreMock) AcquireLoginEmailCooldown(ctx context.Context, userID string, isDashboardRequest bool, requestURL string, ttl time.Duration) (bool, error) {
	if m.acquireLoginEmailCooldownFunc != nil {
		return m.acquireLoginEmailCooldownFunc(ctx, userID, isDashboardRequest, requestURL, ttl)
	}
	return true, nil
}

func (m *loginEmailEphemeralStoreMock) ReleaseLoginEmailCooldown(ctx context.Context, userID string, isDashboardRequest bool, requestURL string) (int64, error) {
	m.releaseLoginEmailCooldowns++
	return 1, nil
}

func (m *loginEmailEphemeralStoreMock) StoreToken(ctx context.Context, accessTokenUUID string, userID string, ttl time.Duration) error {
	if m.storeTokenFunc != nil {
		return m.storeTokenFunc(ctx, accessTokenUUID, userID, ttl)
	}
	return nil
}

func (m *loginEmailEphemeralStoreMock) CodeExists(ctx context.Context, code string) (bool, error) {
	return false, nil
}

func (m *loginEmailEphemeralStoreMock) StoreCode(ctx context.Context, code string, ttl time.Duration) error {
	if m.storeCodeFunc != nil {
		return m.storeCodeFunc(ctx, code, ttl)
	}
	return nil
}

func (m *loginEmailEphemeralStoreMock) StoreCodeMapping(ctx context.Context, code, token string, ttl time.Duration) error {
	if m.storeCodeMappingFunc != nil {
		return m.storeCodeMappingFunc(ctx, code, token, ttl)
	}
	return nil
}

// loginEmailManagerMock captures SendLoginEmail behavior for login-email tests.
type loginEmailManagerMock struct {
	sendLoginEmailFunc func(ctx context.Context, req *emailmanager.SendLoginEmailRequest) error
}

type loginAuditServiceMock struct {
	logAuditEventFunc func(ctx context.Context, req *audit.LogAuditEventRequest) error
}

func (m *loginAuditServiceMock) LogAuditEvent(ctx context.Context, req *audit.LogAuditEventRequest) error {
	if m.logAuditEventFunc != nil {
		return m.logAuditEventFunc(ctx, req)
	}
	return nil
}

func (m *loginEmailManagerMock) SendCustomEmail(ctx context.Context, req *emailmanager.SendCustomEmailRequest) error {
	return nil
}

func (m *loginEmailManagerMock) SendLoginEmail(ctx context.Context, req *emailmanager.SendLoginEmailRequest) error {
	if m.sendLoginEmailFunc != nil {
		return m.sendLoginEmailFunc(ctx, req)
	}
	return nil
}

func (m *loginEmailManagerMock) SendVerificationEmail(ctx context.Context, req *emailmanager.SendVerificationEmailRequest) error {
	return nil
}

// newLoginEmailTestUser returns a fixed active user for login-email tests.
func newLoginEmailTestUser() *userv2.UniversalUser {
	return &userv2.UniversalUser{
		ID:     "user-1",
		Email:  "user@example.com",
		Status: userv2.AccountStatusKeyActive,
	}
}

// newInitialTokenDetails returns a deterministic initial login token for login-email tests.
func newInitialTokenDetails() *auth.TokenDetails {
	return &auth.TokenDetails{
		EphemeralToken: "initial-login-token",
		EphemeralUUID:  "initial-login-uuid",
		EtTTL:          time.Minute,
	}
}

// TestServiceCreateInitalLoginTokenSkipsDuplicateWithinCooldown verifies duplicate sends are suppressed while the cooldown is active.
func TestServiceCreateInitalLoginTokenSkipsDuplicateWithinCooldown(t *testing.T) {
	t.Parallel()

	createTokenCalls := 0
	sendEmailCalls := 0
	store := &loginEmailEphemeralStoreMock{
		acquireLoginEmailCooldownFunc: func(ctx context.Context, userID string, isDashboardRequest bool, requestURL string, ttl time.Duration) (bool, error) {
			require.Equal(t, "user-1", userID)
			require.False(t, isDashboardRequest)
			require.Equal(t, "/app", requestURL)
			require.Greater(t, ttl, time.Duration(0))
			return false, nil
		},
	}
	service := &accessmanager.Service{
		EphemeralStore: store,
		AuthService: &refreshAuthServiceMock{
			createInitalTokenFunc: func(ctx context.Context, user auth.UserModel) (*auth.TokenDetails, error) {
				createTokenCalls++
				return newInitialTokenDetails(), nil
			},
		},
		EmailManager: &loginEmailManagerMock{
			sendLoginEmailFunc: func(ctx context.Context, req *emailmanager.SendLoginEmailRequest) error {
				sendEmailCalls++
				return nil
			},
		},
	}

	token, err := service.CreateInitalLoginToken(context.Background(), newLoginEmailTestUser(), false, "/app")

	require.NoError(t, err)
	require.Empty(t, token)
	require.Zero(t, createTokenCalls)
	require.Zero(t, sendEmailCalls)
	require.Zero(t, store.releaseLoginEmailCooldowns)
}

// TestServiceCreateInitalLoginTokenSendsOnceWhenCooldownAcquired verifies the first accepted request creates and sends one token.
func TestServiceCreateInitalLoginTokenSendsOnceWhenCooldownAcquired(t *testing.T) {
	t.Parallel()

	var sentRequest *emailmanager.SendLoginEmailRequest
	storeTokenCalls := 0
	storeCodeMappingCalls := 0
	store := &loginEmailEphemeralStoreMock{
		storeTokenFunc: func(ctx context.Context, accessTokenUUID string, userID string, ttl time.Duration) error {
			storeTokenCalls++
			require.Equal(t, "initial-login-uuid", accessTokenUUID)
			require.Equal(t, "user-1", userID)
			return nil
		},
		storeCodeMappingFunc: func(ctx context.Context, code, token string, ttl time.Duration) error {
			storeCodeMappingCalls++
			require.Len(t, code, 8)
			require.Equal(t, "initial-login-token", token)
			return nil
		},
	}
	service := &accessmanager.Service{
		EphemeralStore: store,
		AuthService: &refreshAuthServiceMock{
			createInitalTokenFunc: func(ctx context.Context, user auth.UserModel) (*auth.TokenDetails, error) {
				return newInitialTokenDetails(), nil
			},
		},
		EmailManager: &loginEmailManagerMock{
			sendLoginEmailFunc: func(ctx context.Context, req *emailmanager.SendLoginEmailRequest) error {
				sentRequest = req
				return nil
			},
		},
	}

	token, err := service.CreateInitalLoginToken(context.Background(), newLoginEmailTestUser(), true, "/app/plan")

	require.NoError(t, err)
	require.Equal(t, "initial-login-token", token)
	require.Equal(t, 1, storeTokenCalls)
	require.Equal(t, 1, storeCodeMappingCalls)
	require.NotNil(t, sentRequest)
	require.Equal(t, "user@example.com", sentRequest.Email)
	require.Equal(t, "initial-login-token", sentRequest.Token)
	require.Len(t, sentRequest.Code, 8)
	require.True(t, sentRequest.IsDashboardRequest)
	require.Equal(t, "/app/plan", sentRequest.RequestUrl)
	require.Zero(t, store.releaseLoginEmailCooldowns)
}

// TestServiceCreateInitalLoginTokenReleasesCooldownWhenSendFails verifies failed sends allow a later retry.
func TestServiceCreateInitalLoginTokenReleasesCooldownWhenSendFails(t *testing.T) {
	t.Parallel()

	store := &loginEmailEphemeralStoreMock{}
	service := &accessmanager.Service{
		EphemeralStore: store,
		AuthService: &refreshAuthServiceMock{
			createInitalTokenFunc: func(ctx context.Context, user auth.UserModel) (*auth.TokenDetails, error) {
				return newInitialTokenDetails(), nil
			},
		},
		EmailManager: &loginEmailManagerMock{
			sendLoginEmailFunc: func(ctx context.Context, req *emailmanager.SendLoginEmailRequest) error {
				return errors.New("send failed")
			},
		},
	}

	token, err := service.CreateInitalLoginToken(context.Background(), newLoginEmailTestUser(), false, "/app")

	require.Error(t, err)
	require.Empty(t, token)
	require.Equal(t, 1, store.releaseLoginEmailCooldowns)
}

func TestServiceLoginUserCodeVerifiesAndActivatesProvisionedUser(t *testing.T) {
	t.Parallel()

	const (
		verificationCode  = "ABCD1234"
		verificationToken = "verification-token"
		verificationUUID  = "verification-uuid"
	)

	user := userv2.NewUserFactory(nil).CreateUser("user@example.com")
	user.ID = "user-1"

	var deletedTokenID string
	store := &refreshEphemeralStoreMock{
		getCodeMappingFunc: func(ctx context.Context, code string) (string, error) {
			require.Equal(t, verificationCode, code)
			return verificationToken, nil
		},
		fetchAuthFunc: func(ctx context.Context, details ephemeral.TokenDetailsAccess) (string, error) {
			require.Equal(t, "user-1", details.GetUserId())
			require.Equal(t, verificationUUID, details.GetTokenAccessUuid())
			return "user-1", nil
		},
		createAuthFunc: func(ctx context.Context, userID string, tokenDetails ephemeral.TokenDetailsAuth) error {
			require.Equal(t, "user-1", userID)
			require.Equal(t, "session-access-uuid", tokenDetails.GetTokenAccessUuid())
			return nil
		},
		deleteAuthFunc: func(ctx context.Context, tokenID string) (int64, error) {
			deletedTokenID = tokenID
			return 1, nil
		},
	}

	authService := &refreshAuthServiceMock{
		parseAccessTokenFromStringFunc: func(ctx context.Context, tokenAsString string) (*jwt.Token, error) {
			require.Equal(t, verificationToken, tokenAsString)
			return &jwt.Token{Valid: true}, nil
		},
		checkAccessTokenValidityGetDetails: func(ctx context.Context, token *jwt.Token) (*auth.TokenAccessDetails, error) {
			return &auth.TokenAccessDetails{UserID: "user-1", AccessUUID: verificationUUID}, nil
		},
		createTokenFunc: func(ctx context.Context, tokenUser auth.UserModel) (*auth.TokenDetails, error) {
			require.Equal(t, userv2.AccountStatusKeyActive, tokenUser.GetUserStatus())
			require.True(t, user.Verification.EmailVerified)
			return &auth.TokenDetails{
				AccessToken:  "session-access-token",
				AccessUUID:   "session-access-uuid",
				RefreshToken: "session-refresh-token",
				AtExpires:    1700000000,
				RtExpires:    1700003600,
			}, nil
		},
	}

	updateCalls := 0
	userService := &refreshUserServiceMock{
		user: user,
		updateUserFunc: func(ctx context.Context, req *userv2.UpdateUserRequest) (*userv2.UpdateUserResponse, error) {
			updateCalls++
			require.Equal(t, userv2.AccountStatusKeyActive, req.User.Status)
			require.True(t, req.User.Verification.EmailVerified)
			return &userv2.UpdateUserResponse{User: req.User}, nil
		},
	}

	auditCalls := 0
	service := &accessmanager.Service{
		EphemeralStore: store,
		AuthService:    authService,
		UserService:    userService,
		AuditService: &loginAuditServiceMock{
			logAuditEventFunc: func(ctx context.Context, req *audit.LogAuditEventRequest) error {
				auditCalls++
				require.Equal(t, "user-1", req.TargetId)
				require.Equal(t, audit.UserLogin, req.Action)
				return nil
			},
		},
	}

	response, err := service.LoginUser(context.Background(), &accessmanager.LoginUserRequest{Code: verificationCode})

	require.NoError(t, err)
	require.Equal(t, "session-access-token", response.AccessToken)
	require.Equal(t, "session-refresh-token", response.RefreshToken)
	require.Equal(t, userv2.AccountStatusKeyActive, user.Status)
	require.True(t, user.Verification.EmailVerified)
	require.Equal(t, 1, updateCalls)
	require.Equal(t, 1, auditCalls)
	require.Equal(t, "user-1:"+verificationUUID, deletedTokenID)
}

func TestServiceLoginUserKeepsActiveUserOnOrdinaryLoginPath(t *testing.T) {
	t.Parallel()

	const (
		loginToken = "login-token"
		loginUUID  = "login-uuid"
	)

	user := userv2.NewUserFactory(nil).CreateUser("user@example.com")
	user.ID = "user-1"
	user.Status = userv2.AccountStatusKeyActive
	user.Verification.EmailVerified = true

	var deletedTokenID string
	store := &refreshEphemeralStoreMock{
		fetchAuthFunc: func(ctx context.Context, details ephemeral.TokenDetailsAccess) (string, error) {
			require.Equal(t, "user-1", details.GetUserId())
			require.Equal(t, loginUUID, details.GetTokenAccessUuid())
			return "user-1", nil
		},
		createAuthFunc: func(ctx context.Context, userID string, tokenDetails ephemeral.TokenDetailsAuth) error {
			require.Equal(t, "user-1", userID)
			require.Equal(t, "session-access-uuid", tokenDetails.GetTokenAccessUuid())
			return nil
		},
		deleteAuthFunc: func(ctx context.Context, tokenID string) (int64, error) {
			deletedTokenID = tokenID
			return 1, nil
		},
	}

	authService := &refreshAuthServiceMock{
		parseAccessTokenFromStringFunc: func(ctx context.Context, tokenAsString string) (*jwt.Token, error) {
			require.Equal(t, loginToken, tokenAsString)
			return &jwt.Token{Valid: true}, nil
		},
		checkAccessTokenValidityGetDetails: func(ctx context.Context, token *jwt.Token) (*auth.TokenAccessDetails, error) {
			return &auth.TokenAccessDetails{UserID: "user-1", AccessUUID: loginUUID}, nil
		},
		createTokenFunc: func(ctx context.Context, tokenUser auth.UserModel) (*auth.TokenDetails, error) {
			require.Equal(t, userv2.AccountStatusKeyActive, tokenUser.GetUserStatus())
			return &auth.TokenDetails{
				AccessToken:  "session-access-token",
				AccessUUID:   "session-access-uuid",
				RefreshToken: "session-refresh-token",
				AtExpires:    1700000000,
				RtExpires:    1700003600,
			}, nil
		},
	}

	updateCalls := 0
	service := &accessmanager.Service{
		EphemeralStore: store,
		AuthService:    authService,
		UserService: &refreshUserServiceMock{
			user: user,
			updateUserFunc: func(ctx context.Context, req *userv2.UpdateUserRequest) (*userv2.UpdateUserResponse, error) {
				updateCalls++
				require.Equal(t, userv2.AccountStatusKeyActive, req.User.Status)
				require.True(t, req.User.Verification.EmailVerified)
				return &userv2.UpdateUserResponse{User: req.User}, nil
			},
		},
		AuditService: &loginAuditServiceMock{},
	}

	response, err := service.LoginUser(context.Background(), &accessmanager.LoginUserRequest{Token: loginToken})

	require.NoError(t, err)
	require.Equal(t, "session-access-token", response.AccessToken)
	require.Equal(t, "session-refresh-token", response.RefreshToken)
	require.Equal(t, userv2.AccountStatusKeyActive, user.Status)
	require.True(t, user.Verification.EmailVerified)
	require.Equal(t, 1, updateCalls)
	require.Equal(t, "user-1:"+loginUUID, deletedTokenID)
}

func TestServiceLoginUserRejectsNonLiveAccountStatuses(t *testing.T) {
	t.Parallel()

	for _, status := range []string{
		userv2.AccountStatusKeyDeactivated,
		userv2.AccountStatusKeyLockedOut,
		userv2.AccountStatusKeyRecovery,
		userv2.AccountStatusKeySuspended,
	} {
		status := status
		t.Run(status, func(t *testing.T) {
			t.Parallel()

			user := userv2.NewUserFactory(nil).CreateUser("user@example.com")
			user.ID = "user-1"
			user.Status = status

			createTokenCalls := 0
			updateUserCalls := 0
			service := &accessmanager.Service{
				EphemeralStore: &refreshEphemeralStoreMock{
					fetchAuthFunc: func(ctx context.Context, details ephemeral.TokenDetailsAccess) (string, error) {
						return "user-1", nil
					},
				},
				AuthService: &refreshAuthServiceMock{
					parseAccessTokenFromStringFunc: func(ctx context.Context, tokenAsString string) (*jwt.Token, error) {
						return &jwt.Token{Valid: true}, nil
					},
					checkAccessTokenValidityGetDetails: func(ctx context.Context, token *jwt.Token) (*auth.TokenAccessDetails, error) {
						return &auth.TokenAccessDetails{UserID: "user-1", AccessUUID: "one-time-uuid"}, nil
					},
					createTokenFunc: func(ctx context.Context, tokenUser auth.UserModel) (*auth.TokenDetails, error) {
						createTokenCalls++
						return &auth.TokenDetails{}, nil
					},
				},
				UserService: &refreshUserServiceMock{
					user: user,
					updateUserFunc: func(ctx context.Context, req *userv2.UpdateUserRequest) (*userv2.UpdateUserResponse, error) {
						updateUserCalls++
						return &userv2.UpdateUserResponse{User: req.User}, nil
					},
				},
			}

			response, err := service.LoginUser(context.Background(), &accessmanager.LoginUserRequest{Token: "one-time-token"})

			require.ErrorIs(t, err, accessmanager.ErrUnauthorizedNonActiveStatus)
			require.Nil(t, response)
			require.Equal(t, status, user.Status)
			require.False(t, user.Verification.EmailVerified)
			require.Zero(t, createTokenCalls)
			require.Zero(t, updateUserCalls)
		})
	}
}

func TestServiceUserEmailVerificationRevisionsRejectsNonProvisionedUser(t *testing.T) {
	t.Parallel()

	user := userv2.NewUserFactory(nil).CreateUser("user@example.com")
	user.ID = "user-1"
	user.Status = userv2.AccountStatusKeyDeactivated

	updateUserCalls := 0
	service := &accessmanager.Service{
		UserService: &refreshUserServiceMock{
			user: user,
			updateUserFunc: func(ctx context.Context, req *userv2.UpdateUserRequest) (*userv2.UpdateUserResponse, error) {
				updateUserCalls++
				return &userv2.UpdateUserResponse{User: req.User}, nil
			},
		},
	}

	_, _, _, _, err := service.UserEmailVerificationRevisions(context.Background(), &accessmanager.UserEmailVerificationRevisionsRequest{UserID: "user-1"})

	require.ErrorIs(t, err, accessmanager.ErrUserStatusUncaught)
	require.Equal(t, userv2.AccountStatusKeyDeactivated, user.Status)
	require.False(t, user.Verification.EmailVerified)
	require.Zero(t, updateUserCalls)
}

// Compile-time guards keep the login-email mocks aligned with service dependencies.
var _ accessmanager.EphemeralStore = (*loginEmailEphemeralStoreMock)(nil)
var _ accessmanager.EmailManager = (*loginEmailManagerMock)(nil)
var _ accessmanager.AuditService = (*loginAuditServiceMock)(nil)
