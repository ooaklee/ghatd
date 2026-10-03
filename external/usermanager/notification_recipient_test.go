package usermanager_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/accessmanager"
	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/router"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/usermanager"
	"github.com/ooaklee/ghatd/external/validator"
	"github.com/ritwickdey/querydecoder"
	"github.com/stretchr/testify/require"
)

// TestNotificationRecipientMapper keeps recipient selection on administrative
// routes; a self-service URL cannot turn query or path values into authority.
func TestNotificationRecipientMapper(t *testing.T) {
	for _, tc := range []struct {
		name, path, pathUser, target, email string
	}{
		{"self defaults", "/me/notifications/latest", "", "caller", ""},
		{"self forged selectors", "/me/notifications/latest?user_id=other&user_email=other@example.com&AdminView=true&admin_view=true", "", "caller", ""},
		{"self forged path variable", "/me/notifications/latest?user_id=other&user_email=other@example.com", "other", "caller", ""},
		{"admin query target", "/notifications/latest?user_id=other", "", "other", ""},
		{"admin path precedence", "/notifications/target/latest?user_id=other", "target", "target", ""},
		{"admin email target", "/notifications/latest?user_email=invitee@example.com", "", "caller", "invitee@example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(context.Background(), "caller"), true)
			r := httptest.NewRequest(http.MethodGet, usermanager.APIUserManagerV1Prefix+tc.path, nil).WithContext(ctx)
			query := r.URL.Query()
			query.Set("kinds", "group_invite_outstanding")
			query.Set("limit", "5")
			r.URL.RawQuery = query.Encode()
			r = mux.SetURLVars(r, map[string]string{"userId": tc.pathUser})
			mapped, err := usermanager.MapRequestToGetLatestNotificationOverviewsRequest(r, validator.NewValidator())
			require.NoError(t, err)
			require.Equal(t, "caller", mapped.ActorID)
			require.Equal(t, tc.target, mapped.UserID)
			require.Equal(t, tc.email, mapped.UserEmail)
			require.Equal(t, "group_invite_outstanding", mapped.Kinds)
			require.Equal(t, 5, mapped.Limit)
			require.Equal(t, !strings.HasPrefix(tc.path, "/me/"), mapped.AdminView)
		})
	}
}

// notificationUsers is a per-case live-account port. Nil embedded methods fail
// unexpected calls, while responses and errors exercise the real manager.
type notificationUsers struct {
	usermanager.UserService
	responses map[string]*user.GetUserByIDResponse
	errors    map[string]error
	lookups   []string
}

// GetUserByID records the account key and returns that case's live result.
func (s *notificationUsers) GetUserByID(_ context.Context, r *user.GetUserByIDRequest) (*user.GetUserByIDResponse, error) {
	s.lookups = append(s.lookups, r.ID)
	return s.responses[r.ID], s.errors[r.ID]
}

// notificationGroups records the query admitted to the lower group domain.
type notificationGroups struct {
	usermanager.GroupService
	queries []common.GetLatestNotificationOverviewsRequest
	err     error
}

// GetLatestNotificationOverviews echoes only the admitted recipient's fixture.
func (s *notificationGroups) GetLatestNotificationOverviews(_ context.Context, r *common.GetLatestNotificationOverviewsRequest) (*common.GetLatestNotificationOverviewsResponse, error) {
	s.queries = append(s.queries, *r)
	if s.err != nil {
		return nil, s.err
	}
	return &common.GetLatestNotificationOverviewsResponse{Overviews: []common.NotificationOverview{{ID: "invitation", Title: r.UserEmail}}}, nil
}

// notificationAccounts allocates independent live records for each case.
func notificationAccounts(admin bool) *notificationUsers {
	actor := &user.UniversalUser{ID: "caller", Email: "caller@example.com", Type: "person", Status: user.AccountStatusKeyActive}
	if admin {
		actor.Roles = []string{"ADMIN"}
	}
	return &notificationUsers{responses: map[string]*user.GetUserByIDResponse{
		"caller": {User: actor},
		"other":  {User: &user.UniversalUser{ID: "other", Email: "other@example.com"}},
	}, errors: map[string]error{}}
}

func TestNotificationRecipientService(t *testing.T) {
	for _, tc := range []struct {
		name, actor, target, email, fault, wantTarget, wantEmail string
		admin, adminView                                         bool
		wantErr                                                  error
		wantLookups                                              []string
	}{
		{name: "self defaults", actor: "caller", wantTarget: "caller", wantEmail: "caller@example.com", wantLookups: []string{"caller"}},
		{name: "self ignores other recipient", actor: "caller", target: "other", email: "forged@example.com", wantTarget: "caller", wantEmail: "caller@example.com", wantLookups: []string{"caller"}},
		{name: "admin self remains self", actor: "caller", admin: true, target: "other", email: "forged@example.com", wantTarget: "caller", wantEmail: "caller@example.com", wantLookups: []string{"caller"}},
		{name: "admin target account", actor: "caller", admin: true, adminView: true, target: "other", wantTarget: "other", wantEmail: "other@example.com", wantLookups: []string{"caller", "other"}},
		{name: "admin target invite email", actor: "caller", admin: true, adminView: true, email: " invitee@example.com ", wantTarget: "caller", wantEmail: "invitee@example.com", wantLookups: []string{"caller"}},
		{name: "admin default self", actor: "caller", admin: true, adminView: true, wantTarget: "caller", wantEmail: "caller@example.com", wantLookups: []string{"caller"}},
		{name: "unregistered admin email precedence", actor: "caller", admin: true, adminView: true, target: "unregistered", email: "invitee@example.com", wantTarget: "unregistered", wantEmail: "invitee@example.com", wantLookups: []string{"caller"}},
		{name: "member cannot opt into admin", actor: "caller", adminView: true, target: "other", wantErr: router.ErrRouteDenied, wantLookups: []string{"caller"}},
		{name: "admin mode checks even self", actor: "caller", adminView: true, wantErr: router.ErrRouteDenied, wantLookups: []string{"caller"}},
		{name: "suspended administrator", actor: "caller", admin: true, adminView: true, fault: "suspended", wantErr: router.ErrRouteDenied, wantLookups: []string{"caller"}},
		{name: "missing actor", wantErr: usermanager.ErrUnableToIdentifyUser},
		{name: "blank actor", actor: " ", wantErr: usermanager.ErrUnableToIdentifyUser},
		{name: "nil command", fault: "nil command", wantErr: usermanager.ErrUnableToIdentifyUser},
		{name: "nil query defaults to self", actor: "caller", fault: "nil query", wantTarget: "caller", wantEmail: "caller@example.com", wantLookups: []string{"caller"}},
		{name: "nil context", actor: "caller", fault: "nil context", wantErr: router.ErrRouteUnavailable},
		{name: "canceled context", actor: "caller", fault: "canceled", wantErr: context.Canceled},
		{name: "missing users", actor: "caller", fault: "missing users", wantErr: router.ErrRouteUnavailable},
		{name: "missing groups", actor: "caller", fault: "missing groups", wantErr: usermanager.ErrGroupServiceNotEnabled},
		{name: "nil manager", actor: "caller", fault: "nil manager", wantErr: router.ErrRouteUnavailable},
		{name: "nil actor response", actor: "caller", fault: "nil actor", wantErr: router.ErrRouteUnavailable, wantLookups: []string{"caller"}},
		{name: "nil actor account", actor: "caller", fault: "nil account", wantErr: router.ErrRouteUnavailable, wantLookups: []string{"caller"}},
		{name: "mismatched actor", actor: "caller", admin: true, adminView: true, fault: "mismatched actor", wantErr: router.ErrRouteUnavailable, wantLookups: []string{"caller"}},
		{name: "missing actor lookup", actor: "caller", fault: "actor missing", wantErr: user.ErrUserNotFound, wantLookups: []string{"caller"}},
		{name: "missing target lookup", actor: "caller", admin: true, adminView: true, target: "other", fault: "target missing", wantErr: user.ErrUserNotFound, wantLookups: []string{"caller", "other"}},
		{name: "inconsistent target lookup", actor: "caller", admin: true, adminView: true, target: "other", fault: "nil target", wantErr: router.ErrRouteUnavailable, wantLookups: []string{"caller", "other"}},
		{name: "group failure", actor: "caller", fault: "group failure", wantTarget: "caller", wantEmail: "caller@example.com", wantErr: context.DeadlineExceeded, wantLookups: []string{"caller"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			users, groups := notificationAccounts(tc.admin), &notificationGroups{}
			s := &usermanager.Service{UserService: users, GroupService: groups}
			r := &usermanager.GetLatestNotificationOverviewsRequest{ActorID: tc.actor, AdminView: tc.adminView, GetLatestNotificationOverviewsRequest: &common.GetLatestNotificationOverviewsRequest{UserID: tc.target, UserEmail: tc.email, Kinds: "group_invite_outstanding", Limit: 3}}
			ctx := context.Background()
			switch tc.fault {
			case "nil command":
				r = nil
			case "nil query":
				r.GetLatestNotificationOverviewsRequest = nil
			case "nil context":
				ctx = nil
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "missing users":
				s.UserService = nil
			case "missing groups":
				s.GroupService = nil
			case "nil manager":
				s = nil
			case "suspended":
				users.responses["caller"].User.Status = user.AccountStatusKeySuspended
			case "nil actor":
				users.responses["caller"] = nil
			case "nil account":
				users.responses["caller"].User = nil
			case "mismatched actor":
				users.responses["caller"].User.ID = "other"
			case "actor missing":
				users.errors["caller"] = fmt.Errorf("private lookup: %w", user.ErrUserNotFound)
			case "target missing":
				users.errors["other"] = user.ErrUserNotFound
			case "nil target":
				users.responses["other"] = nil
			case "group failure":
				groups.err = context.DeadlineExceeded
			}
			before, err := json.Marshal(r)
			require.NoError(t, err)
			response, err := s.GetLatestNotificationOverviews(ctx, r)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				require.Nil(t, response)
			} else {
				require.NoError(t, err)
				require.NotNil(t, response)
			}
			after, marshalErr := json.Marshal(r)
			require.NoError(t, marshalErr)
			require.Equal(t, before, after, "query must not be mutated")
			if r != nil {
				require.Equal(t, tc.actor, r.ActorID)
				require.Equal(t, tc.adminView, r.AdminView)
			}
			require.Equal(t, tc.wantLookups, users.lookups)
			if tc.wantTarget == "" {
				require.Empty(t, groups.queries)
			} else {
				require.Len(t, groups.queries, 1)
				require.Equal(t, tc.wantTarget, groups.queries[0].UserID)
				require.Equal(t, tc.wantEmail, groups.queries[0].UserEmail)
				if tc.fault != "nil query" {
					require.Equal(t, 3, groups.queries[0].Limit)
					require.Equal(t, "group_invite_outstanding", groups.queries[0].Kinds)
				}
			}
		})
	}
}

// notificationPolicy has no grant calls because these legacy route definitions
// carry only credential/account requirements. Any unexpected grant call fails.
type notificationPolicy struct{ middleware.RoutePolicyService }

// TestNotificationRecipientRoutes covers real route attachment, policy, mapper,
// manager and reply composition. Authentication is a trusted publisher fixture,
// not a simulated credential verifier; credential verification has its own suite.
func TestNotificationRecipientRoutes(t *testing.T) {
	for _, tc := range []struct {
		name, path, credential, fault, recipient string
		admin                                    bool
		status                                   int
	}{
		{"self forged ID", "/me/notifications/latest?user_id=other", "session", "", "caller@example.com", false, 200},
		{"self forged email and admin mode", "/me/notifications/latest?user_email=other@example.com&admin_view=true&AdminView=true", "session", "", "caller@example.com", false, 200},
		{"self API token", "/me/notifications/latest?user_id=other&user_email=other@example.com", "api", "", "caller@example.com", false, 200},
		{"admin self cannot redirect", "/me/notifications/latest?user_id=other&user_email=other@example.com", "session", "", "caller@example.com", true, 200},
		{"admin query target", "/notifications/latest?user_id=other", "session", "", "other@example.com", true, 200},
		{"admin path target", "/notifications/other/latest?user_id=ignored", "session", "", "other@example.com", true, 200},
		{"admin email target", "/notifications/latest?user_email=invitee@example.com", "session", "", "invitee@example.com", true, 200},
		{"member admin route denied", "/notifications/other/latest", "session", "", "", false, 403},
		{"admin API not a session", "/notifications/latest?user_email=invitee@example.com", "api", "", "", true, 401},
		{"anonymous denied", "/me/notifications/latest?user_email=other@example.com", "", "", "", false, 401},
		{"admin role removed after middleware", "/notifications/other/latest", "session", "revoked", "", true, 403},
		{"admin suspended after middleware", "/notifications/other/latest", "session", "suspended", "", true, 403},
		{"nil successful account lookup", "/me/notifications/latest", "session", "nil actor", "", false, 503},
		{"native not found mapping", "/notifications/other/latest", "session", "not found", "", true, 404},
		{"private lookup failure", "/me/notifications/latest", "session", "private error", "", false, 500},
		{"malformed limit", "/me/notifications/latest?limit=bad", "session", "", "", false, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			users, groups := notificationAccounts(tc.admin), &notificationGroups{}
			account := *users.responses["caller"].User
			switch tc.fault {
			case "revoked":
				users.responses["caller"].User.Roles = nil
			case "suspended":
				users.responses["caller"].User.Status = user.AccountStatusKeySuspended
			case "nil actor":
				users.responses["caller"] = nil
			case "not found":
				users.errors["other"] = fmt.Errorf("private diagnostic: %w", user.ErrUserNotFound)
			case "private error":
				users.errors["caller"] = errors.New("private diagnostic")
			}
			publish := func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if tc.credential == "" {
						next.ServeHTTP(w, r)
						return
					}
					result := &accessmanager.MiddlewareAuthedUserResponse{Authenticated: true, UserID: account.ID, User: &account}
					if tc.credential == "api" {
						result.APIToken = &apitoken.CredentialDetails{TokenID: "credential", UserID: account.ID}
					} else {
						result.Token = &auth.TokenAccessDetails{UserID: account.ID, UserType: account.Type, AccessUUID: "session", TokenUse: auth.TokenUseAccess}
					}
					ctx, err := middleware.ContextWithAuthentication(r.Context(), result)
					require.NoError(t, err)
					next.ServeHTTP(w, r.WithContext(ctx))
				})
			}
			routes := router.NewRouter(nil, nil)
			guard, err := middleware.NewRoutePolicyGuard("example", &notificationPolicy{}, nil)
			require.NoError(t, err)
			require.NoError(t, guard.Install(routes))
			handler := usermanager.NewHandler(&usermanager.NewHandlerRequest{Service: &usermanager.Service{UserService: users, GroupService: groups}, Validator: validator.NewValidator()})
			usermanager.AttachRoutes(&usermanager.AttachRoutesRequest{Router: routes, Handler: handler, AdminOnlyMiddleware: publish, ActiveValidApiTokenOrJWTMiddleware: publish, ValidApiTokenOrJWTMiddleware: publish, RateLimitOrActiveMiddleware: publish})
			require.NoError(t, routes.ValidateRoutePolicies())
			recorder := httptest.NewRecorder()
			routes.GetRouter().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, usermanager.APIUserManagerV1Prefix+tc.path, nil))
			require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			require.NotContains(t, recorder.Body.String(), "private diagnostic")
			if tc.recipient == "" {
				require.Empty(t, groups.queries)
			} else {
				require.Len(t, groups.queries, 1)
				require.Equal(t, tc.recipient, groups.queries[0].UserEmail)
				require.Contains(t, recorder.Body.String(), tc.recipient)
			}
			if tc.fault == "not found" {
				require.Contains(t, recorder.Body.String(), user.UserErrorMap[user.ErrUserNotFound].Code)
			}
		})
	}
}

func TestNotificationAdminViewIsServerOwned(t *testing.T) {
	for _, initial := range []bool{false, true} {
		t.Run(fmt.Sprint(initial), func(t *testing.T) {
			r := &usermanager.GetLatestNotificationOverviewsRequest{AdminView: initial}
			require.NoError(t, json.Unmarshal([]byte(`{"AdminView":true,"admin_view":true}`), r))
			require.NoError(t, querydecoder.New(url.Values{"AdminView": {"true"}, "admin_view": {"true"}, "-": {"true"}}).Decode(r))
			require.Equal(t, initial, r.AdminView)
			encoded, err := json.Marshal(r)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "AdminView")
		})
	}
}
