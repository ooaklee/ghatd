package accessmanager_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/ephemeral"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// loginStateReceipt is a detached recording-port fixture, not Mongo evidence.
func loginStateReceipt(source *user.UniversalUser, activate bool) *user.UniversalUser {
	v := *source
	m := user.UserMetadata{}
	if source.Metadata != nil {
		m = *source.Metadata
	}
	v.Metadata = &m
	m.LastLoginAt, m.LastFreshLoginAt = "2026-10-03T18:00:00Z", "2026-10-03T18:00:00Z"
	if activate {
		v.Status = user.AccountStatusKeyActive
		verification := user.VerificationStatus{}
		if source.Verification != nil {
			verification = *source.Verification
		}
		v.Verification = &verification
		verification.EmailVerified, verification.EmailVerifiedAt = true, m.LastFreshLoginAt
		m.ActivatedAt, m.StatusChangedAt, m.UpdatedAt = m.LastFreshLoginAt, m.LastFreshLoginAt, m.LastFreshLoginAt
	}
	return &v
}

// loginTimeAuth records the exact authentication time passed to the signer.
type loginTimeAuth struct {
	refreshAuthServiceMock
	at time.Time
}

func (a *loginTimeAuth) CreateTokenWithAuthenticationTime(ctx context.Context, v auth.UserModel, at time.Time) (*auth.TokenDetails, error) {
	a.at = at
	return a.CreateToken(ctx, v)
}

func TestLoginStateManagerOrderingAndReceipts(t *testing.T) {
	native := errors.New("private-write-diagnostic")
	type testCase struct {
		name            string
		want            error
		minted, session bool
	}
	for _, flow := range []string{"active", "activation", "verification"} {
		cases := []testCase{
			{"incomplete mint", accessmanager.ErrSessionVerificationUnavailable, true, false},
			{"audit outage", nil, true, true},
			{"success", nil, true, true},
			{"native write", native, false, false},
			{"nil receipt", user.ErrLoginStateUnavailable, false, false},
			{"owner receipt", user.ErrLoginStateUnavailable, false, false},
			{"email receipt", user.ErrLoginStateUnavailable, false, false},
			{"revision receipt", user.ErrLoginStateUnavailable, false, false},
			{"type receipt", user.ErrLoginStateUnavailable, false, false},
			{"status receipt", user.ErrLoginStateUnavailable, false, false},
			{"stamp receipt", user.ErrLoginStateUnavailable, false, false},
			{"non UTC receipt", user.ErrLoginStateUnavailable, false, false},
			{"zero time receipt", user.ErrLoginStateUnavailable, false, false},
			{"mismatched login receipt", user.ErrLoginStateUnavailable, false, false},
			{"nil metadata", user.ErrLoginStateUnavailable, false, false},
			{"cancel write", context.Canceled, false, false},
			{"session outage", native, true, true},
			{"nil mint", accessmanager.ErrSessionVerificationUnavailable, true, false},
			{"mutated request", user.ErrLoginStateUnavailable, false, false},
		}
		if flow != "active" {
			for _, name := range []string{"nil verification", "unverified receipt", "verification time", "activation time", "status time", "updated time"} {
				cases = append(cases, testCase{name, user.ErrLoginStateUnavailable, false, false})
			}
		}
		for _, tc := range cases {
			t.Run(flow+"/"+tc.name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				activate := flow != "active"
				account := user.NewUserFactory(nil).CreateUser("owner@example.test")
				account.ID = "owner"
				account.EmailRevision = 2
				purpose := auth.TokenUseEmailVerification
				if !activate {
					account.Status = user.AccountStatusKeyActive
					purpose = auth.TokenUseLogin
				}
				beforeStatus := account.Status
				var events []string
				port := &refreshUserServiceMock{user: account, updateUserFunc: func(context.Context, *user.UpdateUserRequest) (*user.UpdateUserResponse, error) {
					t.Fatal("broad update invoked")
					return nil, nil
				}}
				transition := func(_ context.Context, r *user.AccountSnapshot) (*user.UniversalUser, error) {
					events = append(events, "write")
					require.Equal(t, "owner", r.UserID)
					require.Equal(t, beforeStatus, r.Status)
					require.Equal(t, int64(2), r.EmailRevision)
					if tc.name == "native write" {
						return nil, native
					}
					v := loginStateReceipt(account, activate)
					v.Roles = []string{"fresh-role"}
					switch tc.name {
					case "nil receipt":
						return nil, nil
					case "owner receipt", "mutated request":
						v.ID = "other"
						r.UserID = "other"
					case "email receipt":
						v.Email = "other@example.test"
					case "revision receipt":
						v.EmailRevision++
					case "type receipt":
						v.Type = "other"
					case "status receipt":
						v.Status = user.AccountStatusKeySuspended
					case "stamp receipt":
						v.Metadata.LastFreshLoginAt = "bad"
					case "non UTC receipt":
						v.Metadata.LastFreshLoginAt = "2026-10-03T19:00:00+01:00"
					case "zero time receipt":
						v.Metadata.LastFreshLoginAt = "0001-01-01T00:00:00Z"
					case "mismatched login receipt":
						v.Metadata.LastLoginAt = "different"
					case "nil verification":
						v.Verification = nil
					case "unverified receipt":
						v.Verification.EmailVerified = false
					case "verification time":
						v.Verification.EmailVerifiedAt = "different"
					case "activation time":
						v.Metadata.ActivatedAt = "different"
					case "status time":
						v.Metadata.StatusChangedAt = "different"
					case "updated time":
						v.Metadata.UpdatedAt = "different"
					case "nil metadata":
						v.Metadata = nil
					case "cancel write":
						cancel()
					}
					return v, nil
				}
				port.recordFreshLoginFunc, port.activateVerifiedEmailFunc = transition, transition
				signer := &loginTimeAuth{refreshAuthServiceMock: refreshAuthServiceMock{
					parseAccessTokenFromStringFunc: func(context.Context, string) (*jwt.Token, error) { return &jwt.Token{Valid: true}, nil },
					checkAccessTokenValidityGetDetails: func(context.Context, *jwt.Token) (*auth.TokenAccessDetails, error) {
						return &auth.TokenAccessDetails{UserID: "owner", AccessUUID: "proof", UserType: account.Type, EmailRevision: 2, TokenUse: purpose}, nil
					},
					createTokenFunc: func(_ context.Context, v auth.UserModel) (*auth.TokenDetails, error) {
						events = append(events, "mint")
						require.Equal(t, []string{"fresh-role"}, v.(*user.UniversalUser).Roles)
						if tc.name == "nil mint" {
							return nil, nil
						}
						if tc.name == "incomplete mint" {
							return &auth.TokenDetails{AccessToken: "only-access"}, nil
						}
						return &auth.TokenDetails{AccessToken: "access", RefreshToken: "refresh", AccessUUID: "session"}, nil
					},
				}}
				audits := 0
				s := &accessmanager.Service{UserService: port, AuthService: signer, AuditService: &loginAuditServiceMock{logAuditEventFunc: func(_ context.Context, r *audit.LogAuditEventRequest) error {
					audits++
					require.Equal(t, "owner", r.TargetId)
					require.Equal(t, audit.UserLogin, r.Action)
					if tc.name == "audit outage" {
						return native
					}
					return nil
				}}, EphemeralStore: &refreshEphemeralStoreMock{
					fetchAuthFunc:  func(context.Context, ephemeral.TokenDetailsAccess) (string, error) { return "owner", nil },
					deleteAuthFunc: func(context.Context, string) (int64, error) { events = append(events, "consume"); return 1, nil },
					createAuthFunc: func(context.Context, string, ephemeral.TokenDetailsAuth) error {
						events = append(events, "session")
						if tc.name == "session outage" {
							return native
						}
						return nil
					},
				}}
				var err error
				if flow == "verification" {
					_, err = s.ValidateEmailVerificationCode(ctx, &accessmanager.ValidateEmailVerificationCodeRequest{Token: "proof"})
				} else {
					_, err = s.LoginUser(ctx, &accessmanager.LoginUserRequest{Token: "proof"})
				}
				require.Equal(t, tc.want, err)
				if tc.want == nil {
					require.Equal(t, 1, audits)
				} else {
					require.Zero(t, audits)
				}
				require.Equal(t, beforeStatus, account.Status, "input snapshot mutated")
				require.Equal(t, []string{"consume", "write"}, events[:2])
				require.Equal(t, tc.minted, containsEvent(events, "mint"))
				require.Equal(t, tc.session, containsEvent(events, "session"))
				if tc.minted {
					require.Equal(t, "2026-10-03T18:00:00Z", signer.at.Format(time.RFC3339Nano))
					require.Equal(t, "mint", events[2])
				}
			})
		}
	}
}

// baseLoginUsers deliberately exposes only the pre-transition manager contract.
// Embedding the interface hides narrow methods even when its value supports them.
type baseLoginUsers struct{ accessmanager.UserService }

func TestLoginStateProofDependencyAdmission(t *testing.T) {
	for _, flow := range []string{"login", "verification"} {
		for _, tc := range []struct {
			name string
			want error
		}{
			{"nil service", accessmanager.ErrSessionVerificationUnavailable},
			{"nil context", accessmanager.ErrSessionVerificationUnavailable},
			{"canceled", context.Canceled},
			{"nil auth", accessmanager.ErrSessionVerificationUnavailable},
			{"typed nil auth", accessmanager.ErrSessionVerificationUnavailable},
			{"nil store", accessmanager.ErrSessionVerificationUnavailable},
			{"typed nil store", accessmanager.ErrSessionVerificationUnavailable},
			{"nil users", accessmanager.ErrSessionVerificationUnavailable},
			{"typed nil users", accessmanager.ErrSessionVerificationUnavailable},
			{"missing narrow capability", user.ErrLoginStateUnavailable},
		} {
			t.Run(flow+"/"+tc.name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				parsed, consumed := 0, 0
				s := &accessmanager.Service{
					UserService: &refreshUserServiceMock{},
					AuthService: &refreshAuthServiceMock{parseAccessTokenFromStringFunc: func(context.Context, string) (*jwt.Token, error) {
						parsed++
						return nil, errors.New("unexpected proof parsing")
					}},
					EphemeralStore: &refreshEphemeralStoreMock{deleteAuthFunc: func(context.Context, string) (int64, error) {
						consumed++
						return 0, errors.New("unexpected proof consumption")
					}},
				}
				switch tc.name {
				case "nil service":
					s = nil
				case "nil context":
					ctx = nil
				case "canceled":
					cancel()
				case "nil auth":
					s.AuthService = nil
				case "typed nil auth":
					s.AuthService = (*refreshAuthServiceMock)(nil)
				case "nil store":
					s.EphemeralStore = nil
				case "typed nil store":
					s.EphemeralStore = (*refreshEphemeralStoreMock)(nil)
				case "nil users":
					s.UserService = nil
				case "typed nil users":
					s.UserService = (*refreshUserServiceMock)(nil)
				case "missing narrow capability":
					s.UserService = &baseLoginUsers{UserService: s.UserService}
				}
				if flow == "login" {
					result, err := s.LoginUser(ctx, &accessmanager.LoginUserRequest{Token: "proof"})
					require.Equal(t, tc.want, err)
					require.Nil(t, result)
				} else {
					result, err := s.ValidateEmailVerificationCode(ctx, &accessmanager.ValidateEmailVerificationCodeRequest{Token: "proof"})
					require.Equal(t, tc.want, err)
					require.Nil(t, result)
				}
				require.Zero(t, parsed, "invalid wiring reached proof admission")
				require.Zero(t, consumed, "invalid wiring consumed a proof")
			})
		}
	}
}
