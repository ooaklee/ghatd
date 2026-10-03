package accessmanager_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/accessmanager"
	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// observedCookieService counts rotation attempts and can disconnect only its
// case-owned Redis client after rotation, before middleware retries verification.
type observedCookieService struct {
	*accessmanager.Service
	rotations    int
	afterRefresh func()
}

func (s *observedCookieService) RefreshToken(ctx context.Context, request *accessmanager.RefreshTokenRequest) (*accessmanager.RefreshTokenResponse, error) {
	s.rotations++
	response, err := s.Service.RefreshToken(ctx, request)
	if s.afterRefresh != nil {
		s.afterRefresh()
	}
	return response, err
}

// failedRotationDelete retains real lookup/locking but injects an operational
// deletion failure. No key is deleted and the original refresh stays usable.
type failedRotationDelete struct{ accessmanager.EphemeralStore }

func (failedRotationDelete) DeleteAuth(context.Context, string) (int64, error) {
	return 0, errors.New("private-storage-diagnostic")
}

func TestCookieLifecycleRealStores(t *testing.T) {
	for _, mode := range []string{"standard", "active", "admin", "optional"} {
		for _, tc := range []struct {
			name              string
			missingAccess     bool
			rotations, status int
			newCookies        bool
		}{
			{"valid", false, 0, 200, false},
			{"no cookies", false, 0, 200, false},
			{"wrong session owner", false, 0, 503, false},
			{"initial Redis outage", false, 0, 500, false},
			{"rotate missing access", true, 1, 200, true},
			{"refresh deletion outage", true, 1, 500, false},
			{"retry Redis outage", true, 1, 500, false},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				f := newConnectionFixture(t)
				// Optional public routes require a configured anonymous identity;
				// it must remain distinct from a verified account observation.
				f.service.StaticPlaceholderUuid = f.namespace + "-anonymous"
				_, err := f.db.Collection(user.UserCollection).UpdateOne(f.ctx, bson.M{"_id": f.account.ID}, bson.M{"$set": bson.M{"roles": []string{user.UserRoleAdmin}}})
				require.NoError(t, err)
				if tc.missingAccess {
					deleted, err := f.ephemeral.DeleteAuth(f.ctx, toolbox.CombinedUuidFormat(f.account.ID, f.tokens.AccessUUID))
					require.NoError(t, err)
					require.EqualValues(t, 1, deleted)
				}
				service := &observedCookieService{Service: f.service}
				switch tc.name {
				case "wrong session owner":
					key := f.namespace + "-test_" + toolbox.CombinedUuidFormat(f.account.ID, f.tokens.AccessUUID)
					require.NoError(t, f.redis.Set(key, "another-owner", time.Minute).Err())
				case "initial Redis outage":
					require.NoError(t, f.redis.Close())
				case "refresh deletion outage":
					service.EphemeralStore = failedRotationDelete{f.ephemeral}
				case "retry Redis outage":
					service.afterRefresh = func() { require.NoError(t, f.redis.Close()) }
				}
				mw := middleware.NewMiddleware(&middleware.NewMiddlewareRequest{Service: service, Environment: "production", CookiePrefixAuthToken: "access", CookiePrefixRefreshToken: "refresh", ErrorMaps: []reply.ErrorManifest{accessmanager.AccessmanagerErrorMap, auth.AuthErrorMap}})
				handlerCalls := 0
				endpoint := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					handlerCalls++
					if tc.name == "no cookies" {
						require.False(t, helpers.AcquireAuthenticatedFrom(r.Context()))
						require.Empty(t, helpers.AcquireAuthenticatedUserIDFrom(r.Context()))
						require.Nil(t, helpers.AcquireSessionFrom(r.Context()))
					}
					w.WriteHeader(http.StatusOK)
				})
				var handler http.Handler
				switch mode {
				case "standard":
					handler = mw.JWTRequired(endpoint)
				case "active":
					handler = mw.ActiveJWTRequired(endpoint)
				case "admin":
					handler = mw.AdminJWTRequired(endpoint)
				case "optional":
					handler = mw.RateLimitOrActiveJWTRequired(endpoint)
				}
				request := httptest.NewRequest(http.MethodGet, "/resource", nil)
				if tc.name != "no cookies" {
					request.AddCookie(&http.Cookie{Name: "access", Value: f.tokens.AccessToken})
					request.AddCookie(&http.Cookie{Name: "refresh", Value: f.tokens.RefreshToken})
				}
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, request)
				status := tc.status
				if tc.name == "no cookies" && mode != "optional" {
					status = http.StatusUnauthorized
				}
				require.Equal(t, status, recorder.Code, recorder.Body.String())
				require.Equal(t, tc.rotations, service.rotations)
				require.Equal(t, status == 200, handlerCalls == 1)
				require.NotContains(t, recorder.Body.String(), "private-storage-diagnostic")
				if tc.newCookies {
					require.Len(t, recorder.Result().Cookies(), 2)
					for _, cookie := range recorder.Result().Cookies() {
						require.NotEmpty(t, cookie.Value)
						require.GreaterOrEqual(t, cookie.MaxAge, 0)
					}
				} else if tc.name == "no cookies" && mode != "optional" {
					// Protected cookie adapters retain the legacy missing-pair
					// cleanup response; this must never mint replacement cookies.
					require.Len(t, recorder.Result().Cookies(), 2)
					for _, cookie := range recorder.Result().Cookies() {
						require.Empty(t, cookie.Value)
						require.Less(t, cookie.MaxAge, 0)
					}
				} else {
					require.Empty(t, recorder.Header().Values("Set-Cookie"))
				}
				if tc.name == "refresh deletion outage" {
					service.EphemeralStore = f.ephemeral
					// Recovery can still rotate the untouched refresh credential.
					response, err := f.service.RefreshToken(f.ctx, &accessmanager.RefreshTokenRequest{RefreshToken: f.tokens.RefreshToken})
					require.NoError(t, err)
					require.NotEmpty(t, response.AccessToken)
				}
			})
		}
	}
}
