package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/apitoken"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/require"
)

// policyRecorder captures the guard's boundary calls without pretending to
// implement live grants or transactional accounting; those need store tests.
type policyRecorder struct {
	events                   []string
	subject                  accesspolicy.Subject
	scopes, permissions      []string
	consumptions             []accesspolicy.Consumption
	authorizeErr, consumeErr error
}

func (s *policyRecorder) Authorize(_ context.Context, subject accesspolicy.Subject, scopes, permissions []string) error {
	s.events = append(s.events, "authorize")
	s.subject, s.scopes, s.permissions = subject, scopes, permissions
	return s.authorizeErr
}

func (s *policyRecorder) ConsumeAuthorized(_ context.Context, request accesspolicy.Consumption) (accesspolicy.Usage, error) {
	s.events = append(s.events, "consume")
	s.consumptions = append(s.consumptions, request)
	return accesspolicy.Usage{}, s.consumeErr
}

// policyContext publishes an authenticated fixture using the production
// publisher; mixed/forged contexts are deliberately assembled by their cases.
func policyContext(t *testing.T, credential string) context.Context {
	t.Helper()
	if credential == "anonymous" {
		return context.Background()
	}
	result := mockAuthedResp("member", "ACTIVE", nil)
	result.User.Type = "person"
	result.Authenticated = true
	switch credential {
	case "session", "mixed", "mismatched user":
		result.Token = &auth.TokenAccessDetails{UserID: "member", AccessUUID: "session", UserType: "person"}
	case "api":
		result.APIToken = &apitoken.CredentialDetails{UserID: "member", TokenID: "credential"}
	}
	ctx, err := ContextWithAuthentication(context.Background(), result)
	require.NoError(t, err)
	if credential == "mixed" {
		ctx = helpers.TransitAPITokenWith(ctx, &apitoken.CredentialDetails{UserID: "member", TokenID: "credential"})
	}
	if credential == "mismatched user" {
		ctx = helpers.TransitWith(ctx, "other")
	}
	return ctx
}

func TestRoutePolicyGuardIdentity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, credential string
		access           router.AccessMode
		policy           router.RoutePolicy
		wantError        error
		wantSubject      accesspolicy.Subject
	}{
		{name: "public anonymous", credential: "anonymous", access: router.Public},
		{name: "handler owns proof", credential: "anonymous", access: router.HandlerVerified, policy: router.RoutePolicy{Proof: "webhook-signature"}},
		{name: "public requirements rejected", credential: "anonymous", access: router.Public, policy: router.RoutePolicy{Scopes: []string{"read"}}, wantError: router.ErrRouteConfiguration},
		{name: "optional anonymous", credential: "anonymous", access: router.OptionalActive},
		{name: "profile anonymous", credential: "anonymous", access: router.ProfileOptional},
		{name: "optional restricted needs identity", credential: "anonymous", access: router.OptionalActive, policy: router.RoutePolicy{Scopes: []string{"read"}}, wantError: router.ErrRouteUnauthenticated},
		{name: "protected anonymous", credential: "anonymous", access: router.Session, wantError: router.ErrRouteUnauthenticated},
		{name: "missing credential metadata", credential: "missing", access: router.Session, wantError: router.ErrRouteUnauthenticated},
		{name: "mixed metadata", credential: "mixed", access: router.SessionOrAPI, wantError: router.ErrRouteUnauthenticated},
		{name: "actor user mismatch", credential: "mismatched user", access: router.Session, wantError: router.ErrRouteUnauthenticated},
		{name: "API cannot enter session route", credential: "api", access: router.Session, wantError: router.ErrRouteUnauthenticated},
		{name: "API cannot enter active session route", credential: "api", access: router.ActiveSession, wantError: router.ErrRouteUnauthenticated},
		{name: "API cannot enter admin session route", credential: "api", access: router.AdminSession, wantError: router.ErrRouteUnauthenticated},
		{name: "session without extra grants", credential: "session", access: router.Session},
		{name: "current type accepted", credential: "session", access: router.Session, policy: router.RoutePolicy{UserTypes: []string{"partner", "person"}}},
		{name: "current type rejected", credential: "session", access: router.Session, policy: router.RoutePolicy{UserTypes: []string{"partner"}}, wantError: router.ErrRouteDenied},
		{name: "user subject", credential: "session", access: router.Session, policy: router.RoutePolicy{Scopes: []string{"read"}, Permissions: []string{"view"}}, wantSubject: accesspolicy.Subject{System: "service", Kind: accesspolicy.UserSubject, ID: "member"}},
		{name: "credential subject never owner union", credential: "api", access: router.ActiveSessionOrAPI, policy: router.RoutePolicy{Scopes: []string{"read"}, Permissions: []string{"view"}}, wantSubject: accesspolicy.Subject{System: "service", Kind: accesspolicy.APITokenSubject, ID: "credential"}},
		{name: "unknown mode", credential: "session", access: "sessoin", wantError: router.ErrRouteConfiguration},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &policyRecorder{}
			guard, err := NewRoutePolicyGuard("service", store, nil)
			require.NoError(t, err)
			err = guard.Authorize(policyContext(t, tc.credential), httptest.NewRequest("GET", "/items", nil), router.RouteDefinition{Access: tc.access, Policy: tc.policy})
			require.ErrorIs(t, err, tc.wantError)
			require.Equal(t, tc.wantSubject, store.subject)
			if tc.wantSubject.ID != "" {
				require.Equal(t, tc.policy.Scopes, store.scopes)
				require.Equal(t, tc.policy.Permissions, store.permissions)
			}
			require.Empty(t, store.consumptions)
		})
	}
}

func TestRoutePolicyGuardOrderAndErrorMapping(t *testing.T) {
	t.Parallel()
	grantFailure := errors.New("private backend diagnostic")
	resourceFailure := errors.New("private ownership diagnostic")
	counterFailure := errors.New("private counter diagnostic")
	for _, tc := range []struct {
		name                                  string
		authorizeErr, resourceErr, consumeErr error
		missingRevision                       bool
		wantError                             error
		wantEvents                            []string
	}{
		{name: "admitted", wantEvents: []string{"authorize", "resource", "consume"}},
		{name: "grant denied", authorizeErr: accesspolicy.ErrDenied, wantError: accesspolicy.ErrDenied, wantEvents: []string{"authorize"}},
		{name: "grant unavailable", authorizeErr: grantFailure, wantError: grantFailure, wantEvents: []string{"authorize"}},
		{name: "revision missing", missingRevision: true, wantError: router.ErrRoutePrecondition, wantEvents: []string{"authorize"}},
		{name: "resource denied", resourceErr: accesspolicy.ErrDenied, wantError: accesspolicy.ErrDenied, wantEvents: []string{"authorize", "resource"}},
		{name: "resource unavailable", resourceErr: resourceFailure, wantError: resourceFailure, wantEvents: []string{"authorize", "resource"}},
		{name: "revoked at admission", consumeErr: accesspolicy.ErrDenied, wantError: accesspolicy.ErrDenied, wantEvents: []string{"authorize", "resource", "consume"}},
		{name: "quota exhausted", consumeErr: accesspolicy.ErrLimitReached, wantError: accesspolicy.ErrLimitReached, wantEvents: []string{"authorize", "resource", "consume"}},
		{name: "bad policy", consumeErr: accesspolicy.ErrConfiguration, wantError: accesspolicy.ErrConfiguration, wantEvents: []string{"authorize", "resource", "consume"}},
		{name: "counter unavailable", consumeErr: counterFailure, wantError: counterFailure, wantEvents: []string{"authorize", "resource", "consume"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			store := &policyRecorder{authorizeErr: tc.authorizeErr, consumeErr: tc.consumeErr}
			guard, err := NewRoutePolicyGuard("service", store, map[string]ResourceCheck{"owner": func(_ context.Context, _ *http.Request, actor string) error {
				require.Equal(t, "member", actor)
				store.events = append(store.events, "resource")
				return tc.resourceErr
			}})
			require.NoError(t, err)
			req := httptest.NewRequest("PATCH", "/items/item", nil)
			req.Header.Set("Idempotency-Key", "client-controlled-key")
			if !tc.missingRevision {
				req.Header.Set("If-Match", `"revision"`)
			}
			def := router.RouteDefinition{Access: router.SessionOrAPI, Operation: "items.update", Policy: router.RoutePolicy{Scopes: []string{"write"}, Permissions: []string{"update"}, RevisionRequired: true, ResourceCheck: "owner", UsageMetric: "requests"}}
			err = guard.Authorize(policyContext(t, "api"), req, def)
			require.ErrorIs(t, err, tc.wantError)
			require.Equal(t, tc.wantEvents, store.events)
			for _, c := range store.consumptions {
				require.Equal(t, accesspolicy.Subject{System: "service", Kind: accesspolicy.APITokenSubject, ID: "credential"}, c.Subject)
				require.Equal(t, def.Policy.Scopes, c.Scopes)
				require.Equal(t, def.Policy.Permissions, c.Permissions)
				require.Equal(t, "requests", c.Metric)
				require.Equal(t, "items.update", c.Fingerprint)
				require.NotEmpty(t, c.Key)
				require.NotEqual(t, "client-controlled-key", c.Key)
			}
			if tc.wantError == nil {
				require.NoError(t, guard.Authorize(policyContext(t, "api"), req, def))
				require.Len(t, store.consumptions, 2)
				require.NotEqual(t, store.consumptions[0].Key, store.consumptions[1].Key, "HTTP admissions must not replay a client key")
			}
		})
	}
}

func TestStrongRevisionSyntax(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		values []string
		want   error
	}{
		{"absent", nil, router.ErrRoutePrecondition},
		{"empty header", []string{""}, router.ErrRouteInvalidRevision},
		{"strong", []string{`"revision-1"`}, nil},
		{"outer whitespace", []string{`  "revision"  `}, nil},
		{"empty strong entity tag", []string{`""`}, router.ErrRouteInvalidRevision},
		{"weak", []string{`W/"revision"`}, router.ErrRouteInvalidRevision},
		{"wildcard", []string{"*"}, router.ErrRouteInvalidRevision},
		{"unquoted", []string{"revision"}, router.ErrRouteInvalidRevision},
		{"list", []string{`"one", "two"`}, router.ErrRouteInvalidRevision},
		{"multiple headers", []string{`"one"`, `"two"`}, router.ErrRouteInvalidRevision},
		{"control", []string{"\"one\x01two\""}, router.ErrRouteInvalidRevision},
		{"internal whitespace", []string{`"one two"`}, router.ErrRouteInvalidRevision},
		{"at bound", []string{`"` + strings.Repeat("a", 254) + `"`}, nil},
		{"over bound", []string{`"` + strings.Repeat("a", 255) + `"`}, router.ErrRouteInvalidRevision},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest("PATCH", "/items", nil)
			for _, v := range tc.values {
				req.Header.Add("If-Match", v)
			}
			require.ErrorIs(t, requireStrongRevision(req), tc.want)
		})
	}
}

func TestRoutePolicyGuardConfiguration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, system string
		nilService   bool
		resources    map[string]ResourceCheck
		wantError    bool
	}{
		{name: "valid", system: "service"},
		{name: "empty system", wantError: true},
		{name: "wildcard system", system: "*", wantError: true},
		{name: "invalid UTF8 system", system: "service\xff", wantError: true},
		{name: "nil service", system: "service", nilService: true, wantError: true},
		{name: "nil resource checker", system: "service", resources: map[string]ResourceCheck{"owner": nil}, wantError: true},
		{name: "invalid checker name", system: "service", resources: map[string]ResourceCheck{"bad name": func(context.Context, *http.Request, string) error { return nil }}, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var service RoutePolicyService = &policyRecorder{}
			if tc.nilService {
				service = nil
			}
			guard, err := NewRoutePolicyGuard(tc.system, service, tc.resources)
			if tc.wantError {
				require.ErrorIs(t, err, router.ErrRouteConfiguration)
				require.Nil(t, guard)
			} else {
				require.NoError(t, err)
				require.NotNil(t, guard)
			}
		})
	}
}

func TestRoutePolicyGuardCurrentAccountBoundary(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, credential, status string
		access                   router.AccessMode
		admin                    bool
		wantError                bool
	}{
		{"ordinary session need not be active", "session", "PROVISIONED", router.Session, false, false},
		{"active session rejects inactive", "session", "PROVISIONED", router.ActiveSession, false, true},
		{"optional active rejects inactive identity", "session", "PROVISIONED", router.OptionalActive, false, true},
		{"API requires active account", "api", "PROVISIONED", router.SessionOrAPI, false, true},
		{"admin session rejects ordinary role", "session", "ACTIVE", router.AdminSession, false, true},
		{"admin API rejects ordinary role", "api", "ACTIVE", router.AdminSessionOrAPI, false, true},
		{"admin session accepts current role", "session", "ACTIVE", router.AdminSession, true, false},
		{"admin API accepts current role", "api", "ACTIVE", router.AdminSessionOrAPI, true, false},
		{"admin role not sufficient for inactive account", "api", "PROVISIONED", router.AdminSessionOrAPI, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := policyContext(t, tc.credential)
			user := helpers.AcquireUserFrom(ctx)
			user.Status = tc.status
			if tc.admin {
				user.Roles = []string{"ADMIN"}
			}
			store := &policyRecorder{}
			guard, err := NewRoutePolicyGuard("service", store, nil)
			require.NoError(t, err)
			err = guard.Authorize(ctx, httptest.NewRequest("GET", "/items", nil), router.RouteDefinition{Access: tc.access})
			if tc.wantError {
				require.ErrorIs(t, err, router.ErrRouteDenied)
			} else {
				require.NoError(t, err)
			}
			require.Empty(t, store.events, "account boundaries precede live grants and usage")
		})
	}
}

func TestRoutePolicyGuardRegisteredResourceChecks(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, check string
		mutate      bool
		wantError   bool
	}{
		{"known", "owner", false, false}, {"unknown", "missing", false, true}, {"map copied", "owner", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			resources := map[string]ResourceCheck{"owner": func(context.Context, *http.Request, string) error { return router.ErrRouteDenied }}
			guard, err := NewRoutePolicyGuard("service", &policyRecorder{}, resources)
			require.NoError(t, err)
			if tc.mutate {
				delete(resources, "owner")
			}
			r := router.NewRouter(nil, nil)
			require.NoError(t, guard.Install(r))
			r.NewRouteGroup("/api", router.Session, func(next http.Handler) http.Handler { return next }).Handle(router.RouteDefinition{Path: "/items", Methods: []string{"GET"}, Operation: "items.read", Policy: router.RoutePolicy{ResourceCheck: tc.check}}, func(http.ResponseWriter, *http.Request) {})
			if tc.wantError {
				require.ErrorIs(t, r.ValidateRoutePolicies(), router.ErrRouteConfiguration)
			} else {
				require.NoError(t, r.ValidateRoutePolicies())
				require.ErrorIs(t, guard.Authorize(policyContext(t, "session"), httptest.NewRequest("GET", "/items", nil), router.RouteDefinition{Access: router.Session, Policy: router.RoutePolicy{ResourceCheck: "owner"}}), router.ErrRouteDenied)
			}
			require.ErrorIs(t, guard.Install(r), router.ErrRouteConfiguration, "configuration must freeze at group creation")
		})
	}
}
