package partneraccess

import (
	"context"
	"strings"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/partnermanager"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
)

// AccountAdmission checks current host account restrictions for the selected
// identity. It must fail closed on unknown/unavailable evidence, not delete or
// reinterpret financial history. Hosts may explicitly supply an unrestricted
// policy; nil is never implicit admission. No private worker context is granted.
type AccountAdmission func(context.Context, string) error

// SessionAuthenticator supplies a current owning member session directly from
// its bearer. It never synthesizes an HTTP request or trusts a decoded actor.
type SessionAuthenticator interface {
	// AuthenticateSession resolves the current owning member session for a bearer
	// credential, returning the authenticated user response.
	AuthenticateSession(context.Context, string) (*accessmanager.MiddlewareAuthedUserResponse, error)
}

// MemberSessionVerifier combines current owning authentication, verified email
// and an explicit host account-admission hook. It supplies no action grants.
type MemberSessionVerifier struct {
	members      SessionAuthenticator
	admission    AccountAdmission
	activeStatus string
}

// NewMemberSessionVerifier validates borrowed ports without authenticating.
func NewMemberSessionVerifier(members SessionAuthenticator, admission AccountAdmission, activeStatus string) (*MemberSessionVerifier, error) {
	if nilPort(members) || admission == nil {
		return nil, partnermanager.ErrUnavailable
	}
	if !validID(activeStatus) {
		return nil, partnermanager.ErrInvalid
	}
	return &MemberSessionVerifier{members, admission, activeStatus}, nil
}

// CheckPartnerSession authenticates the credential with the owning member
// service and requires the returned identity to match the actor with active
// status and verified email. Malformed actor or credential shapes are denials;
// missing dependencies are unavailable; context errors are returned directly.
// The host admission hook runs last and its error is returned.
func (v *MemberSessionVerifier) CheckPartnerSession(ctx context.Context, actor, credential string) error {
	if ctx == nil {
		return partnermanager.ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if v == nil || nilPort(v.members) || v.admission == nil {
		return partnermanager.ErrUnavailable
	}
	if !validID(actor) || credential == "" || len(credential) > 8192 || strings.ContainsAny(credential, " \t\r\n\x00") {
		return partnermanager.ErrDenied
	}
	member, err := v.members.AuthenticateSession(ctx, credential)
	if e := ctx.Err(); e != nil {
		return e
	}
	if err != nil || member == nil || !member.Authenticated || member.UserID != actor || member.User == nil || member.User.ID != actor || member.User.Status != v.activeStatus || member.User.Verification == nil || !member.User.Verification.EmailVerified {
		return partnermanager.ErrDenied
	}
	if err := v.admission(ctx, actor); err != nil {
		return err
	}
	return ctx.Err()
}

// UserWorkerIdentity checks a configured account type/status through user/v2
// and the host's current account admission. A stored grant is not identity.
type UserWorkerIdentity struct {
	users                     billingmanager.CheckoutPayerUserService
	admission                 AccountAdmission
	accountType, activeStatus string
}

// NewUserWorkerIdentity validates the borrowed user service, admission hook and
// the configured account type/status strings, returning unavailable or invalid
// rather than constructing around invalid configuration.
func NewUserWorkerIdentity(users billingmanager.CheckoutPayerUserService, admission AccountAdmission, accountType, activeStatus string) (*UserWorkerIdentity, error) {
	if nilPort(users) || admission == nil {
		return nil, partnermanager.ErrUnavailable
	}
	if !validID(accountType) || !validID(activeStatus) {
		return nil, partnermanager.ErrInvalid
	}
	return &UserWorkerIdentity{users, admission, accountType, activeStatus}, nil
}

// CheckWorkerIdentity reads the account through user/v2 and requires a matching
// ID with the configured type and active status, then runs the host admission
// hook. Shape failures are denials, lookup inconsistencies are unavailable, and
// context errors are returned directly.
func (i *UserWorkerIdentity) CheckWorkerIdentity(ctx context.Context, actor string) error {
	if ctx == nil || !validID(actor) {
		return partnermanager.ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if i == nil || nilPort(i.users) || i.admission == nil {
		return partnermanager.ErrUnavailable
	}
	response, err := i.users.GetUserByID(ctx, &userv2.GetUserByIDRequest{ID: actor})
	if e := ctx.Err(); e != nil {
		return e
	}
	if err != nil {
		return err
	}
	if response == nil || response.User == nil || response.User.ID != actor {
		return partnermanager.ErrUnavailable
	}
	if response.User.Type != i.accountType || response.User.Status != i.activeStatus {
		return partnermanager.ErrDenied
	}
	if err := i.admission(ctx, actor); err != nil {
		return err
	}
	return ctx.Err()
}

var _ SessionVerifier = (*MemberSessionVerifier)(nil)
var _ WorkerIdentity = (*UserWorkerIdentity)(nil)
