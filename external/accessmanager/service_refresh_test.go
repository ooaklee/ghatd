package accessmanager_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/ephemeral"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
)

// refreshEphemeralStoreMock records and controls ephemeral storage behavior for refresh tests.
type refreshEphemeralStoreMock struct {
	createAuthFunc                       func(ctx context.Context, userID string, tokenDetails ephemeral.TokenDetailsAuth) error
	deleteAuthFunc                       func(ctx context.Context, tokenID string) (int64, error)
	acquireRefreshTokenRotationLockFunc  func(ctx context.Context, userID, refreshTokenUUID string, ttl time.Duration) (bool, error)
	releaseRefreshTokenRotationLockFunc  func(ctx context.Context, userID, refreshTokenUUID string) (int64, error)
	storeRefreshTokenRotationResultFunc  func(ctx context.Context, userID, refreshTokenUUID string, result *ephemeral.RefreshTokenRotationResult, ttl time.Duration) error
	getRefreshTokenRotationResultFunc    func(ctx context.Context, userID, refreshTokenUUID string) (*ephemeral.RefreshTokenRotationResult, error)
	deleteAuthCalls                      int
	acquireRefreshTokenRotationLockCalls int
	releaseRefreshTokenRotationLockCalls int
}

func (m *refreshEphemeralStoreMock) CreateAuth(ctx context.Context, userID string, tokenDetails ephemeral.TokenDetailsAuth) error {
	if m.createAuthFunc != nil {
		return m.createAuthFunc(ctx, userID, tokenDetails)
	}
	return nil
}

func (m *refreshEphemeralStoreMock) StoreToken(ctx context.Context, accessTokenUUID string, userID string, ttl time.Duration) error {
	return nil
}

func (m *refreshEphemeralStoreMock) FetchAuth(ctx context.Context, accessDetails ephemeral.TokenDetailsAccess) (string, error) {
	return "", nil
}

func (m *refreshEphemeralStoreMock) DeleteAuth(ctx context.Context, tokenID string) (int64, error) {
	m.deleteAuthCalls++
	if m.deleteAuthFunc != nil {
		return m.deleteAuthFunc(ctx, tokenID)
	}
	return 0, nil
}

func (m *refreshEphemeralStoreMock) AcquireRefreshTokenRotationLock(ctx context.Context, userID, refreshTokenUUID string, ttl time.Duration) (bool, error) {
	m.acquireRefreshTokenRotationLockCalls++
	if m.acquireRefreshTokenRotationLockFunc != nil {
		return m.acquireRefreshTokenRotationLockFunc(ctx, userID, refreshTokenUUID, ttl)
	}
	return true, nil
}

func (m *refreshEphemeralStoreMock) ReleaseRefreshTokenRotationLock(ctx context.Context, userID, refreshTokenUUID string) (int64, error) {
	m.releaseRefreshTokenRotationLockCalls++
	if m.releaseRefreshTokenRotationLockFunc != nil {
		return m.releaseRefreshTokenRotationLockFunc(ctx, userID, refreshTokenUUID)
	}
	return 1, nil
}

func (m *refreshEphemeralStoreMock) StoreRefreshTokenRotationResult(ctx context.Context, userID, refreshTokenUUID string, result *ephemeral.RefreshTokenRotationResult, ttl time.Duration) error {
	if m.storeRefreshTokenRotationResultFunc != nil {
		return m.storeRefreshTokenRotationResultFunc(ctx, userID, refreshTokenUUID, result, ttl)
	}
	return nil
}

func (m *refreshEphemeralStoreMock) GetRefreshTokenRotationResult(ctx context.Context, userID, refreshTokenUUID string) (*ephemeral.RefreshTokenRotationResult, error) {
	if m.getRefreshTokenRotationResultFunc != nil {
		return m.getRefreshTokenRotationResultFunc(ctx, userID, refreshTokenUUID)
	}
	return nil, nil
}

func (m *refreshEphemeralStoreMock) AcquireLoginEmailCooldown(ctx context.Context, userID string, isDashboardRequest bool, requestURL string, ttl time.Duration) (bool, error) {
	return true, nil
}

func (m *refreshEphemeralStoreMock) ReleaseLoginEmailCooldown(ctx context.Context, userID string, isDashboardRequest bool, requestURL string) (int64, error) {
	return 1, nil
}

func (m *refreshEphemeralStoreMock) AddRequestCountEntry(ctx context.Context, clientIp string) error {
	return nil
}

func (m *refreshEphemeralStoreMock) DeleteAllTokenExceptedSpecified(ctx context.Context, userId string, exemptionTokenIds []string) error {
	return nil
}

func (m *refreshEphemeralStoreMock) CodeExists(ctx context.Context, code string) (bool, error) {
	return false, nil
}

func (m *refreshEphemeralStoreMock) StoreCode(ctx context.Context, code string, ttl time.Duration) error {
	return nil
}

func (m *refreshEphemeralStoreMock) StoreCodeMapping(ctx context.Context, code, token string, ttl time.Duration) error {
	return nil
}

func (m *refreshEphemeralStoreMock) GetCodeMapping(ctx context.Context, code string) (string, error) {
	return "", nil
}

// refreshAuthServiceMock supplies controllable auth-token behavior for refresh tests.
type refreshAuthServiceMock struct {
	createInitalTokenFunc func(ctx context.Context, user auth.UserModel) (*auth.TokenDetails, error)
	createTokenFunc       func(ctx context.Context, user auth.UserModel) (*auth.TokenDetails, error)
}

func (m *refreshAuthServiceMock) CreateInitalToken(ctx context.Context, user auth.UserModel) (*auth.TokenDetails, error) {
	if m.createInitalTokenFunc != nil {
		return m.createInitalTokenFunc(ctx, user)
	}
	return nil, nil
}

func (m *refreshAuthServiceMock) CreateToken(ctx context.Context, user auth.UserModel) (*auth.TokenDetails, error) {
	if m.createTokenFunc != nil {
		return m.createTokenFunc(ctx, user)
	}
	return nil, nil
}

func (m *refreshAuthServiceMock) ExtractTokenMetadata(ctx context.Context, r *http.Request) (*auth.TokenAccessDetails, error) {
	return nil, nil
}

func (m *refreshAuthServiceMock) CheckRefreshTokenIsValid(ctx context.Context, t string) (*jwt.Token, error) {
	return &jwt.Token{Valid: true}, nil
}

func (m *refreshAuthServiceMock) GetRefreshTokenUUID(ctx context.Context, token *jwt.Token) (*auth.TokenRefreshDetails, error) {
	return &auth.TokenRefreshDetails{
		RefreshUUID: "old-refresh-uuid",
		UserID:      "user-1",
	}, nil
}

func (m *refreshAuthServiceMock) CheckAccessTokenValidityGetDetails(ctx context.Context, token *jwt.Token) (*auth.TokenAccessDetails, error) {
	return nil, nil
}

func (m *refreshAuthServiceMock) ParseAccessTokenFromString(ctx context.Context, tokenAsString string) (*jwt.Token, error) {
	return nil, nil
}

func (m *refreshAuthServiceMock) CreateEmailVerificationToken(ctx context.Context, user auth.UserModel) (*auth.TokenDetails, error) {
	return nil, nil
}

func (m *refreshAuthServiceMock) ExtractRefreshTokenMetadataByString(ctx context.Context, tokenAsString string) (*auth.TokenRefreshDetails, error) {
	return nil, nil
}

func (m *refreshAuthServiceMock) ExtractAccessTokenMetadataByString(ctx context.Context, tokenAsString string) (*auth.TokenAccessDetails, error) {
	return nil, nil
}

// refreshUserServiceMock returns a fixed user for refresh-service tests.
type refreshUserServiceMock struct {
	user *userv2.UniversalUser
}

func (m *refreshUserServiceMock) GetUserByNanoID(ctx context.Context, r *userv2.GetUserByNanoIDRequest) (*userv2.GetUserByNanoIDResponse, error) {
	return nil, nil
}

func (m *refreshUserServiceMock) GetUserByID(ctx context.Context, r *userv2.GetUserByIDRequest) (*userv2.GetUserByIDResponse, error) {
	return &userv2.GetUserByIDResponse{User: m.user}, nil
}

func (m *refreshUserServiceMock) GetUserByEmail(ctx context.Context, r *userv2.GetUserByEmailRequest) (*userv2.GetUserByEmailResponse, error) {
	return nil, nil
}

func (m *refreshUserServiceMock) UpdateUser(ctx context.Context, r *userv2.UpdateUserRequest) (*userv2.UpdateUserResponse, error) {
	return nil, nil
}

func (m *refreshUserServiceMock) CreateUser(ctx context.Context, r *userv2.CreateUserRequest) (*userv2.CreateUserResponse, error) {
	return nil, nil
}

// newRefreshTokenTestService wires the minimal service dependencies used by refresh tests.
func newRefreshTokenTestService(store *refreshEphemeralStoreMock, authService *refreshAuthServiceMock) *accessmanager.Service {
	return &accessmanager.Service{
		EphemeralStore: store,
		AuthService:    authService,
		UserService: &refreshUserServiceMock{user: &userv2.UniversalUser{
			ID:     "user-1",
			Email:  "user@example.com",
			Status: userv2.AccountStatusKeyActive,
		}},
	}
}

// TestServiceRemoveAccessTokenWithCookieValueDoesNotRequireHTTPRequestURL verifies
// access-token cookie cleanup does not depend on a real request URL.
func TestServiceRemoveAccessTokenWithCookieValueDoesNotRequireHTTPRequestURL(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	authService := auth.NewService(&auth.NewServiceRequest{
		AccessTokenSecret:  "access-secret",
		RefreshTokenSecret: "refresh-secret",
	})
	tokenDetails, err := authService.CreateToken(ctx, &userv2.UniversalUser{
		ID:     "user-1",
		Status: userv2.AccountStatusKeyActive,
	})
	require.NoError(t, err)

	store := &refreshEphemeralStoreMock{
		deleteAuthFunc: func(ctx context.Context, tokenID string) (int64, error) {
			require.Contains(t, tokenID, "user-1:")
			return 1, nil
		},
	}
	service := &accessmanager.Service{
		EphemeralStore: store,
		AuthService:    authService,
	}

	require.NotPanics(t, func() {
		err = service.RemoveAccessTokenWithCookieValue(ctx, "user-1", tokenDetails.AccessToken)
	})
	require.NoError(t, err)
	require.Equal(t, 1, store.deleteAuthCalls)
}

// TestServiceRefreshTokenReplaysCachedRotation verifies duplicate refreshes can reuse a cached rotation result.
func TestServiceRefreshTokenReplaysCachedRotation(t *testing.T) {
	t.Parallel()

	cached := &ephemeral.RefreshTokenRotationResult{
		AccessToken:           "cached-access-token",
		RefreshToken:          "cached-refresh-token",
		AccessTokenExpiresAt:  100,
		RefreshTokenExpiresAt: 200,
	}
	store := &refreshEphemeralStoreMock{
		getRefreshTokenRotationResultFunc: func(ctx context.Context, userID, refreshTokenUUID string) (*ephemeral.RefreshTokenRotationResult, error) {
			return cached, nil
		},
	}
	service := newRefreshTokenTestService(store, &refreshAuthServiceMock{})

	response, err := service.RefreshToken(context.Background(), &accessmanager.RefreshTokenRequest{RefreshToken: "old-refresh-token"})

	require.NoError(t, err)
	require.Equal(t, cached.AccessToken, response.AccessToken)
	require.Equal(t, cached.RefreshToken, response.RefreshToken)
	require.Zero(t, store.deleteAuthCalls)
	require.Zero(t, store.acquireRefreshTokenRotationLockCalls)
}

// TestServiceRefreshTokenWaitsForInFlightRotation verifies concurrent refreshes wait for the winning rotation result.
func TestServiceRefreshTokenWaitsForInFlightRotation(t *testing.T) {
	t.Parallel()

	cached := &ephemeral.RefreshTokenRotationResult{
		AccessToken:           "cached-access-token",
		RefreshToken:          "cached-refresh-token",
		AccessTokenExpiresAt:  100,
		RefreshTokenExpiresAt: 200,
	}
	getCalls := 0
	store := &refreshEphemeralStoreMock{
		getRefreshTokenRotationResultFunc: func(ctx context.Context, userID, refreshTokenUUID string) (*ephemeral.RefreshTokenRotationResult, error) {
			getCalls++
			if getCalls >= 2 {
				return cached, nil
			}
			return nil, nil
		},
		acquireRefreshTokenRotationLockFunc: func(ctx context.Context, userID, refreshTokenUUID string, ttl time.Duration) (bool, error) {
			return false, nil
		},
	}
	service := newRefreshTokenTestService(store, &refreshAuthServiceMock{})

	response, err := service.RefreshToken(context.Background(), &accessmanager.RefreshTokenRequest{RefreshToken: "old-refresh-token"})

	require.NoError(t, err)
	require.Equal(t, cached.AccessToken, response.AccessToken)
	require.Equal(t, cached.RefreshToken, response.RefreshToken)
	require.Zero(t, store.deleteAuthCalls)
	require.Equal(t, 1, store.acquireRefreshTokenRotationLockCalls)
}

// TestServiceRefreshTokenStoresRotationResultAfterSuccessfulRotation verifies the winning refresh stores its replay payload.
func TestServiceRefreshTokenStoresRotationResultAfterSuccessfulRotation(t *testing.T) {
	t.Parallel()

	newTokens := &auth.TokenDetails{
		AccessToken:  "new-access-token",
		AccessUUID:   "new-access-uuid",
		AtExpires:    100,
		AtTTL:        time.Minute,
		RefreshToken: "new-refresh-token",
		RefreshUUID:  "new-refresh-uuid",
		RtExpires:    200,
		RtTTL:        time.Hour,
	}
	var storedResult *ephemeral.RefreshTokenRotationResult
	store := &refreshEphemeralStoreMock{
		deleteAuthFunc: func(ctx context.Context, tokenID string) (int64, error) {
			require.Equal(t, "user-1:old-refresh-uuid", tokenID)
			return 1, nil
		},
		storeRefreshTokenRotationResultFunc: func(ctx context.Context, userID, refreshTokenUUID string, result *ephemeral.RefreshTokenRotationResult, ttl time.Duration) error {
			require.Equal(t, "user-1", userID)
			require.Equal(t, "old-refresh-uuid", refreshTokenUUID)
			require.Greater(t, ttl, time.Duration(0))
			storedResult = result
			return nil
		},
	}
	service := newRefreshTokenTestService(store, &refreshAuthServiceMock{
		createTokenFunc: func(ctx context.Context, user auth.UserModel) (*auth.TokenDetails, error) {
			return newTokens, nil
		},
	})

	response, err := service.RefreshToken(context.Background(), &accessmanager.RefreshTokenRequest{RefreshToken: "old-refresh-token"})

	require.NoError(t, err)
	require.Equal(t, newTokens.AccessToken, response.AccessToken)
	require.Equal(t, newTokens.RefreshToken, response.RefreshToken)
	require.Equal(t, &ephemeral.RefreshTokenRotationResult{
		AccessToken:           newTokens.AccessToken,
		RefreshToken:          newTokens.RefreshToken,
		AccessTokenExpiresAt:  newTokens.AtExpires,
		RefreshTokenExpiresAt: newTokens.RtExpires,
	}, storedResult)
	require.Equal(t, 1, store.deleteAuthCalls)
	require.Equal(t, 1, store.acquireRefreshTokenRotationLockCalls)
	require.Equal(t, 1, store.releaseRefreshTokenRotationLockCalls)
}

// TestServiceRefreshTokenReplaysWhenDeleteMissesAfterAnotherRotation verifies a delete miss can still replay the cached result.
func TestServiceRefreshTokenReplaysWhenDeleteMissesAfterAnotherRotation(t *testing.T) {
	t.Parallel()

	cached := &ephemeral.RefreshTokenRotationResult{
		AccessToken:           "cached-access-token",
		RefreshToken:          "cached-refresh-token",
		AccessTokenExpiresAt:  100,
		RefreshTokenExpiresAt: 200,
	}
	getCalls := 0
	store := &refreshEphemeralStoreMock{
		getRefreshTokenRotationResultFunc: func(ctx context.Context, userID, refreshTokenUUID string) (*ephemeral.RefreshTokenRotationResult, error) {
			getCalls++
			if getCalls >= 2 {
				return cached, nil
			}
			return nil, nil
		},
		deleteAuthFunc: func(ctx context.Context, tokenID string) (int64, error) {
			return 0, nil
		},
	}
	service := newRefreshTokenTestService(store, &refreshAuthServiceMock{})

	response, err := service.RefreshToken(context.Background(), &accessmanager.RefreshTokenRequest{RefreshToken: "old-refresh-token"})

	require.NoError(t, err)
	require.Equal(t, cached.AccessToken, response.AccessToken)
	require.Equal(t, cached.RefreshToken, response.RefreshToken)
	require.Equal(t, 1, store.deleteAuthCalls)
	require.Equal(t, 1, store.acquireRefreshTokenRotationLockCalls)
	require.Equal(t, 1, store.releaseRefreshTokenRotationLockCalls)
}

// TestServiceRefreshTokenReturnsContextCancellationWhileWaitingForRotation verifies waiters respect request cancellation.
func TestServiceRefreshTokenReturnsContextCancellationWhileWaitingForRotation(t *testing.T) {
	t.Parallel()

	store := &refreshEphemeralStoreMock{
		acquireRefreshTokenRotationLockFunc: func(ctx context.Context, userID, refreshTokenUUID string, ttl time.Duration) (bool, error) {
			return false, nil
		},
	}
	service := newRefreshTokenTestService(store, &refreshAuthServiceMock{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	response, err := service.RefreshToken(ctx, &accessmanager.RefreshTokenRequest{RefreshToken: "old-refresh-token"})

	require.Nil(t, response)
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, store.deleteAuthCalls)
	require.Equal(t, 1, store.acquireRefreshTokenRotationLockCalls)
	require.Zero(t, store.releaseRefreshTokenRotationLockCalls)
}
