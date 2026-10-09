package partnermanagerhelper

import (
	"context"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ooaklee/ghatd/external/partneraccess"
	"github.com/ooaklee/ghatd/external/partnermanager"
)

// AccountIdentity applies the host's durable deletion admission to
// selected principals too, including admin-selected referred/owner accounts
// without their sessions. It never interprets deletion as absent financial
// history. Immutable signup evidence remains an owning-service delegation.
type AccountIdentity struct {
	owner     partnermanager.Identity
	admission partneraccess.AccountAdmission
}

func (i *AccountIdentity) GetPartnerPrincipal(ctx context.Context, customerID string) (partnermanager.Principal, error) {
	if ctx == nil || !validAccountID(customerID) {
		return partnermanager.Principal{}, partnermanager.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return partnermanager.Principal{}, err
	}
	if i == nil || nilHelperPort(i.owner) || i.admission == nil {
		return partnermanager.Principal{}, partnermanager.ErrUnavailable
	}
	principal, err := i.owner.GetPartnerPrincipal(ctx, customerID)
	if err := ctx.Err(); err != nil {
		return partnermanager.Principal{}, err
	}
	if err != nil {
		return partnermanager.Principal{}, err
	}
	if principal.ID != customerID {
		return partnermanager.Principal{}, partnermanager.ErrUnavailable
	}
	if err := i.admission(ctx, customerID); err != nil {
		return partnermanager.Principal{}, err
	}
	if err := ctx.Err(); err != nil {
		return partnermanager.Principal{}, err
	}
	return principal, nil
}

func (i *AccountIdentity) GetSignupFact(ctx context.Context, customerID string) (partnermanager.SignupFact, error) {
	if ctx == nil || !validAccountID(customerID) {
		return partnermanager.SignupFact{}, partnermanager.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return partnermanager.SignupFact{}, err
	}
	if i == nil || nilHelperPort(i.owner) {
		return partnermanager.SignupFact{}, partnermanager.ErrUnavailable
	}
	// Do not bind immutable capture recovery to mutable host account availability.
	fact, err := i.owner.GetSignupFact(ctx, customerID)
	if err := ctx.Err(); err != nil {
		return partnermanager.SignupFact{}, err
	}
	if err != nil {
		return partnermanager.SignupFact{}, err
	}
	return fact, nil
}

// NewAccountIdentity adds explicit live host admission to selected principals.
// Immutable signup evidence deliberately delegates without mutable admission.
func NewAccountIdentity(owner partnermanager.Identity, admission partneraccess.AccountAdmission) (*AccountIdentity, error) {
	if nilHelperPort(owner) || admission == nil {
		return nil, partnermanager.ErrUnavailable
	}
	return &AccountIdentity{owner: owner, admission: admission}, nil
}
func validAccountID(value string) bool {
	return value != "" && len(value) <= 256 && utf8.ValidString(value) && strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}

var _ partnermanager.Identity = (*AccountIdentity)(nil)
