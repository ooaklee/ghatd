package auth_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ooaklee/ghatd/external/auth"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
)

func newObservedContext() (context.Context, *observer.ObservedLogs) {
	core, observed := observer.New(zapcore.DebugLevel)
	logger := zap.New(core)
	return ghatdlogger.TransitWith(context.Background(), logger), observed
}

func newService() *auth.Service {
	return auth.NewService(&auth.NewServiceRequest{
		AccessTokenSecret:  "access-secret",
		RefreshTokenSecret: "refresh-secret",
	})
}

func TestServiceExtractTokenMetadataHandlesRequestWithoutURL(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	service := auth.NewService(&auth.NewServiceRequest{
		AccessTokenSecret:  "access-secret",
		RefreshTokenSecret: "refresh-secret",
	})
	tokenDetails, err := service.CreateToken(ctx, &userv2.UniversalUser{
		ID:     "user-1",
		Status: userv2.AccountStatusKeyActive,
	})
	require.NoError(t, err)

	req := &http.Request{Header: http.Header{}}
	req.Header.Set("Authorization", "Bearer "+tokenDetails.AccessToken)

	var details *auth.TokenAccessDetails
	require.NotPanics(t, func() {
		details, err = service.ExtractTokenMetadata(ctx, req)
	})
	require.NoError(t, err)
	require.Equal(t, "user-1", details.UserID)
}

func TestServiceExtractTokenMetadataMissingHeaderDoesNotWarn(t *testing.T) {
	t.Parallel()

	ctx, observed := newObservedContext()
	svc := newService()
	req := &http.Request{
		Header: http.Header{},
		URL:    &url.URL{Path: "/api/test"},
	}

	details, err := svc.ExtractTokenMetadata(ctx, req)
	require.ErrorIs(t, err, auth.ErrNoBearerHeaderFound)
	require.Nil(t, details)
	require.Empty(t, observed.FilterLevelExact(zapcore.WarnLevel).All())
	require.Empty(t, observed.FilterLevelExact(zapcore.ErrorLevel).All())
	require.Len(t, observed.FilterMessage("auth-bearer-header-missing").All(), 1)
}

func TestServiceExtractTokenMetadataMalformedTokenLogsOnce(t *testing.T) {
	t.Parallel()

	ctx, observed := newObservedContext()
	svc := newService()
	req := &http.Request{
		Header: http.Header{"Authorization": []string{"Bearer not-a-valid-jwt"}},
		URL:    &url.URL{Path: "/api/test"},
	}

	details, err := svc.ExtractTokenMetadata(ctx, req)
	require.ErrorIs(t, err, auth.ErrUnauthorizedMalformattedToken)
	require.Nil(t, details)
	require.Len(t, observed.FilterLevelExact(zapcore.WarnLevel).All(), 1)
	require.Len(t, observed.FilterMessage("access-token-malformatted").All(), 1)
}

func TestServiceCheckTokenIsValidDoesNotRelogMalformedToken(t *testing.T) {
	t.Parallel()

	ctx, observed := newObservedContext()
	svc := newService()
	req := &http.Request{
		Header: http.Header{"Authorization": []string{"Bearer not-a-valid-jwt"}},
		URL:    &url.URL{Path: "/api/test"},
	}

	err := svc.CheckTokenIsValid(ctx, req)
	require.ErrorIs(t, err, auth.ErrUnauthorizedMalformattedToken)
	require.Len(t, observed.FilterLevelExact(zapcore.WarnLevel).All(), 1)
	require.Len(t, observed.FilterMessage("access-token-malformatted").All(), 1)
}

func TestServiceParseAccessTokenFromStringUnexpectedSigningMethodLogsOnce(t *testing.T) {
	t.Parallel()

	ctx, observed := newObservedContext()
	svc := newService()

	unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"sub": "user-1", "exp": time.Now().Add(time.Hour).Unix(),
	})
	signingString, err := unsigned.SigningString()
	require.NoError(t, err)
	tokenString := signingString + "."

	_, err = svc.ParseAccessTokenFromString(ctx, tokenString)
	require.ErrorIs(t, err, auth.ErrUnauthorizedTokenUnexpectedSigningMethod)
	require.Empty(t, observed.FilterLevelExact(zapcore.ErrorLevel).All())
	require.Len(t, observed.FilterLevelExact(zapcore.WarnLevel).All(), 1)
	logs := observed.FilterMessage("access-token-unexpected-signing-method").All()
	require.Len(t, logs, 1)
	require.Equal(t, "none", logs[0].ContextMap()["algorithm"])
}

func TestServiceParseAccessTokenFromStringInvalidSignatureLogsOnce(t *testing.T) {
	t.Parallel()

	ctx, observed := newObservedContext()
	svc := newService()

	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "user-1",
	}).SignedString([]byte("wrong-secret"))
	require.NoError(t, err)

	_, err = svc.ParseAccessTokenFromString(ctx, token)
	require.ErrorIs(t, err, auth.ErrUnauthorizedParsedStringUnknown)
	require.Empty(t, observed.FilterLevelExact(zapcore.ErrorLevel).All())
	require.Len(t, observed.FilterLevelExact(zapcore.WarnLevel).All(), 1)
	require.Len(t, observed.FilterMessage("access-token-signature-invalid").All(), 1)
}

func TestServiceCheckRefreshTokenIsValidDoesNotRelogMalformedToken(t *testing.T) {
	t.Parallel()

	ctx, observed := newObservedContext()
	svc := newService()

	_, err := svc.CheckRefreshTokenIsValid(ctx, "not-a-valid-jwt")
	require.ErrorIs(t, err, auth.ErrUnauthorizedRefreshTokenExpired)
	require.Len(t, observed.FilterLevelExact(zapcore.WarnLevel).All(), 1)
	require.Len(t, observed.FilterMessage("refresh-token-malformatted").All(), 1)
}
