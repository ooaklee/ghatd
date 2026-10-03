package accessmanager_test

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/go-redis/redis/v7"
	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/ephemeral"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// sessionAccountStub returns a fresh stored-user observation for each guard call.
type sessionAccountStub struct {
	accessmanager.UserService
	response *userv2.GetUserByIDResponse
	err      error
	reads    int
	cancel   context.CancelFunc
}

func (s *sessionAccountStub) GetUserByID(context.Context, *userv2.GetUserByIDRequest) (*userv2.GetUserByIDResponse, error) {
	s.reads++
	if s.cancel != nil {
		s.cancel()
	}
	return s.response, s.err
}

// sessionStoreStub separates live owner lookup from anonymous rate accounting.
type sessionStoreStub struct {
	accessmanager.EphemeralStore
	owner              string
	err                error
	cancel             context.CancelFunc
	lookups, anonymous int
}

func (s *sessionStoreStub) FetchAuth(context.Context, ephemeral.TokenDetailsAccess) (string, error) {
	s.lookups++
	if s.cancel != nil {
		s.cancel()
	}
	return s.owner, s.err
}
func (s *sessionStoreStub) AddRequestCountEntry(context.Context, string) error {
	s.anonymous++
	if s.cancel != nil {
		s.cancel()
	}
	return s.err
}

func TestOptionalSessionAbsenceBoundary(t *testing.T) {
	mixed := errors.Join(auth.ErrNoBearerHeaderFound, context.DeadlineExceeded)
	for _, tc := range []struct {
		name             string
		cause            error
		cancelAccounting bool
		want             error
		calls            int
	}{
		{"absent", auth.ErrNoBearerHeaderFound, false, nil, 1},
		{"wrapped absent", fmt.Errorf("header: %w", auth.ErrNoBearerHeaderFound), false, nil, 1},
		{"joined absent and outage", mixed, false, mixed, 0},
		{"expired is not absent", auth.ErrUnauthorizedParsedStringTokenExpired, false, auth.ErrUnauthorizedParsedStringTokenExpired, 0},
		{"custom Is cannot turn expiry into absence", misleadingSessionError{auth.ErrUnauthorizedParsedStringTokenExpired}, false, auth.ErrUnauthorizedParsedStringTokenExpired, 0},
		{"canceled accounting", auth.ErrNoBearerHeaderFound, true, context.Canceled, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			store := &sessionStoreStub{}
			if tc.cancelAccounting {
				store.cancel = cancel
			}
			service := &accessmanager.Service{AuthService: &sessionClaimsStub{err: tc.cause}, EphemeralStore: store, StaticPlaceholderUuid: "anonymous"}
			got, err := service.MiddlewareRateLimitOrActiveJWTRequired(httptest.NewRequest("GET", "/", nil).WithContext(ctx))
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, tc.calls, store.anonymous)
			if tc.want != nil {
				require.Nil(t, got)
			} else {
				require.False(t, got.Authenticated)
			}
		})
	}
}

// invokeSessionGuard exercises public entry points with the same identity fixture.
func invokeSessionGuard(s *accessmanager.Service, mode string, ctx context.Context) (*accessmanager.MiddlewareAuthedUserResponse, error) {
	if mode == "explicit" {
		return s.AuthenticateSession(ctx, "selected-credential")
	}
	r := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	switch mode {
	case "active":
		return s.MiddlewareActiveJWTRequired(r)
	case "admin":
		return s.MiddlewareAdminJWTRequired(r)
	case "optional":
		return s.MiddlewareRateLimitOrActiveJWTRequired(r)
	default:
		return s.MiddlewareJWTRequired(r)
	}
}

func TestSessionGuardCurrentAuthority(t *testing.T) {
	for _, mode := range []string{"explicit", "standard", "active", "admin", "optional"} {
		t.Run(mode, func(t *testing.T) {
			for _, tc := range []struct {
				name, variant  string
				want           error
				lookups, reads int
			}{
				{"current active administrator", "", nil, 1, 1},
				{"current roles override false signed flags", "false-flags", nil, 1, 1},
				{"demoted administrator", "demoted", nil, 1, 1},
				{"disabled account", "disabled", nil, 1, 1},
				{"revoked session", "revoked", accessmanager.ErrUnauthorizedTokenNotFoundInStore, 1, 0},
				{"legacy Redis absence", "legacy-absence", accessmanager.ErrUnauthorizedTokenNotFoundInStore, 1, 0},
				{"session store failure", "store-error", context.DeadlineExceeded, 1, 0},
				{"absence mixed with outage", "mixed-absence", context.DeadlineExceeded, 1, 0},
				{"canceled during session read", "cancel-session", context.Canceled, 1, 0},
				{"wrong stored session owner", "wrong-owner", accessmanager.ErrSessionVerificationUnavailable, 1, 0},
				{"missing stored session owner", "empty-owner", accessmanager.ErrSessionVerificationUnavailable, 1, 0},
				{"signature or expiry rejected", "invalid", auth.ErrUnauthorizedParsedStringTokenExpired, 0, 0},
				{"nil claims", "nil-claims", auth.ErrUnauthorized, 0, 0},
				{"missing access identity", "no-session", auth.ErrUnauthorized, 0, 0},
				{"missing user identity", "no-user", auth.ErrUnauthorized, 0, 0},
				{"login proof is not a session", "login-proof", auth.ErrUnauthorized, 0, 0},
				{"verification proof is not a session", "email-proof", auth.ErrUnauthorized, 0, 0},
				{"legacy absent purpose and type", "legacy", nil, 1, 1},
				{"different stored user", "wrong-user", auth.ErrUnauthorized, 1, 1},
				{"nil user response", "nil-response", auth.ErrUnauthorized, 1, 1},
				{"nil stored user", "nil-user", auth.ErrUnauthorized, 1, 1},
				{"email revision changed", "email", accessmanager.ErrOAuthReauthenticationRequired, 1, 1},
				{"user type changed", "type", accessmanager.ErrOAuthReauthenticationRequired, 1, 1},
				{"account dependency failure", "user-error", context.DeadlineExceeded, 1, 1},
				{"canceled before verification", "canceled", context.Canceled, 0, 0},
				{"canceled during account read", "cancel-read", context.Canceled, 1, 1},
			} {
				t.Run(tc.name, func(t *testing.T) {
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					claims := &sessionClaimsStub{details: &auth.TokenAccessDetails{UserID: "member", AccessUUID: "session", EmailRevision: 3, UserType: "person", TokenUse: auth.TokenUseAccess, IsAdmin: true, IsAuthorized: true}}
					user := &userv2.UniversalUser{ID: "member", EmailRevision: 3, Type: "person", Status: userv2.AccountStatusKeyActive, Roles: []string{userv2.UserRoleAdmin}}
					users := &sessionAccountStub{response: &userv2.GetUserByIDResponse{User: user}}
					store := &sessionStoreStub{owner: "member"}
					want := tc.want
					switch tc.variant {
					case "false-flags":
						claims.details.IsAdmin = false
						claims.details.IsAuthorized = false
					case "demoted":
						user.Roles = nil
						if mode == "admin" {
							want = accessmanager.ErrUnauthorizedAdminAccessAttempted
						}
					case "disabled":
						user.Status = userv2.AccountStatusKeySuspended
						if mode == "active" || mode == "admin" || mode == "optional" {
							want = accessmanager.ErrUnauthorizedNonActiveStatus
						}
					case "revoked":
						store.err = ephemeral.ErrAuthNotFound
					case "legacy-absence":
						store.err = redis.Nil
					case "store-error":
						store.err = context.DeadlineExceeded
					case "mixed-absence":
						store.err = errors.Join(ephemeral.ErrAuthNotFound, context.DeadlineExceeded)
					case "cancel-session":
						store.cancel = cancel
					case "wrong-owner":
						store.owner = "other"
					case "empty-owner":
						store.owner = ""
					case "invalid":
						claims.err = auth.ErrUnauthorizedParsedStringTokenExpired
					case "nil-claims":
						claims.details = nil
					case "no-session":
						claims.details.AccessUUID = ""
					case "no-user":
						claims.details.UserID = ""
					case "login-proof":
						claims.details.TokenUse = auth.TokenUseLogin
					case "email-proof":
						claims.details.TokenUse = auth.TokenUseEmailVerification
					case "legacy":
						claims.details.TokenUse = ""
						claims.details.UserType = ""
					case "wrong-user":
						user.ID = "other"
					case "nil-response":
						users.response = nil
					case "nil-user":
						users.response.User = nil
					case "email":
						user.EmailRevision++
					case "type":
						user.Type = "service"
					case "user-error":
						users.err = context.DeadlineExceeded
					case "canceled":
						cancel()
					case "cancel-read":
						users.cancel = cancel
					}
					service := &accessmanager.Service{AuthService: claims, EphemeralStore: store, UserService: users}
					got, err := invokeSessionGuard(service, mode, ctx)
					require.ErrorIs(t, err, want)
					require.Equal(t, tc.lookups, store.lookups)
					require.Equal(t, tc.reads, users.reads)
					require.Zero(t, store.anonymous)
					if tc.variant == "mixed-absence" {
						require.Same(t, store.err, err)
						require.Equal(t, accessmanager.SessionErrorUnknown, accessmanager.ClassifySessionError(err))
					}
					if want != nil {
						require.Nil(t, got)
					} else {
						require.True(t, got.Authenticated)
						require.Equal(t, "member", got.UserID)
						require.Equal(t, claims.details, got.Token)
						require.Equal(t, user, got.User)
					}
					if mode == "explicit" && tc.variant != "canceled" {
						require.Equal(t, "selected-credential", claims.seen)
					}
				})
			}
		})
	}
}

func TestSessionGuardRechecksBetweenRequests(t *testing.T) {
	for _, variant := range []string{"revoke", "demote", "disable"} {
		t.Run(variant, func(t *testing.T) {
			claims := &sessionClaimsStub{details: &auth.TokenAccessDetails{UserID: "member", AccessUUID: "session", IsAdmin: true, IsAuthorized: true}}
			user := &userv2.UniversalUser{ID: "member", Status: userv2.AccountStatusKeyActive, Roles: []string{userv2.UserRoleAdmin}}
			store := &sessionStoreStub{owner: user.ID}
			users := &sessionAccountStub{response: &userv2.GetUserByIDResponse{User: user}}
			service := &accessmanager.Service{AuthService: claims, EphemeralStore: store, UserService: users}
			_, err := invokeSessionGuard(service, "admin", context.Background())
			require.NoError(t, err)
			want := accessmanager.ErrUnauthorizedAdminAccessAttempted
			switch variant {
			case "revoke":
				store.err = ephemeral.ErrAuthNotFound
				want = accessmanager.ErrUnauthorizedTokenNotFoundInStore
			case "demote":
				user.Roles = nil
			case "disable":
				user.Status = userv2.AccountStatusKeySuspended
				want = accessmanager.ErrUnauthorizedNonActiveStatus
			}
			got, err := invokeSessionGuard(service, "admin", context.Background())
			require.ErrorIs(t, err, want)
			require.Nil(t, got)
			require.Equal(t, 2, store.lookups)
		})
	}
}

func TestSessionGuardConfigurationAndAnonymousBoundary(t *testing.T) {
	for _, variant := range []string{"nil service", "nil request", "missing verifier", "missing store", "missing users", "nil explicit context", "empty explicit bearer", "anonymous optional"} {
		t.Run(variant, func(t *testing.T) {
			claims := &sessionClaimsStub{details: &auth.TokenAccessDetails{UserID: "member", AccessUUID: "session"}}
			store := &sessionStoreStub{}
			service := &accessmanager.Service{AuthService: claims, EphemeralStore: store, UserService: &sessionAccountStub{}, StaticPlaceholderUuid: "anonymous"}
			r := httptest.NewRequest("GET", "/", nil)
			want := accessmanager.ErrSessionVerificationUnavailable
			switch variant {
			case "nil service":
				service = nil
			case "nil request":
				r = nil
			case "missing verifier":
				service.AuthService = nil
			case "missing store":
				service.EphemeralStore = nil
			case "missing users":
				service.UserService = nil
			case "empty explicit bearer":
				want = auth.ErrNoBearerHeaderFound
			case "anonymous optional":
				claims.err = auth.ErrNoBearerHeaderFound
				want = nil
			}
			var got *accessmanager.MiddlewareAuthedUserResponse
			var err error
			switch variant {
			case "nil explicit context":
				got, err = service.AuthenticateSession(nil, "selected")
			case "empty explicit bearer":
				got, err = service.AuthenticateSession(context.Background(), "")
			case "anonymous optional":
				got, err = service.MiddlewareRateLimitOrActiveJWTRequired(r)
			default:
				got, err = service.MiddlewareAdminJWTRequired(r)
			}
			require.ErrorIs(t, err, want)
			if want != nil {
				require.Nil(t, got)
			} else {
				require.False(t, got.Authenticated)
				require.Nil(t, got.Token)
				require.Equal(t, 1, store.anonymous)
				require.Zero(t, store.lookups)
			}
		})
	}
}
