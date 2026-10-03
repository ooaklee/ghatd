package accessmanager_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/ephemeral"
	"github.com/ooaklee/ghatd/external/logger"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/reply/v2"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestLogoutTransportRegression(t *testing.T) {
	for _, name := range []string{"bearer dispatch", "refresh failure"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			s := &mockAccessmanagerService{
				logoutUserFunc: func(_ context.Context, r *accessmanager.LogoutUserRequest) error {
					calls++
					if r.RefreshToken != "" {
						return accessmanager.ErrSessionVerificationUnavailable
					}
					return nil
				},
			}
			h := newTestHandler(s)
			r := httptest.NewRequest(http.MethodGet, "/logout", nil)
			want := http.StatusOK
			if name == "bearer dispatch" {
				r.Header.Set("Authorization", "Bearer example-access")
			} else {
				r.AddCookie(&http.Cookie{Name: h.CookiePrefixRefreshToken, Value: "example-refresh"})
				want = http.StatusServiceUnavailable
			}
			w := httptest.NewRecorder()
			h.LogoutUser(w, r)
			require.Equal(t, want, w.Code)
			if name == "bearer dispatch" {
				require.Equal(t, 1, calls)
			}
		})
	}
}

// logoutAuthProbe records purpose-separated verification without claiming that
// a unit fixture verifies cryptography; real JWT coverage is in auth/integration.
type logoutAuthProbe struct {
	accessmanager.AuthService
	access         *auth.TokenAccessDetails
	refresh        *auth.TokenRefreshDetails
	removal        *auth.SessionRemovalDetails
	err            error
	calls          int
	after          context.CancelFunc
	nilRemoval     bool
	foreignRefresh bool
}

func (p *logoutAuthProbe) ExtractSessionRemovalMetadata(_ context.Context, raw, use string) (*auth.SessionRemovalDetails, error) {
	p.calls++
	if p.after != nil {
		p.after()
	}
	if p.err != nil || p.nilRemoval {
		return nil, p.err
	}
	if p.removal != nil {
		return p.removal, nil
	}
	if p.foreignRefresh && use == auth.TokenUseRefresh {
		return &auth.SessionRemovalDetails{UserID: "foreign", TokenID: raw, TokenUse: use}, nil
	}
	return &auth.SessionRemovalDetails{UserID: "owner", TokenID: raw, TokenUse: use}, nil
}
func (p *logoutAuthProbe) ExtractAccessTokenMetadataByString(context.Context, string) (*auth.TokenAccessDetails, error) {
	p.calls++
	if p.after != nil {
		p.after()
	}
	return p.access, p.err
}
func (p *logoutAuthProbe) ExtractRefreshTokenMetadataByString(context.Context, string) (*auth.TokenRefreshDetails, error) {
	p.calls++
	return p.refresh, p.err
}

// logoutStoreProbe records exact retained/deleted keys independently of proof
// validation. Each case owns its receipts, cancellation and mutable slices.
type logoutStoreProbe struct {
	accessmanager.EphemeralStore
	deleted, retained []string
	swept             string
	count             int64
	err               error
	fetchOwner        string
	fetchErr          error
	reads             int
	after             context.CancelFunc
}

func (p *logoutStoreProbe) DeleteAuth(_ context.Context, key string) (int64, error) {
	p.deleted = append(p.deleted, key)
	if p.after != nil {
		p.after()
	}
	return p.count, p.err
}
func (p *logoutStoreProbe) FetchAuth(context.Context, ephemeral.TokenDetailsAccess) (string, error) {
	p.reads++
	if p.after != nil {
		p.after()
	}
	return p.fetchOwner, p.fetchErr
}
func (p *logoutStoreProbe) DeleteAllTokenExceptedSpecified(_ context.Context, owner string, exempt []string) error {
	p.swept = owner
	p.retained = append([]string{}, exempt...)
	if p.after != nil {
		p.after()
	}
	return p.err
}

func TestLogoutManagerCleanup(t *testing.T) {
	driver := errors.New("private-store-error")
	for _, name := range []string{"pair", "access only", "refresh only", "no credentials", "already absent", "store failure", "negative receipt", "large receipt", "wrong owner pair", "nil receipt", "wrong purpose", "bad namespace", "auth failure", "nil request", "nil context", "nil manager", "nil store", "typed nil auth", "unsupported auth", "canceled", "cancel verification", "cancel deletion"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			a := &logoutAuthProbe{}
			store := &logoutStoreProbe{count: 1}
			s := &accessmanager.Service{AuthService: a, EphemeralStore: store}
			r := &accessmanager.LogoutUserRequest{AccessToken: "access", RefreshToken: "refresh"}
			wantCalls := 2
			var want error
			switch name {
			case "access only":
				r.RefreshToken = ""
				wantCalls = 1
			case "refresh only":
				r.AccessToken = ""
				wantCalls = 1
			case "no credentials":
				r.AccessToken = ""
				r.RefreshToken = ""
				s.AuthService = nil
				s.EphemeralStore = nil
				wantCalls = 0
			case "already absent":
				store.count = 0
			case "store failure":
				store.err = driver
				want = driver
			case "negative receipt":
				store.count = -1
				want = accessmanager.ErrSessionVerificationUnavailable
			case "large receipt":
				store.count = 2
				want = accessmanager.ErrSessionVerificationUnavailable
			case "wrong owner pair":
				a.foreignRefresh = true
				want = accessmanager.ErrForbiddenUnableToAction
				wantCalls = 0
			case "nil receipt":
				a.nilRemoval = true
				want = accessmanager.ErrSessionVerificationUnavailable
				wantCalls = 0
			case "wrong purpose":
				a.removal = &auth.SessionRemovalDetails{UserID: "owner", TokenID: "access", TokenUse: auth.TokenUseLogin}
				want = accessmanager.ErrSessionVerificationUnavailable
				wantCalls = 0
			case "bad namespace":
				a.removal = &auth.SessionRemovalDetails{UserID: "owner:foreign", TokenID: "refresh", TokenUse: auth.TokenUseRefresh}
				want = accessmanager.ErrSessionVerificationUnavailable
				wantCalls = 0
			case "auth failure":
				a.err = driver
				want = driver
				wantCalls = 0
			case "nil request":
				r = nil
				want = accessmanager.ErrBadRequest
				wantCalls = 0
			case "nil context":
				ctx = nil
				want = accessmanager.ErrSessionVerificationUnavailable
				wantCalls = 0
			case "nil manager":
				s = nil
				want = accessmanager.ErrSessionVerificationUnavailable
				wantCalls = 0
			case "nil store":
				s.EphemeralStore = (*logoutStoreProbe)(nil)
				want = accessmanager.ErrSessionVerificationUnavailable
				wantCalls = 0
			case "typed nil auth":
				s.AuthService = (*logoutAuthProbe)(nil)
				want = accessmanager.ErrSessionVerificationUnavailable
				wantCalls = 0
			case "unsupported auth":
				s.AuthService = nil
				want = accessmanager.ErrSessionVerificationUnavailable
				wantCalls = 0
			case "canceled":
				cancel()
				want = context.Canceled
				wantCalls = 0
			case "cancel verification":
				a.after = cancel
				want = context.Canceled
				wantCalls = 0
			case "cancel deletion":
				store.after = cancel
				want = context.Canceled
				wantCalls = 1
			}
			var before accessmanager.LogoutUserRequest
			if r != nil {
				before = *r
			}
			err := s.LogoutUser(ctx, r)
			require.ErrorIs(t, err, want)
			require.Len(t, store.deleted, wantCalls)
			if r != nil {
				require.Equal(t, before, *r)
			}
			if name == "pair" {
				require.Equal(t, []string{"owner:refresh", "owner:access"}, store.deleted)
			}
		})
	}
}

func TestLogoutOthersManagerAuthority(t *testing.T) {
	driver := errors.New("private-store-error")
	for _, name := range []string{"owner", "no context", "actor mismatch", "other owner", "api context", "inactive", "revision changed", "wrong supplied session", "access proof", "refresh foreign owner", "refresh proof", "refresh revision", "refresh type", "absent record", "foreign live record", "store read failure", "sweep failure", "canceled", "cancel parse", "cancel fetch", "nil request", "nil context", "nil manager", "typed nil auth", "typed nil store"} {
		t.Run(name, func(t *testing.T) {
			base, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			ctx := tokenSessionContext(base, "owner")
			a := &logoutAuthProbe{access: &auth.TokenAccessDetails{UserID: "owner", AccessUUID: "session", TokenUse: auth.TokenUseAccess}, refresh: &auth.TokenRefreshDetails{UserID: "owner", RefreshUUID: "refresh", TokenUse: auth.TokenUseRefresh}}
			users := &managementUsers{account: &userv2.UniversalUser{ID: "owner", Status: userv2.AccountStatusKeyActive}}
			store := &logoutStoreProbe{fetchOwner: "owner"}
			s := &accessmanager.Service{AuthService: a, EphemeralStore: store, UserService: users}
			r := &accessmanager.LogoutUserOthersRequest{ActorID: "owner", UserID: "owner", AuthToken: "access", RefreshToken: "refresh"}
			var want error
			switch name {
			case "no context":
				ctx = base
				want = accessmanager.ErrUnauthorizedUnableToAttainRequestorID
			case "actor mismatch":
				r.ActorID = "foreign"
				want = accessmanager.ErrUnauthorizedUnableToAttainRequestorID
			case "other owner":
				r.UserID = "foreign"
				want = accessmanager.ErrForbiddenUnableToAction
			case "api context":
				ctx = accesshelpers.TransitSessionWith(ctx, nil)
				want = accessmanager.ErrUnauthorizedUnableToAttainRequestorID
			case "inactive":
				users.account.Status = userv2.AccountStatusKeySuspended
				want = accessmanager.ErrForbiddenUnableToAction
			case "revision changed":
				users.account.EmailRevision = 1
				want = accessmanager.ErrUnauthorizedUnableToAttainRequestorID
			case "wrong supplied session":
				a.access.AccessUUID = "different"
				want = accessmanager.ErrUnauthorizedUnableToAttainRequestorID
			case "access proof":
				a.access.TokenUse = auth.TokenUseLogin
				want = accessmanager.ErrUnauthorizedUnableToAttainRequestorID
			case "refresh foreign owner":
				a.refresh.UserID = "foreign"
				want = accessmanager.ErrUnauthorizedUnableToAttainRequestorID
			case "refresh proof":
				a.refresh.TokenUse = auth.TokenUseLogin
				want = accessmanager.ErrUnauthorizedUnableToAttainRequestorID
			case "refresh revision":
				a.refresh.EmailRevision = 1
				want = accessmanager.ErrUnauthorizedUnableToAttainRequestorID
			case "refresh type":
				a.refresh.UserType = "foreign"
				want = accessmanager.ErrUnauthorizedUnableToAttainRequestorID
			case "absent record":
				store.fetchErr = ephemeral.ErrAuthNotFound
				want = accessmanager.ErrUnauthorizedUnableToAttainRequestorID
			case "foreign live record":
				store.fetchOwner = "foreign"
				want = accessmanager.ErrUnauthorizedUnableToAttainRequestorID
			case "store read failure":
				store.fetchErr = driver
				want = driver
			case "sweep failure":
				store.err = driver
				want = driver
			case "canceled":
				cancel()
				want = context.Canceled
			case "cancel parse":
				a.after = cancel
				want = context.Canceled
			case "cancel fetch":
				store.after = cancel
				want = context.Canceled
			case "nil request":
				r = nil
				want = accessmanager.ErrBadRequest
			case "nil context":
				ctx = nil
				want = accessmanager.ErrSessionVerificationUnavailable
			case "nil manager":
				s = nil
				want = accessmanager.ErrSessionVerificationUnavailable
			case "typed nil auth":
				s.AuthService = (*logoutAuthProbe)(nil)
				want = accessmanager.ErrSessionVerificationUnavailable
			case "typed nil store":
				s.EphemeralStore = (*logoutStoreProbe)(nil)
				want = accessmanager.ErrSessionVerificationUnavailable
			}
			err := s.LogoutUserOthers(ctx, r)
			require.ErrorIs(t, err, want)
			if want == nil || name == "sweep failure" {
				require.Equal(t, "owner", store.swept)
				require.Equal(t, []string{"owner:session", "owner:refresh"}, store.retained)
			} else {
				require.Empty(t, store.swept)
			}
		})
	}
}

func TestLogoutTransportSelection(t *testing.T) {
	for _, name := range []string{"cookies", "bearer", "same bearer cookie", "conflict", "duplicate cookie", "duplicate header", "malformed header", "nil request", "canceled", "forged identity"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			r := httptest.NewRequest("GET", "/logout?ActorID=forged&UserID=foreign", nil).WithContext(tokenSessionContext(ctx, "owner"))
			r.AddCookie(&http.Cookie{Name: "refresh", Value: "refresh"})
			var want error
			if name != "bearer" {
				r.AddCookie(&http.Cookie{Name: "access", Value: "access"})
			}
			switch name {
			case "bearer", "same bearer cookie":
				r.Header.Set("Authorization", "bEaReR access")
			case "conflict":
				r.Header.Set("Authorization", "Bearer foreign")
				want = accessmanager.ErrInvalidAuthToken
			case "duplicate cookie":
				r.AddCookie(&http.Cookie{Name: "access", Value: "access"})
				want = accessmanager.ErrBadRequest
			case "duplicate header":
				r.Header.Add("Authorization", "Bearer access")
				r.Header.Add("Authorization", "Bearer access")
				want = accessmanager.ErrInvalidAuthToken
			case "malformed header":
				r.Header.Set("Authorization", "Basic value")
				want = accessmanager.ErrInvalidAuthToken
			case "nil request":
				r = nil
				want = accessmanager.ErrBadRequest
			case "canceled":
				cancel()
				want = context.Canceled
			}
			var header http.Header
			if r != nil {
				header = r.Header.Clone()
			}
			result, err := accessmanager.MapRequestToLogoutUserRequest(r, "access", "refresh")
			require.ErrorIs(t, err, want)
			if want == nil {
				require.Equal(t, &accessmanager.LogoutUserRequest{AccessToken: "access", RefreshToken: "refresh"}, result)
			} else {
				require.Nil(t, result)
			}
			if r != nil {
				require.Equal(t, header, r.Header)
			}
			if name == "forged identity" {
				other, err := accessmanager.MapRequestToLogoutUserOthersRequest(r, nil, "access", "refresh")
				require.NoError(t, err)
				require.Equal(t, "owner", other.ActorID)
				require.Equal(t, "owner", other.UserID)
				encoded, err := json.Marshal(other)
				require.NoError(t, err)
				require.JSONEq(t, `{}`, string(encoded))
			}
		})
	}
}

func TestLogoutOthersMissingCredential(t *testing.T) {
	for _, tc := range []struct {
		name            string
		access, refresh bool
		want            error
	}{
		{"missing access", false, true, accessmanager.ErrInvalidAuthToken},
		{"missing refresh", true, false, accessmanager.ErrInvalidRefreshToken},
		{"missing both", false, false, accessmanager.ErrInvalidAuthToken},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/logout/other-sessions", nil).WithContext(tokenSessionContext(context.Background(), "owner"))
			if tc.access {
				r.AddCookie(&http.Cookie{Name: "access", Value: "access"})
			}
			if tc.refresh {
				r.AddCookie(&http.Cookie{Name: "refresh", Value: "refresh"})
			}
			value, err := accessmanager.MapRequestToLogoutUserOthersRequest(r, nil, "access", "refresh")
			require.ErrorIs(t, err, tc.want)
			require.Nil(t, value)
		})
	}
}

func TestLogoutHTTPNativeErrors(t *testing.T) {
	for _, other := range []bool{false, true} {
		for _, kind := range []string{"success", "mapped", "override", "unknown", "joined mapped", "joined mixed", "canceled", "nil service"} {
			t.Run(fmt.Sprintf("other=%t/%s", other, kind), func(t *testing.T) {
				core, logs := observer.New(zap.DebugLevel)
				base, cancel := context.WithCancel(logger.TransitWith(context.Background(), zap.New(core)))
				t.Cleanup(cancel)
				var failure error
				want := 200
				if other {
					want = 202
				}
				if kind == "mapped" || kind == "override" {
					failure = fmt.Errorf("private-diagnostic: %w", accessmanager.ErrSessionVerificationUnavailable)
					want = 503
				}
				if kind == "unknown" {
					failure = errors.New("private-diagnostic")
					want = 500
				}
				if kind == "joined mapped" {
					failure = errors.Join(accessmanager.ErrSessionVerificationUnavailable, fmt.Errorf("private-diagnostic: %w", accessmanager.ErrSessionVerificationUnavailable))
					// Authentication's existing CanonicalError contract deliberately
					// rejects every multi-cause tree, including homogeneous joins.
					want = 500
				}
				if kind == "joined mixed" {
					failure = errors.Join(accessmanager.ErrSessionVerificationUnavailable, errors.New("private-diagnostic"))
					want = 500
				}
				s := &mockAccessmanagerService{logoutUserFunc: func(context.Context, *accessmanager.LogoutUserRequest) error { return failure }, logoutUserOthersFunc: func(context.Context, *accessmanager.LogoutUserOthersRequest) error { return failure }}
				h := newTestHandler(s)
				if kind == "override" {
					h = accessmanager.NewHandler(&accessmanager.NewHandlerRequest{Service: s, CookiePrefixAuthToken: "access", CookiePrefixRefreshToken: "refresh", ErrorMaps: []reply.ErrorManifest{{accessmanager.ErrSessionVerificationUnavailable: {Title: "Unavailable", Detail: "Try later", Code: "HOST-1", StatusCode: 502}}}})
					want = 502
				}
				if kind == "nil service" {
					h.Service = (*mockAccessmanagerService)(nil)
					want = 503
				}
				if kind == "canceled" {
					cancel()
					want = 500
				}
				r := httptest.NewRequest("GET", "/logout", nil).WithContext(tokenSessionContext(base, "owner"))
				r.AddCookie(&http.Cookie{Name: h.CookiePrefixAuthToken, Value: "access"})
				r.AddCookie(&http.Cookie{Name: h.CookiePrefixRefreshToken, Value: "refresh"})
				w := httptest.NewRecorder()
				if other {
					h.LogoutUserOthers(w, r)
				} else {
					h.LogoutUser(w, r)
				}
				require.Equal(t, want, w.Code, w.Body.String())
				require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
				require.NotContains(t, w.Body.String(), "private-diagnostic")
				if other {
					require.Empty(t, w.Result().Cookies())
				} else {
					require.NotEmpty(t, w.Result().Cookies())
				}
				for _, entry := range logs.All() {
					require.NotContains(t, entry.Message, "private-diagnostic")
					require.NotContains(t, entry.ContextMap(), "error")
				}
			})
		}
	}
}
