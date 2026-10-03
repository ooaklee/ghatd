package accessmanager_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/ephemeral"
	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestRefreshHandlerFailureCookies(t *testing.T) {
	outage := errors.New("private-redis-address")
	cases := []struct {
		name            string
		err             error
		cancelAt        string
		invalidResponse bool
		status          int
		clear           bool
	}{
		{name: "outage", err: outage, status: 500},
		{name: "wrapped outage", err: fmt.Errorf("refresh: %w", outage), status: 500},
		{name: "timeout", err: accessmanager.ErrRefreshTemporarilyUnavailable, status: 503},
		{name: "missing refresh record", err: accessmanager.ErrUnauthorizedRefreshTokenCacheDeletionFailure, status: 401, clear: true},
		{name: "invalid refresh", err: accessmanager.ErrEmptyRefreshToken, status: 401, clear: true},
		{name: "wrapped invalid refresh", err: fmt.Errorf("refresh: %w", accessmanager.ErrEmptyRefreshToken), status: 401, clear: true},
		{name: "policy denial", err: accessmanager.ErrUnauthorizedNonActiveStatus, status: 401},
		{name: "joined outage and invalid", err: errors.Join(outage, accessmanager.ErrEmptyRefreshToken), status: 500},
		{name: "nil result", status: 503},
		{name: "partial result", invalidResponse: true, status: 503},
		{name: "cancel before", cancelAt: "before", status: 500},
		{name: "cancel during success", cancelAt: "during", status: 500},
		{name: "cancel during invalid", err: accessmanager.ErrEmptyRefreshToken, cancelAt: "during", status: 500},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			calls := 0
			h := newTestHandler(&mockAccessmanagerService{refreshTokenFunc: func(context.Context, *accessmanager.RefreshTokenRequest) (*accessmanager.RefreshTokenResponse, error) {
				calls++
				if tc.cancelAt == "during" {
					cancel()
				}
				if tc.invalidResponse {
					return &accessmanager.RefreshTokenResponse{RefreshToken: "incomplete"}, nil
				}
				return nil, tc.err
			}})
			request := httptest.NewRequest(http.MethodPost, "/refresh", nil).WithContext(ctx)
			request.AddCookie(&http.Cookie{Name: testCookieRefresh, Value: testValidToken128})
			if tc.cancelAt == "before" {
				cancel()
			}
			recorder := httptest.NewRecorder()
			h.RefreshToken(recorder, request)
			require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			require.Equal(t, tc.cancelAt != "before", calls == 1)
			require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
			require.NotContains(t, recorder.Body.String(), outage.Error())
			if tc.clear {
				cookies := recorder.Result().Cookies()
				require.Len(t, cookies, 4)
				for _, name := range []string{testCookieAuth, testCookieRefresh, common.AccessTokenAuthInfoCookieName, common.RefreshTokenAuthInfoCookieName} {
					found := false
					for _, cookie := range cookies {
						if cookie.Name == name {
							found = cookie.MaxAge < 0 && cookie.Value == ""
						}
					}
					require.True(t, found, name)
				}
			} else {
				require.Empty(t, recorder.Header().Values("Set-Cookie"))
			}
		})
	}
}

func TestRefreshServiceWriteBoundaries(t *testing.T) {
	privateErr := errors.New("private-driver-password")
	for _, tc := range []struct {
		name             string
		want             error
		deletes, creates int
	}{
		{"success", nil, 1, 1},
		{"nil tokens", accessmanager.ErrSessionVerificationUnavailable, 1, 0},
		{"missing token record ID", accessmanager.ErrSessionVerificationUnavailable, 1, 0},
		{"empty replay", accessmanager.ErrSessionVerificationUnavailable, 0, 0},
		{"cancel lock", context.Canceled, 0, 0},
		{"cancel mint", context.Canceled, 1, 0},
		{"cancel write", context.Canceled, 1, 1},
		{"cancel replay write", context.Canceled, 1, 1},
		{"write failure", privateErr, 1, 1},
		{"replay write best effort", nil, 1, 1},
		{"release failure best effort", nil, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			core, logs := observer.New(zap.DebugLevel)
			ctx, cancel := context.WithCancel(ghatdlogger.TransitWith(context.Background(), zap.New(core)))
			t.Cleanup(cancel)
			creates := 0
			store := &refreshEphemeralStoreMock{
				getRefreshTokenRotationResultFunc: func(context.Context, string, string) (*ephemeral.RefreshTokenRotationResult, error) {
					if tc.name == "empty replay" {
						return &ephemeral.RefreshTokenRotationResult{}, nil
					}
					return nil, nil
				},
				acquireRefreshTokenRotationLockFunc: func(context.Context, string, string, time.Duration) (bool, error) {
					if tc.name == "cancel lock" {
						cancel()
					}
					return true, nil
				},
				deleteAuthFunc: func(context.Context, string) (int64, error) { return 1, nil },
				createAuthFunc: func(context.Context, string, ephemeral.TokenDetailsAuth) error {
					creates++
					if tc.name == "cancel write" {
						cancel()
					}
					if tc.name == "write failure" {
						return privateErr
					}
					return nil
				},
				storeRefreshTokenRotationResultFunc: func(context.Context, string, string, *ephemeral.RefreshTokenRotationResult, time.Duration) error {
					if tc.name == "cancel replay write" {
						cancel()
					}
					if tc.name == "replay write best effort" {
						return privateErr
					}
					return nil
				},
				releaseRefreshTokenRotationLockFunc: func(context.Context, string, string) (int64, error) {
					if tc.name == "release failure best effort" {
						return 0, privateErr
					}
					return 1, nil
				},
			}
			service := newRefreshTokenTestService(store, &refreshAuthServiceMock{createTokenFunc: func(context.Context, auth.UserModel) (*auth.TokenDetails, error) {
				if tc.name == "nil tokens" {
					return nil, nil
				}
				if tc.name == "cancel mint" {
					cancel()
				}
				tokens := &auth.TokenDetails{AccessToken: "private-access", RefreshToken: "private-refresh", AccessUUID: "private-access-id", RefreshUUID: "private-refresh-id"}
				if tc.name == "missing token record ID" {
					tokens.AccessUUID = ""
				}
				return tokens, nil
			}})
			got, err := service.RefreshToken(ctx, &accessmanager.RefreshTokenRequest{RefreshToken: "private-presented"})
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, tc.deletes, store.deleteAuthCalls)
			require.Equal(t, tc.creates, creates)
			if tc.want != nil {
				require.Nil(t, got)
			} else {
				require.Equal(t, "private-access", got.AccessToken)
			}
			for _, entry := range logs.All() {
				encoded := fmt.Sprint(entry.Message, entry.ContextMap())
				for _, value := range []string{privateErr.Error(), "user-1", "old-refresh-uuid", "private-access", "private-refresh", "private-presented"} {
					require.NotContains(t, encoded, value)
				}
			}
		})
	}
}

func TestAccessCookieRemovalBoundary(t *testing.T) {
	for _, tc := range []struct {
		name    string
		want    error
		deletes int
	}{
		{"valid", nil, 1},
		{"nil service", accessmanager.ErrSessionVerificationUnavailable, 0},
		{"nil context", accessmanager.ErrSessionVerificationUnavailable, 0},
		{"nil claims", auth.ErrUnauthorized, 0},
		{"wrong owner", auth.ErrUnauthorized, 0},
		{"wrong purpose", auth.ErrUnauthorized, 0},
		{"missing record", accessmanager.ErrUnauthorizedAccessTokenCacheDeletionFailure, 1},
		{"storage failure", context.DeadlineExceeded, 1},
		{"canceled delete", context.Canceled, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			claims := &sessionClaimsStub{details: &auth.TokenAccessDetails{UserID: "member", AccessUUID: "session", TokenUse: auth.TokenUseAccess}}
			store := &refreshEphemeralStoreMock{deleteAuthFunc: func(context.Context, string) (int64, error) {
				switch tc.name {
				case "missing record":
					return 0, nil
				case "storage failure":
					return 0, context.DeadlineExceeded
				case "canceled delete":
					cancel()
				}
				return 1, nil
			}}
			service := &accessmanager.Service{AuthService: claims, EphemeralStore: store}
			switch tc.name {
			case "nil service":
				service = nil
			case "nil context":
				ctx = nil
			case "nil claims":
				claims.details = nil
			case "wrong owner":
				claims.details.UserID = "other"
			case "wrong purpose":
				claims.details.TokenUse = auth.TokenUseLogin
			}
			err := service.RemoveAccessTokenWithCookieValue(ctx, "member", "selected-access")
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, tc.deletes, store.deleteAuthCalls)
		})
	}
}

func TestRefreshServiceOperationalFailures(t *testing.T) {
	outage := errors.New("storage unavailable")
	cases := []struct {
		name, phase            string
		want                   error
		wantDeletes, wantLocks int
	}{
		{"cached replay outage", "read", outage, 0, 0},
		{"rotation lock outage", "lock", outage, 0, 1},
		{"waiter read outage", "wait", outage, 0, 1},
		{"waiter timeout", "timeout", accessmanager.ErrRefreshTemporarilyUnavailable, 0, 1},
		{"delete outage", "delete", outage, 1, 1},
		{"confirmed missing refresh", "missing", accessmanager.ErrUnauthorizedRefreshTokenCacheDeletionFailure, 1, 1},
		{"cancel after deletion", "cancel delete", context.Canceled, 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			reads, creates := 0, 0
			store := &refreshEphemeralStoreMock{
				getRefreshTokenRotationResultFunc: func(context.Context, string, string) (*ephemeral.RefreshTokenRotationResult, error) {
					reads++
					if tc.phase == "read" || (tc.phase == "wait" && reads > 1) {
						return nil, outage
					}
					return nil, nil
				},
				acquireRefreshTokenRotationLockFunc: func(context.Context, string, string, time.Duration) (bool, error) {
					if tc.phase == "lock" {
						return false, outage
					}
					return tc.phase != "wait" && tc.phase != "timeout", nil
				},
				deleteAuthFunc: func(context.Context, string) (int64, error) {
					if tc.phase == "delete" {
						return 0, outage
					}
					if tc.phase == "cancel delete" {
						cancel()
						return 1, nil
					}
					return 0, nil
				},
			}
			service := newRefreshTokenTestService(store, &refreshAuthServiceMock{createTokenFunc: func(context.Context, auth.UserModel) (*auth.TokenDetails, error) {
				creates++
				return nil, errors.New("must not issue")
			}})
			response, err := service.RefreshToken(ctx, &accessmanager.RefreshTokenRequest{RefreshToken: "presented-refresh"})
			require.Nil(t, response)
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, tc.wantDeletes, store.deleteAuthCalls)
			require.Equal(t, tc.wantLocks, store.acquireRefreshTokenRotationLockCalls)
			require.Zero(t, creates)
			if tc.phase != "missing" {
				require.Equal(t, accessmanager.SessionErrorUnknown, accessmanager.ClassifySessionError(err))
			}
		})
	}
}
