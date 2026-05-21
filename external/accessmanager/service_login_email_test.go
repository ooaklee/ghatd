package accessmanager_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/emailmanager"
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

// Compile-time guards keep the login-email mocks aligned with service dependencies.
var _ accessmanager.EphemeralStore = (*loginEmailEphemeralStoreMock)(nil)
var _ accessmanager.EmailManager = (*loginEmailManagerMock)(nil)
