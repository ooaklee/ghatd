package accessmanager_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/contentmanager"
	"github.com/ooaklee/ghatd/external/ephemeral"
	"github.com/ooaklee/ghatd/external/post"
	"github.com/ooaklee/ghatd/external/router"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/validator"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
)

// optionalContentPosts records the lower-domain call after the production CMS
// route, authentication, policy, mapper and manager have admitted the request.
// Unimplemented methods fail if an unexpected operation is dispatched.
type optionalContentPosts struct {
	*post.Service
	calls                       int
	actor, requestor, recipient string
	authenticated               bool
}

func (s *optionalContentPosts) GetLatestNotificationOverviews(ctx context.Context, req *common.GetLatestNotificationOverviewsRequest) (*common.GetLatestNotificationOverviewsResponse, error) {
	s.calls++
	s.actor = helpers.AcquireAuthenticatedUserIDFrom(ctx)
	s.requestor = helpers.AcquireFrom(ctx)
	s.recipient = req.UserID
	s.authenticated = helpers.AcquireAuthenticatedFrom(ctx)
	return &common.GetLatestNotificationOverviewsResponse{Overviews: []common.NotificationOverview{}}, nil
}

// unusedOptionalPolicy deliberately supplies no grant methods: these routes
// have no grant requirements, and any unexpected lookup must fail the test.
type unusedOptionalPolicy struct{ middleware.RoutePolicyService }

func TestOptionalContentRoutesWithoutPlaceholder(t *testing.T) {
	for _, placeholder := range []string{"", "anonymous"} {
		for _, tc := range []struct {
			name, cookie, path       string
			status, accounting       int
			authenticated, suspended bool
			storeErr                 error
		}{
			{name: "anonymous", status: 200, accounting: 1},
			{name: "empty cookies", cookie: "empty", status: 200, accounting: 1},
			{name: "orphan cookie", cookie: "orphan", status: 200, accounting: 1},
			{name: "active session", cookie: "valid", status: 200, authenticated: true},
			{name: "suspended session", cookie: "valid", status: 401, suspended: true},
			{name: "rate limited", status: 429, accounting: 1, storeErr: ephemeral.ErrRequestorLimitExceeded},
			{name: "rate store unavailable", status: 500, accounting: 1, storeErr: errors.New("private diagnostic")},
			{name: "protected route", path: "/protected", status: 401},
			{name: "optional route with scope", path: "/restricted", status: 401, accounting: 1},
		} {
			t.Run(placeholder+"/"+tc.name, func(t *testing.T) {
				claims := &sessionClaimsStub{err: auth.ErrNoBearerHeaderFound}
				account := &user.UniversalUser{ID: "member", Type: "person", Status: user.AccountStatusKeyActive}
				if tc.cookie == "valid" {
					claims.err = nil
					claims.details = &auth.TokenAccessDetails{UserID: account.ID, AccessUUID: "session", UserType: account.Type, TokenUse: auth.TokenUseAccess}
				}
				if tc.suspended {
					account.Status = user.AccountStatusKeySuspended
				}
				store := &sessionStoreStub{owner: account.ID, err: tc.storeErr}
				users := &sessionAccountStub{response: &user.GetUserByIDResponse{User: account}}
				service := &accessmanager.Service{AuthService: claims, EphemeralStore: store, UserService: users, StaticPlaceholderUuid: placeholder}
				mw := middleware.NewMiddleware(&middleware.NewMiddlewareRequest{Service: service, CookiePrefixAuthToken: "access", CookiePrefixRefreshToken: "refresh", ErrorMaps: []reply.ErrorManifest{accessmanager.AccessmanagerErrorMap, auth.AuthErrorMap, ephemeral.EphemeralStoreErrorMap}})
				posts := &optionalContentPosts{}
				routes := router.NewRouter(nil, nil)
				guard, err := middleware.NewRoutePolicyGuard("example", &unusedOptionalPolicy{}, nil)
				require.NoError(t, err)
				require.NoError(t, guard.Install(routes))
				contentmanager.AttachRoutes(&contentmanager.AttachRoutesRequest{Router: routes, Handler: contentmanager.NewHandler(contentmanager.NewService(posts, nil), validator.NewValidator()), MiddlewareAdminApiTokenOrJwtRequired: mw.AdminApiTokenOrJWTRequired, RateLimitOrActiveMiddleware: mw.RateLimitOrActiveJWTRequired})
				deniedHandler := func(http.ResponseWriter, *http.Request) { t.Fatal("anonymous request reached protected handler") }
				routes.NewRouteGroup("/protected", router.Session, mw.JWTRequired).Handle(router.RouteDefinition{Path: "", Methods: []string{http.MethodGet}, Operation: "test.Protected"}, deniedHandler)
				routes.NewRouteGroup("/restricted", router.OptionalActive, mw.RateLimitOrActiveJWTRequired).Handle(router.RouteDefinition{Path: "", Methods: []string{http.MethodGet}, Operation: "test.Restricted", Policy: router.RoutePolicy{Scopes: []string{"private:read"}}}, deniedHandler)
				require.NoError(t, routes.ValidateRoutePolicies())
				path := tc.path
				if path == "" {
					path = "/api/v1/cms/latest?kinds=post_article,post_changelog&limit=20"
				}
				request := httptest.NewRequest(http.MethodGet, path, nil)
				switch tc.cookie {
				case "empty":
					request.AddCookie(&http.Cookie{Name: "access"})
					request.AddCookie(&http.Cookie{Name: "refresh"})
				case "orphan":
					request.AddCookie(&http.Cookie{Name: "access", Value: "stale"})
				case "valid":
					request.AddCookie(&http.Cookie{Name: "access", Value: "selected-session"})
					request.AddCookie(&http.Cookie{Name: "refresh", Value: "selected-refresh"})
				}
				response := httptest.NewRecorder()
				routes.GetRouter().ServeHTTP(response, request)
				require.Equal(t, tc.status, response.Code, response.Body.String())
				require.Equal(t, tc.accounting, store.anonymous)
				require.NotContains(t, response.Body.String(), "private diagnostic")
				if tc.status != http.StatusOK {
					require.Zero(t, posts.calls)
					return
				}
				require.Equal(t, 1, posts.calls)
				require.JSONEq(t, `{"data":[]}`, response.Body.String())
				require.Equal(t, tc.authenticated, posts.authenticated)
				if tc.authenticated {
					require.Equal(t, account.ID, posts.actor)
					require.Equal(t, account.ID, posts.recipient)
				} else {
					require.Empty(t, posts.actor)
					require.Empty(t, posts.recipient)
					require.Equal(t, placeholder, posts.requestor)
				}
			})
		}
	}
}
