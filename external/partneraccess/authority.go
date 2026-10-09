// Package partneraccess adapts the host's live session and shared access-policy
// services to Partners authority. It owns no grants, roles or financial state.
package partneraccess

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/partnerprogram"
)

const (
	// SelfScope permits only the verified account's own customer use cases.
	SelfScope = "partners.self"
	// ProgramScope is an explicit program-wide resource grant. A permission for
	// one action is still required; this scope does not confer other actions.
	ProgramScope = "partners.program"
)

// SessionVerifier rechecks the credential's live identity, verification and
// account-deletion admission. It must never accept stored role claims alone.
type SessionVerifier interface {
	CheckPartnerSession(context.Context, string, string) error
}

// PolicyService is the existing access-policy owner's current enforcement API.
// The adapter neither reads its store nor synthesizes grants from account roles.
type PolicyService interface {
	Authorize(context.Context, accesspolicy.Subject, []string, []string) error
}

type verifiedSession struct{ actor, credential string }
type sessionKey struct{}

// WithVerifiedSession carries credentials only between trusted host boundaries.
// Call it after resolving the current authenticated principal, never from a body
// actor. The private context key and value are not transport fields. Authority
// independently rechecks the live credential on every call, including replay.
func WithVerifiedSession(ctx context.Context, actor, credential string) (context.Context, error) {
	if ctx == nil || !validID(actor) || credential == "" || len(credential) > 8192 || strings.ContainsAny(credential, " \t\r\n\x00") {
		return nil, partnermanager.ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return context.WithValue(ctx, sessionKey{}, verifiedSession{actor, credential}), nil
}

// Authority checks exact current action and resource grants for human callers.
// Background workers require a separate trusted service authority; a browser
// session cannot become a worker merely by supplying a worker actor or action.
type Authority struct {
	system   string
	sessions SessionVerifier
	policy   PolicyService
}

func NewAuthority(system string, sessions SessionVerifier, policy PolicyService) (*Authority, error) {
	if !validID(system) || nilPort(sessions) || nilPort(policy) {
		return nil, partnermanager.ErrUnavailable
	}
	return &Authority{system: system, sessions: sessions, policy: policy}, nil
}

// TargetScope binds an explicit operator grant to one action and selected
// resource. Hashing prevents punctuation, length or namespace ambiguity in
// IDs from creating another scope. Empty targets use ProgramScope instead.
func TargetScope(capability, target string) (string, error) {
	if !operatorCapability(capability) || !validID(target) {
		return "", partnermanager.ErrDenied
	}
	digest := sha256.Sum256([]byte(capability + "\x00" + target))
	return "partners.target." + hex.EncodeToString(digest[:]), nil
}

// OperatorTargetValid recognizes the human action/target forms enforced by
// Authority. It checks shape only, never grants permission or resource existence.
// Policy and processing allow an empty program-list target; operations requires
// the owning program; other actions require an exact selected opaque ID.
func OperatorTargetValid(capability, target string) bool {
	if !operatorCapability(capability) {
		return false
	}
	if target == "" {
		return capability == partnermanager.CapabilityPolicy || capability == partnermanager.CapabilityProcessing
	}
	if !validID(target) {
		return false
	}
	return capability != partnermanager.CapabilityOperations || target == partnerprogram.ProgramID
}

func (a *Authority) CheckPartners(ctx context.Context, actor, capability, target string) error {
	if ctx == nil {
		return partnermanager.ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if a == nil || nilPort(a.sessions) || nilPort(a.policy) {
		return partnermanager.ErrUnavailable
	}
	session, ok := ctx.Value(sessionKey{}).(verifiedSession)
	if !ok || session.actor != actor || !validID(actor) {
		return partnermanager.ErrDenied
	}
	scope := SelfScope
	if customerCapability(capability) {
		if target != actor {
			return partnermanager.ErrDenied
		}
	} else if operatorCapability(capability) {
		if !OperatorTargetValid(capability, target) {
			return partnermanager.ErrDenied
		}
		if target == "" || capability == partnermanager.CapabilityOperations {
			scope = ProgramScope
		} else {
			var err error
			scope, err = TargetScope(capability, target)
			if err != nil {
				return err
			}
		}
	} else {
		return partnermanager.ErrDenied
	}
	if err := a.sessions.CheckPartnerSession(ctx, actor, session.credential); err != nil {
		return err
	}
	subject := accesspolicy.Subject{System: a.system, Kind: accesspolicy.UserSubject, ID: actor}
	err := a.policy.Authorize(ctx, subject, []string{scope}, []string{capability})
	// Program-wide authority is an explicit alternative resource scope, not an
	// administrator/wildcard fallback. Only a sole known denial permits checking
	// it: a joined denial plus outage cannot be reclassified as missing scope.
	if operatorCapability(capability) && scope != ProgramScope && soleDenial(err) {
		err = a.policy.Authorize(ctx, subject, []string{ProgramScope}, []string{capability})
	}
	if err != nil {
		if soleDenial(err) {
			return partnermanager.ErrDenied
		}
		return err
	}
	// A grant read must not return success after the session was revoked while
	// policy I/O was running. The manager also invokes this port on its rechecks.
	if err := a.sessions.CheckPartnerSession(ctx, actor, session.credential); err != nil {
		return err
	}
	return ctx.Err()
}

func customerCapability(capability string) bool {
	switch capability {
	case partnermanager.CapabilitySelf, partnermanager.CapabilityEnroll, partnermanager.CapabilityClaims:
		return true
	}
	return false
}

func operatorCapability(capability string) bool {
	switch capability {
	case partnermanager.CapabilityCreateClaimOnBehalf, partnermanager.CapabilityPolicy,
		partnermanager.CapabilityReporting, partnermanager.CapabilityOperations,
		partnermanager.CapabilityAttribution, partnermanager.CapabilityProcessing,
		partnermanager.CapabilityRecordPayment, partnermanager.CapabilityAmendPayment,
		partnermanager.CapabilityReturnPayment:
		return true
	}
	return false
}

func validID(value string) bool {
	return value != "" && len(value) <= 256 && utf8.ValidString(value) && strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}

func nilPort(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

func soleDenial(err error) bool {
	for depth := 0; err != nil && depth < 32; depth++ {
		if err == accesspolicy.ErrDenied {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

var _ partnermanager.Authority = (*Authority)(nil)
