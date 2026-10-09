package partneraccess

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/partnermanager"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/stretchr/testify/require"
)

type identityOwner struct {
	user    *userv2.UniversalUser
	session *accessmanager.MiddlewareAuthedUserResponse
	err     error
	calls   int
	cancel  context.CancelFunc
}

func (o *identityOwner) AuthenticateSession(context.Context, string) (*accessmanager.MiddlewareAuthedUserResponse, error) {
	o.calls++
	if o.cancel != nil {
		o.cancel()
	}
	return o.session, o.err
}
func (o *identityOwner) GetUserByID(context.Context, *userv2.GetUserByIDRequest) (*userv2.GetUserByIDResponse, error) {
	o.calls++
	if o.cancel != nil {
		o.cancel()
	}
	return &userv2.GetUserByIDResponse{User: o.user}, o.err
}

func TestLiveMemberSessionAndWorkerIdentity(t *testing.T) {
	outage := errors.New("account evidence unavailable")
	for _, mode := range []string{"member", "worker"} {
		t.Run(mode, func(t *testing.T) {
			for _, tc := range []struct {
				name, state                string
				want                       error
				ownerCalls, admissionCalls int
			}{
				{"current_account", "", nil, 1, 1}, {"nil_context", "nil-context", partnermanager.ErrDenied, 0, 0}, {"cancelled_context", "cancelled", context.Canceled, 0, 0},
				{"invalid_actor", "actor", partnermanager.ErrDenied, 0, 0}, {"different_owning_id", "id", nil, 1, 0}, {"inactive", "inactive", partnermanager.ErrDenied, 1, 0},
				{"owner_error", "owner", nil, 1, 0}, {"late_owner_cancellation", "owner-cancel", context.Canceled, 1, 0},
				{"admission_denial", "admission-denied", partnermanager.ErrDenied, 1, 1}, {"admission_outage", "admission-outage", outage, 1, 1}, {"late_admission_cancellation", "admission-cancel", context.Canceled, 1, 1},
				{"missing_verification", "verification", nil, 1, 0}, {"unverified", "unverified", nil, 1, 0}, {"wrong_type", "type", nil, 1, 0},
				{"bad_credential", "credential", nil, 0, 0}, {"oversized_credential", "oversize", nil, 0, 0}, {"unauthenticated", "unauthenticated", nil, 1, 0}, {"missing_user", "nil-user", nil, 1, 0},
			} {
				// Select applicable cases before running them; these are separate member
				// and worker protocols, not skipped dependency/integration evidence.
				if mode == "worker" && (tc.state == "verification" || tc.state == "unverified" || tc.state == "credential" || tc.state == "oversize" || tc.state == "unauthenticated") {
					continue
				}
				if mode == "member" && tc.state == "type" {
					continue
				}
				t.Run(tc.name, func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					t.Cleanup(cancel)
					user := &userv2.UniversalUser{ID: "actor", Type: "configured_service", Status: "configured_active", Verification: &userv2.VerificationStatus{EmailVerified: true}}
					owner := &identityOwner{user: user, session: &accessmanager.MiddlewareAuthedUserResponse{Authenticated: true, UserID: "actor", User: user}}
					actor, credential := "actor", "retained_bearer"
					want := tc.want
					switch tc.state {
					case "nil-context":
						ctx = nil
					case "cancelled":
						cancel()
					case "actor":
						actor = "bad actor"
					case "id":
						user.ID = "other"
						if mode == "member" {
							want = partnermanager.ErrDenied
						} else {
							want = partnermanager.ErrUnavailable
						}
					case "inactive":
						user.Status = "inactive"
					case "owner":
						owner.err = outage
						if mode == "member" {
							want = partnermanager.ErrDenied
						} else {
							want = outage
						}
					case "owner-cancel":
						owner.cancel = cancel
					case "verification":
						user.Verification = nil
						want = partnermanager.ErrDenied
					case "unverified":
						user.Verification.EmailVerified = false
						want = partnermanager.ErrDenied
					case "type":
						user.Type = "ordinary"
						want = partnermanager.ErrDenied
					case "credential":
						credential = "bad bearer"
						want = partnermanager.ErrDenied
					case "oversize":
						credential = strings.Repeat("a", 8193)
						want = partnermanager.ErrDenied
					case "unauthenticated":
						owner.session.Authenticated = false
						want = partnermanager.ErrDenied
					case "nil-user":
						owner.user = nil
						owner.session.User = nil
						if mode == "member" {
							want = partnermanager.ErrDenied
						} else {
							want = partnermanager.ErrUnavailable
						}
					}
					reads := 0
					admission := AccountAdmission(func(_ context.Context, id string) error {
						reads++
						require.Equal(t, "actor", id)
						switch tc.state {
						case "admission-denied":
							return partnermanager.ErrDenied
						case "admission-outage":
							return outage
						case "admission-cancel":
							cancel()
						}
						return nil
					})
					var err error
					if mode == "member" {
						v, e := NewMemberSessionVerifier(owner, admission, "configured_active")
						require.NoError(t, e)
						err = v.CheckPartnerSession(ctx, actor, credential)
					} else {
						v, e := NewUserWorkerIdentity(owner, admission, "configured_service", "configured_active")
						require.NoError(t, e)
						err = v.CheckWorkerIdentity(ctx, actor)
					}
					if want == nil {
						require.NoError(t, err)
					} else {
						require.ErrorIs(t, err, want)
					}
					require.Equal(t, tc.ownerCalls, owner.calls)
					require.Equal(t, tc.admissionCalls, reads)
				})
			}
		})
	}
}
func TestIdentityConstructorsAndZeroValuesRejectWithoutIO(t *testing.T) {
	for _, state := range []string{"nil-owner", "typed-owner", "nil-admission", "bad-status", "bad-type", "zero", "nil"} {
		t.Run(state, func(t *testing.T) {
			owner := &identityOwner{}
			var memberSource SessionAuthenticator = owner
			var userSource billingmanager.CheckoutPayerUserService = owner
			admission := AccountAdmission(func(context.Context, string) error { return nil })
			status, kind := "active", "service"
			switch state {
			case "nil-owner":
				memberSource, userSource = nil, nil
			case "typed-owner":
				memberSource, userSource = (*identityOwner)(nil), (*identityOwner)(nil)
			case "nil-admission":
				admission = nil
			case "bad-status":
				status = "bad status"
			case "bad-type":
				kind = "bad type"
			}
			if state == "zero" || state == "nil" {
				member := &MemberSessionVerifier{}
				worker := &UserWorkerIdentity{}
				if state == "nil" {
					member = nil
					worker = nil
				}
				require.ErrorIs(t, member.CheckPartnerSession(t.Context(), "actor", "bearer"), partnermanager.ErrUnavailable)
				require.ErrorIs(t, worker.CheckWorkerIdentity(t.Context(), "actor"), partnermanager.ErrUnavailable)
			} else {
				worker, err := NewUserWorkerIdentity(userSource, admission, kind, status)
				require.Error(t, err)
				require.Nil(t, worker)
				if state != "bad-type" {
					member, e := NewMemberSessionVerifier(memberSource, admission, status)
					require.Error(t, e)
					require.Nil(t, member)
				}
			}
			require.Zero(t, owner.calls)
		})
	}
}
