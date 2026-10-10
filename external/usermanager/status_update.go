package usermanager

import (
	"context"
	"strings"

	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/logger"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/reply/v2"
)

// AdministratorAuthorizer revalidates a trusted session against live authority.
// It must reject API, mixed, anonymous, revoked and stale-identity contexts.
type AdministratorAuthorizer interface {
	// AuthorizeAdministrator revalidates a trusted session context against live
	// authority, returning the verified administrator identity; implementers must
	// reject API, mixed, anonymous, revoked and stale-identity contexts.
	AuthorizeAdministrator(context.Context) (string, error)
	// AdministratorErrorMaps preserves the verifier's native error contracts.
	AdministratorErrorMaps() []reply.ErrorManifest
}

// AccountStatusService is the narrow configured domain capability. It validates
// receipts and preserves native errors; a broad UpdateUser is not a fallback.
type AccountStatusService interface {
	// UpdateUserStatus applies a status change to the targeted user; the
	// usermanager Service adapts the domain payload, binding the authenticated
	// caller as actor before delegating to ChangeAccountStatus.
	UpdateUserStatus(context.Context, *user.UpdateUserStatusRequest) (*user.UpdateUserStatusResponse, error)
}

// ChangeAccountStatusRequest keeps the verified actor independent of the target.
// ActorID is bound by trusted code, never by JSON, form or query decoding.
type ChangeAccountStatusRequest struct {
	ActorID string `json:"-" query:"-" form:"-"`
	// TargetUserID selects the account; it may equal ActorID under existing policy.
	TargetUserID string
	// DesiredStatus selects a configured transition, including EMAIL_CHANGE.
	DesiredStatus string
}

// WithAdministratorAuthorizer installs the shared live verifier at startup.
// It does not replace route middleware; management rechecks authority afterward.
func (s *Service) WithAdministratorAuthorizer(authorizer AdministratorAuthorizer) *Service {
	s.administratorAuthorizer = authorizer
	return s
}

// StatusManagerErrorMaps keeps manager/domain/verifier failures on one manifest
// chain. Handler-supplied overrides are applied after these canonical maps.
func (s *Service) StatusManagerErrorMaps() []reply.ErrorManifest {
	c := errormanifest.NewComposer().Add(UsermanagerErrorMap).Add(DependencyErrorMaps()...)
	if s != nil && !nilProfilePort(s.administratorAuthorizer) {
		c.Add(s.administratorAuthorizer.AdministratorErrorMaps()...)
	}
	return c.Build()
}

// administratorActor validates wiring and obtains independent current administrator
// authority. Helpers only bind a prior verified identity; they do not verify it.
// unavailable is the caller's canonical missing-capability error, not a wrapper
// for native authorization failures, which are always returned unchanged.
func (s *Service) administratorActor(ctx context.Context, actor string, unavailable error) error {
	if s == nil || ctx == nil || nilProfilePort(s.UserService) || nilProfilePort(s.administratorAuthorizer) {
		return unavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if strings.TrimSpace(actor) == "" || helpers.AcquireAuthenticatedUserIDFrom(ctx) != actor {
		return ErrUnableToIdentifyUser
	}
	verified, err := s.administratorAuthorizer.AuthorizeAdministrator(ctx)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if verified != actor {
		return ErrUnableToIdentifyUser
	}
	return nil
}

// ChangeAccountStatus authorizes, delegates to the domain and audits the actual
// persisted destination. Audit delivery is best effort after an acknowledged
// write; an audit outage never requests replay. Authority and storage are not one
// transaction, so revocation concurrent with an admitted write is not a lock.
func (s *Service) ChangeAccountStatus(ctx context.Context, req *ChangeAccountStatusRequest) (*user.UpdateUserStatusResponse, error) {
	if req == nil {
		return nil, ErrInvalidUserBody
	}
	r := *req
	if err := s.administratorActor(ctx, r.ActorID, user.ErrStatusUpdateUnavailable); err != nil {
		return nil, err
	}
	if strings.TrimSpace(r.TargetUserID) == "" || strings.TrimSpace(r.DesiredStatus) == "" {
		return nil, ErrInvalidUserBody
	}
	domain, ok := s.UserService.(AccountStatusService)
	if !ok || nilProfilePort(domain) {
		return nil, user.ErrStatusUpdateUnavailable
	}
	response, err := domain.UpdateUserStatus(ctx, &user.UpdateUserStatusRequest{ID: r.TargetUserID, DesiredStatus: r.DesiredStatus})
	if err != nil {
		return nil, err
	}
	if response == nil || response.User == nil || response.User.ID != r.TargetUserID || response.User.Status == "" || response.User.Status == "EMAIL_CHANGE" || (r.DesiredStatus != "EMAIL_CHANGE" && response.User.Status != r.DesiredStatus) {
		return nil, user.ErrStatusUpdateUnavailable
	}
	if !nilProfilePort(s.AuditService) {
		if err := s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{ActorId: r.ActorID, Action: "user.status_updated", TargetId: r.TargetUserID, TargetType: audit.TargetTypeUser, Details: map[string]interface{}{"requested_transition": r.DesiredStatus, "new_status": response.User.Status}}); err != nil {
			logger.AcquireOperationFrom(ctx, "external/usermanager", "change-account-status").Warn("status-audit-delivery-failed")
		}
	}
	return response, nil
}

// UpdateUserStatus adapts the existing HTTP domain-shaped payload to an explicit
// manager request. No body field can select the actor or supply authority.
func (s *Service) UpdateUserStatus(ctx context.Context, req *user.UpdateUserStatusRequest) (*user.UpdateUserStatusResponse, error) {
	if ctx == nil {
		return nil, user.ErrStatusUpdateUnavailable
	}
	if req == nil {
		return nil, ErrInvalidUserBody
	}
	return s.ChangeAccountStatus(ctx, &ChangeAccountStatusRequest{ActorID: helpers.AcquireAuthenticatedUserIDFrom(ctx), TargetUserID: req.ID, DesiredStatus: req.DesiredStatus})
}

// BulkUpdateUsersStatus preserves the existing ordered, non-atomic partial-result
// contract. Initial authority failure returns its native error; each item then
// rechecks authority. Item failures (including cancellation/revocation) appear in
// FailedIDs, never as automatic retries. UpdatedCount counts acknowledged writes,
// not a transaction; callers reload failed/uncertain accounts before retrying.
func (s *Service) BulkUpdateUsersStatus(ctx context.Context, req *user.BulkUpdateUsersStatusRequest) (*user.BulkUpdateUsersStatusResponse, error) {
	if ctx == nil {
		return nil, user.ErrStatusUpdateUnavailable
	}
	if req == nil {
		return nil, ErrInvalidUserBody
	}
	actor := helpers.AcquireAuthenticatedUserIDFrom(ctx)
	if err := s.administratorActor(ctx, actor, user.ErrStatusUpdateUnavailable); err != nil {
		return nil, err
	}
	ids, desired := append([]string(nil), req.IDs...), req.DesiredStatus
	response := &user.BulkUpdateUsersStatusResponse{}
	for _, id := range ids {
		_, err := s.ChangeAccountStatus(ctx, &ChangeAccountStatusRequest{ActorID: actor, TargetUserID: id, DesiredStatus: desired})
		if err != nil {
			response.FailedIDs = append(response.FailedIDs, id)
		} else {
			response.UpdatedCount++
		}
	}
	return response, nil
}

var _ user.StatusManager = (*Service)(nil)
