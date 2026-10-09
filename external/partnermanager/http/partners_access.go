package partnerhttp

import (
	"context"
	"errors"
	"reflect"

	"github.com/ooaklee/ghatd/external/partneraccess"
	"github.com/ooaklee/ghatd/external/partnermanager"
)

// partnersOperatorAccess projects one current human action/target check. It
// reads no financial resource and creates no grant or reusable authorization.
// Every owning command must still perform its own current authorization.
func (m *Service) partnersOperatorAccess(ctx context.Context, p Principal, req Request) (Response, error) {
	capability, target, err := partnerOperatorQuery(req)
	if err != nil {
		return Response{}, err
	}
	if missingPartnersAccessPort(m.sessions) || missingPartnersAccessPort(m.authority) {
		return Response{}, partnermanager.ErrUnavailable
	}
	// Rebind from the resolved principal at this boundary too: an unbound or
	// stale inherited context cannot be mistaken for a current policy denial.
	ctx, err = partneraccess.WithVerifiedSession(ctx, p.ActorID, p.Credential)
	if err != nil {
		return Response{}, err
	}
	if err := m.sessions.CheckPartnerSession(ctx, p.ActorID, p.Credential); err != nil {
		return Response{}, err
	}
	err = m.authority.CheckPartners(ctx, p.ActorID, capability, target)
	allowed := err == nil
	// Only a sole canonical authority denial may become allowed=false. Joined
	// failures, session errors and unknown causes keep their error status.
	if !allowed && !solePartnersAccessDenial(err) {
		return Response{}, err
	}
	// Authority also verifies sessions, but a denied policy read may return
	// early. A revoked credential must never become a successful denied view.
	if err := m.sessions.CheckPartnerSession(ctx, p.ActorID, p.Credential); err != nil {
		return Response{}, err
	}
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	return reply(200, map[string]any{"capability": capability, "target": target, "allowed": allowed}, "")
}

// partnerOperatorQuery limits introspection to the exact human capabilities
// and target forms understood by partneraccess.Authority. Empty targets probe
// policy/processing program lists; they do not enumerate selected-target grants.
func partnerOperatorQuery(req Request) (string, string, error) {
	invalid := fail("PARTNERS_INVALID_REQUEST", 400)
	for key, values := range req.Query {
		if (key != "capability" && key != "target") || len(values) != 1 {
			return "", "", invalid
		}
	}
	capability, err := partnerQueryText(req, "capability", 64)
	if err != nil {
		return "", "", err
	}
	target, err := partnerQueryText(req, "target", 256)
	if err != nil {
		return "", "", err
	}
	if !partneraccess.OperatorTargetValid(capability, target) {
		return "", "", invalid
	}
	return capability, target, nil
}

func solePartnersAccessDenial(err error) bool {
	for depth := 0; err != nil && depth < 32; depth++ {
		if err == partnermanager.ErrDenied {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

func missingPartnersAccessPort(value any) bool {
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
