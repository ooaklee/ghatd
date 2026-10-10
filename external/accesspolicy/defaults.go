package accesspolicy

import (
	"context"
	"slices"
	"time"
)

// DefaultAuthorizer applies explicitly configured user capabilities only when
// the policy owner establishes that no grant exists. A stored grant always
// replaces the default in full, including its revocation, expiry and omissions.
// It does not write grants, provide token allowances, or admit quota consumption.
type DefaultAuthorizer struct {
	// service owns the authoritative current grant read.
	service *Service
	// system confines the defaults to one host-owned resource system.
	system string
	// scopes and permissions are private copies of the host's exact defaults.
	scopes, permissions []string
}

// NewDefaultAuthorizer opts one system into explicit default user authority.
// Roles, API tokens and management authority never supply these defaults.
// Existing Service.Authorize remains default-deny. Configure only capabilities
// that every authenticated user may attempt; domain eligibility stays separate.
func NewDefaultAuthorizer(service *Service, system string, scopes, permissions []string) (*DefaultAuthorizer, error) {
	if service == nil || service.store == nil || !validName(system) || len(scopes) == 0 || len(permissions) == 0 || ValidateCapabilities(Capabilities{Scopes: scopes, Permissions: permissions}) != nil {
		return nil, ErrConfiguration
	}
	return &DefaultAuthorizer{service: service, system: system, scopes: slices.Clone(scopes), permissions: slices.Clone(permissions)}, nil
}

// Authorize reads current policy on every invocation. Known absence may use
// the configured defaults; disabled, expired, incomplete or malformed grants
// cannot. Ambiguous storage failures remain errors. The caller must still bind
// the verified user and resource and recheck authority at its command boundary.
func (a *DefaultAuthorizer) Authorize(ctx context.Context, subject Subject, scopes, permissions []string) error {
	if a == nil || a.service == nil {
		return ErrConfiguration
	}
	if err := a.service.checkContext(ctx); err != nil {
		return err
	}
	if !validSubject(subject) || subject.System != a.system || subject.Kind != UserSubject || len(scopes) == 0 || len(permissions) == 0 {
		return ErrDenied
	}
	grant, err := a.service.store.Read(ctx, subject)
	if canceled := ctx.Err(); canceled != nil {
		return canceled
	}
	if isAbsentGrant(err) {
		if includes(a.scopes, scopes) && includes(a.permissions, permissions) {
			return nil
		}
		return ErrDenied
	}
	if err != nil {
		return err
	}
	if err := usableGrant(grant, subject, time.Now()); err != nil {
		return err
	}
	if !includes(grant.Scopes, scopes) || !includes(grant.Permissions, permissions) {
		return ErrDenied
	}
	return nil
}
