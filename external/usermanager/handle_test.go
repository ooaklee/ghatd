package usermanager

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/router"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
)

// handleUsers records lower-domain delegation; unexpected capabilities fail.
type handleUsers struct {
	UserService
	actor, operation, handle string
	revision                 int64
	err                      error
}

func (s *handleUsers) GetUserHandle(_ context.Context, actor string) (*user.UserHandle, error) {
	s.actor = actor
	s.operation = "get"
	return &user.UserHandle{}, s.err
}
func (s *handleUsers) ValidateUserHandle(_ context.Context, r *user.ValidateUserHandleRequest) (*user.ValidateUserHandleResponse, error) {
	s.actor = r.UserID
	s.operation = "validate"
	s.handle = r.Handle
	return &user.ValidateUserHandleResponse{Handle: r.Handle, Available: true}, s.err
}
func (s *handleUsers) UpdateUserHandle(_ context.Context, r *user.UpdateUserHandleRequest) (*user.UserHandle, error) {
	s.actor = r.UserID
	s.operation = "update"
	s.handle = r.Handle
	s.revision = r.ExpectedRevision
	return &user.UserHandle{Handle: r.Handle, Metadata: user.HandleMetadata{Revision: r.ExpectedRevision + 1}}, s.err
}

// handleRequest simulates the output of already verified middleware. It does
// not bypass production credential verification or make arbitrary claims trusted.
func handleRequest(kind, method, body string) *http.Request {
	ctx := context.Background()
	ctx = accesshelpers.TransitWith(ctx, "caller")
	ctx = accesshelpers.TransitAuthenticatedWith(ctx, kind != "anonymous")
	account := &user.UniversalUser{ID: "caller", Type: "member", Status: "ACTIVE"}
	claims := &auth.TokenAccessDetails{UserID: "caller", UserType: "member", TokenUse: auth.TokenUseAccess, AccessUUID: "session-id"}
	if kind == "wrong account" {
		account.ID = "other"
	}
	if kind == "wrong type" {
		claims.UserType = "other"
	}
	if kind == "restricted" {
		account.Status = "SUSPENDED"
	}
	if kind == "proof" {
		claims.TokenUse = auth.TokenUseLogin
	}
	if kind == "legacy" {
		claims.UserType = ""
		claims.TokenUse = ""
	}
	if kind == "api" {
		claims = nil
	}
	if kind == "api" || kind == "mixed" {
		ctx = accesshelpers.TransitAPITokenWith(ctx, &apitoken.CredentialDetails{})
	}
	if kind != "no session" {
		ctx = accesshelpers.TransitSessionWith(ctx, claims)
	}
	if kind != "no account" {
		ctx = accesshelpers.TransitUserWith(ctx, account)
	}
	r := httptest.NewRequest(method, "/api/v1/ums/me/handle?user_id=victim", strings.NewReader(body)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("If-Match", `"0"`)
	return r
}

func TestHandleHTTPIdentityAndDelegation(t *testing.T) {
	for _, operation := range []string{"get", "validate", "update"} {
		t.Run(operation, func(t *testing.T) {
			for _, kind := range []string{"session", "legacy", "anonymous", "api", "mixed", "proof", "no session", "no account", "wrong account", "wrong type", "restricted"} {
				t.Run(kind, func(t *testing.T) {
					users := &handleUsers{}
					h := NewHandler(&NewHandlerRequest{Service: &Service{UserService: users}})
					r := handleRequest(kind, http.MethodPatch, `{"handle":"calm-fox"}`)
					rec := httptest.NewRecorder()
					switch operation {
					case "get":
						h.GetMyHandle(rec, r)
					case "validate":
						h.ValidateMyHandle(rec, r)
					case "update":
						h.UpdateMyHandle(rec, r)
					}
					require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
					if kind != "session" && kind != "legacy" {
						require.Equal(t, 401, rec.Code, rec.Body.String())
						require.Empty(t, users.operation)
						require.Contains(t, rec.Body.String(), "ROUTE_AUTHENTICATION")
						return
					}
					require.Equal(t, 200, rec.Code, rec.Body.String())
					require.Equal(t, "caller", users.actor)
					require.Equal(t, operation, users.operation)
					switch operation {
					case "get":
						require.Equal(t, `"0"`, rec.Header().Get("ETag"))
					case "validate":
						require.Empty(t, rec.Header().Get("ETag"))
						require.Equal(t, "calm-fox", users.handle)
					case "update":
						require.Equal(t, `"1"`, rec.Header().Get("ETag"))
						require.EqualValues(t, 0, users.revision)
					}
				})
			}
		})
	}
}

func TestHandleHTTPBodyBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, body, media, encoding string
		status                      int
	}{
		{"valid", `{"handle":"calm-fox"}`, "application/json", "", 200},
		{"charset", `{"handle":"calm-fox"}`, "application/json; charset=utf-8", "", 200},
		{"wrong type", `{"handle":"calm-fox"}`, "text/plain", "", 400},
		{"missing type", `{"handle":"calm-fox"}`, "", "", 400},
		{"encoding", `{"handle":"calm-fox"}`, "application/json", "gzip", 400},
		{"actor injection", `{"handle":"calm-fox","ActorID":"victim"}`, "application/json", "", 400},
		{"target injection", `{"user_id":"victim","handle":"calm-fox"}`, "application/json", "", 400},
		{"duplicate", `{"handle":"calm-fox","handle":"other"}`, "application/json", "", 400},
		{"wrong case", `{"Handle":"calm-fox"}`, "application/json", "", 400},
		{"null", `{"handle":null}`, "application/json", "", 400},
		{"array", `["calm-fox"]`, "application/json", "", 400},
		{"empty", `{}`, "application/json", "", 400},
		{"number", `{"handle":123}`, "application/json", "", 400},
		{"trailing", `{"handle":"calm-fox"} {}`, "application/json", "", 400},
		{"oversized", `{"handle":"` + strings.Repeat("a", 1024) + `"}`, "application/json", "", 400},
		{"invalid utf8", "{\"handle\":\"\xff\"}", "application/json", "", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			users := &handleUsers{}
			h := NewHandler(&NewHandlerRequest{Service: &Service{UserService: users}})
			r := handleRequest("session", http.MethodPatch, tc.body)
			r.Header.Del("Content-Type")
			if tc.media != "" {
				r.Header.Set("Content-Type", tc.media)
			}
			if tc.encoding != "" {
				r.Header.Set("Content-Encoding", tc.encoding)
			}
			rec := httptest.NewRecorder()
			h.UpdateMyHandle(rec, r)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			if tc.status != 200 {
				require.Empty(t, users.operation)
			}
		})
	}
}

func TestHandleHTTPRevisions(t *testing.T) {
	for _, tc := range []struct {
		name   string
		tags   []string
		status int
	}{
		{"initial", []string{`"0"`}, 200}, {"current", []string{`"42"`}, 200},
		{"absent", nil, 428}, {"wildcard", []string{"*"}, 400}, {"weak", []string{`W/"1"`}, 400},
		{"list", []string{`"1", "2"`}, 400}, {"repeated header", []string{`"1"`, `"2"`}, 400},
		{"negative", []string{`"-1"`}, 400}, {"leading zero", []string{`"01"`}, 400}, {"plus", []string{`"+1"`}, 400},
		{"maximum exhausted", []string{`"9007199254740991"`}, 400}, {"overflow", []string{`"99999999999999999999"`}, 400}, {"empty", []string{`""`}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			users := &handleUsers{}
			h := NewHandler(&NewHandlerRequest{Service: &Service{UserService: users}})
			r := handleRequest("session", http.MethodPatch, `{"handle":"calm-fox"}`)
			r.Header.Del("If-Match")
			for _, tag := range tc.tags {
				r.Header.Add("If-Match", tag)
			}
			rec := httptest.NewRecorder()
			h.UpdateMyHandle(rec, r)
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			if tc.status != 200 {
				require.Empty(t, users.operation)
			}
		})
	}
}

func TestHandleHTTPErrorMapping(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		status   int
		code     string
		override bool
	}{
		{"wrapped conflict", fmt.Errorf("private diagnostic: %w", user.ErrHandleConflict), 412, "USV2-029", false},
		{"taken", user.ErrHandleTaken, 409, "USV2-028", false},
		{"index readiness", user.ErrHandleIndexesRequired, 503, "USV2-031", false},
		{"unknown", errors.New("private diagnostic"), 500, "", false},
		{"mixed", errors.Join(user.ErrHandleTaken, errors.New("private diagnostic")), 500, "", false},
		{"host override", user.ErrHandleTaken, 422, "HOST-HANDLE", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			users := &handleUsers{err: tc.err}
			req := &NewHandlerRequest{Service: &Service{UserService: users}}
			if tc.override {
				req.ErrorMaps = []reply.ErrorManifest{{user.ErrHandleTaken: {StatusCode: 422, Code: "HOST-HANDLE"}}}
			}
			h := NewHandler(req)
			rec := httptest.NewRecorder()
			h.UpdateMyHandle(rec, handleRequest("session", http.MethodPatch, `{"handle":"calm-fox"}`))
			require.Equal(t, tc.status, rec.Code, rec.Body.String())
			require.NotContains(t, rec.Body.String(), "private diagnostic")
			require.Empty(t, rec.Header().Get("ETag"))
			if tc.code != "" {
				require.Contains(t, rec.Body.String(), tc.code)
			}
		})
	}
}

func TestHandleRouteAdmission(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		middleware, authorizer, handler bool
		valid                           bool
	}{
		{"enabled", true, true, true, true}, {"no session middleware", false, true, true, false},
		{"no revision guard", true, false, true, false}, {"no handlers", true, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := router.NewRouter(nil, nil)
			users := &handleUsers{}
			req := &AttachRoutesRequest{Router: r}
			if tc.handler {
				req.Handler = NewHandler(&NewHandlerRequest{Service: &Service{UserService: users}})
			}
			if tc.middleware {
				req.ActiveOnlyMiddleware = func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
						next.ServeHTTP(w, request.WithContext(handleRequest("session", http.MethodGet, "").Context()))
					})
				}
			}
			if tc.authorizer {
				require.NoError(t, r.SetRouteAuthorizer(func(_ context.Context, request *http.Request, d router.RouteDefinition) error {
					require.Equal(t, router.ActiveSession, d.Access)
					if d.Policy.RevisionRequired {
						_, err := handleRevision(request)
						return err
					}
					return nil
				}))
			}
			attachHandleRoutes(req)
			if !tc.valid {
				require.ErrorIs(t, r.ValidateRoutePolicies(), router.ErrRouteConfiguration)
				return
			}
			require.NoError(t, r.ValidateRoutePolicies())
			for _, call := range []struct {
				method, path string
				status       int
			}{
				{http.MethodGet, "/api/v1/ums/me/handle", 200}, {http.MethodPost, "/api/v1/ums/me/handle/validate", 200}, {http.MethodPatch, "/api/v1/ums/me/handle", 428},
			} {
				rec := httptest.NewRecorder()
				request := httptest.NewRequest(call.method, call.path, strings.NewReader(`{"handle":"calm-fox"}`))
				request.Header.Set("Content-Type", "application/json")
				r.GetRouter().ServeHTTP(rec, request)
				require.Equal(t, call.status, rec.Code, rec.Body.String())
			}
		})
	}
}
