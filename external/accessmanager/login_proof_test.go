package accessmanager_test

import (
	"context"
	"errors"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/ephemeral"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

// proofAbsenceAlias must not turn a storage outage into an absent credential.
type proofAbsenceAlias struct{ error }

func (e proofAbsenceAlias) Unwrap() error        { return e.error }
func (e proofAbsenceAlias) Is(target error) bool { return target == ephemeral.ErrAuthNotFound }

func TestLoginProofAdmissionAndConsumption(t *testing.T) {
	outage := errors.New("private store diagnostic")
	for _, endpoint := range []string{"login", "email-verification"} {
		for _, tc := range []struct {
			name             string
			want             error
			consumed, minted bool
		}{
			{"accepted", nil, true, true},
			{"legacy purpose missing", auth.ErrUnauthorized, false, false},
			{"session purpose", auth.ErrUnauthorized, false, false},
			{"refresh purpose", auth.ErrUnauthorized, false, false},
			{"unknown purpose", auth.ErrUnauthorized, false, false},
			{"wrong type", accessmanager.ErrOAuthReauthenticationRequired, false, false},
			{"wrong revision", accessmanager.ErrOAuthReauthenticationRequired, false, false},
			{"nil account result", accessmanager.ErrSessionVerificationUnavailable, false, false},
			{"wrong account ID", accessmanager.ErrSessionVerificationUnavailable, false, false},
			{"wrong cache owner", accessmanager.ErrSessionVerificationUnavailable, false, false},
			{"nil token", accessmanager.ErrSessionVerificationUnavailable, false, false},
			{"nil details", accessmanager.ErrSessionVerificationUnavailable, false, false},
			{"missing token ID", accessmanager.ErrSessionVerificationUnavailable, false, false},
			{"absent proof", accessmanager.ErrUnauthorizedTokenNotFoundInStore, false, false},
			{"cache outage", outage, false, false},
			{"absence alias over outage", outage, false, false},
			{"joined absence outage", outage, false, false},
			{"lost consumption race", accessmanager.ErrUnauthorizedTokenNotFoundInStore, true, false},
			{"invalid deletion count", accessmanager.ErrSessionVerificationUnavailable, true, false},
			{"uncertain consumption", outage, true, false},
			{"account write outage", outage, true, endpoint == "login"},
			{"nil mint result", accessmanager.ErrSessionVerificationUnavailable, true, true},
			{"cancel after lookup", context.Canceled, false, false},
			{"cancel after account", context.Canceled, false, false},
			{"cancel after consumption", context.Canceled, true, false},
			{"cancel during mint", context.Canceled, true, true},
		} {
			t.Run(endpoint+"/"+tc.name, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				account := user.NewUserFactory(nil).CreateUser("proof@example.test")
				account.ID, account.Type, account.EmailRevision = "member", "person", 2
				account.Status = user.AccountStatusKeyProvisioned
				purpose := auth.TokenUseEmailVerification
				if endpoint == "login" {
					account.Status, purpose = user.AccountStatusKeyActive, auth.TokenUseLogin
				}
				details := &auth.TokenAccessDetails{UserID: account.ID, AccessUUID: "proof", UserType: account.Type, TokenUse: purpose, EmailRevision: 2}
				switch tc.name {
				case "legacy purpose missing":
					details.TokenUse = ""
				case "session purpose":
					details.TokenUse = auth.TokenUseAccess
				case "refresh purpose":
					details.TokenUse = auth.TokenUseRefresh
				case "unknown purpose":
					details.TokenUse = "unknown"
				case "wrong type":
					account.Type = "service"
				case "wrong revision":
					account.EmailRevision++
				case "wrong account ID":
					account.ID = "other"
				case "missing token ID":
					details.AccessUUID = ""
				}
				var events []string
				s := &accessmanager.Service{AuditService: &loginAuditServiceMock{}}
				s.AuthService = &refreshAuthServiceMock{
					parseAccessTokenFromStringFunc: func(context.Context, string) (*jwt.Token, error) {
						if tc.name == "nil token" {
							return nil, nil
						}
						return &jwt.Token{Valid: true}, nil
					},
					checkAccessTokenValidityGetDetails: func(context.Context, *jwt.Token) (*auth.TokenAccessDetails, error) {
						if tc.name == "nil details" {
							return nil, nil
						}
						return details, nil
					},
					createTokenFunc: func(context.Context, auth.UserModel) (*auth.TokenDetails, error) {
						events = append(events, "mint")
						if tc.name == "nil mint result" {
							return nil, nil
						}
						if tc.name == "cancel during mint" {
							cancel()
						}
						return &auth.TokenDetails{AccessToken: "access", RefreshToken: "refresh", AccessUUID: "new-session"}, nil
					},
				}
				s.EphemeralStore = &refreshEphemeralStoreMock{
					fetchAuthFunc: func(context.Context, ephemeral.TokenDetailsAccess) (string, error) {
						switch tc.name {
						case "wrong cache owner":
							return "other", nil
						case "absent proof":
							return "", ephemeral.ErrAuthNotFound
						case "cache outage":
							return "", outage
						case "absence alias over outage":
							return "", proofAbsenceAlias{outage}
						case "joined absence outage":
							return "", errors.Join(ephemeral.ErrAuthNotFound, outage)
						case "cancel after lookup":
							cancel()
						}
						return "member", nil
					},
					deleteAuthFunc: func(_ context.Context, key string) (int64, error) {
						require.Equal(t, "member:proof", key)
						events = append(events, "consume")
						switch tc.name {
						case "lost consumption race":
							return 0, nil
						case "invalid deletion count":
							return 2, nil
						case "uncertain consumption":
							return 1, outage
						case "cancel after consumption":
							cancel()
						}
						return 1, nil
					},
					createAuthFunc: func(context.Context, string, ephemeral.TokenDetailsAuth) error {
						events = append(events, "session")
						return nil
					},
				}
				s.UserService = &refreshUserServiceMock{
					getUserByIDFunc: func(context.Context, *user.GetUserByIDRequest) (*user.GetUserByIDResponse, error) {
						if tc.name == "nil account result" {
							return nil, nil
						}
						if tc.name == "cancel after account" {
							cancel()
						}
						return &user.GetUserByIDResponse{User: account}, nil
					},
					updateUserFunc: func(_ context.Context, req *user.UpdateUserRequest) (*user.UpdateUserResponse, error) {
						events = append(events, "update")
						if tc.name == "account write outage" {
							return nil, outage
						}
						return &user.UpdateUserResponse{User: req.User}, nil
					},
				}
				var err error
				if endpoint == "login" {
					_, err = s.LoginUser(ctx, &accessmanager.LoginUserRequest{Token: "signed-proof"})
				} else {
					_, err = s.ValidateEmailVerificationCode(ctx, &accessmanager.ValidateEmailVerificationCodeRequest{Token: "signed-proof"})
				}
				require.ErrorIs(t, err, tc.want)
				require.Equal(t, tc.consumed, containsEvent(events, "consume"), events)
				require.Equal(t, tc.minted, containsEvent(events, "mint"), events)
				if tc.consumed {
					require.Equal(t, "consume", events[0])
				}
				if tc.want != nil {
					require.NotContains(t, events, "session")
				} else {
					require.Contains(t, events, "session")
				}
			})
		}
	}
}

// containsEvent keeps the admission assertions independent from success order.
func containsEvent(events []string, want string) bool {
	for _, event := range events {
		if event == want {
			return true
		}
	}
	return false
}
