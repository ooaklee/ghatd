package usermanager

import (
	"context"
	"reflect"
	"strings"

	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/router"
	user "github.com/ooaklee/ghatd/external/user/v2"
)

// UserProfileNamesService is the narrow domain capability for self-service name
// changes. Custom adapters must implement it; broad UpdateUser is not a fallback.
type UserProfileNamesService interface {
	// UpdateProfileNames preserves the authorized snapshot and unrelated fields.
	UpdateProfileNames(context.Context, *user.UpdateProfileNamesRequest) (*user.UniversalUser, error)
}

// nilProfilePort recognizes missing and typed-nil wiring without dispatch.
func nilProfilePort(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		return v.IsNil()
	}
	return false
}

// UpdateUserProfile authorizes the caller's own names under the existing
// ActiveSessionOrAPI route contract. Trusted middleware must verify credentials
// first; context publishers are not verifiers. Session type/revision and the live
// ACTIVE account are rechecked, then the domain guards that snapshot in its write.
// API credential admission/grants remain the middleware's responsibility.
// Payload identity, status, email, roles and extensions never select a mutation.
func (s *Service) UpdateUserProfile(ctx context.Context, r *UpdateUserProfileRequest) (*UpdateUserProfileResponse, error) {
	if s == nil || ctx == nil || nilProfilePort(s.UserService) {
		return nil, user.ErrProfileUpdateUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || strings.TrimSpace(r.ActorID) == "" || accesshelpers.AcquireAuthenticatedUserIDFrom(ctx) != r.ActorID {
		return nil, ErrUnableToIdentifyUser
	}
	if r.UpdateUserRequest == nil {
		return nil, ErrInvalidUserBody
	}
	actor, first, last := r.ActorID, r.FirstName, r.LastName
	session, api := accesshelpers.AcquireSessionFrom(ctx), accesshelpers.AcquireAPITokenFrom(ctx)
	if session == nil && api == nil || session != nil && strings.TrimSpace(session.AccessUUID) == "" {
		return nil, router.ErrRouteUnauthenticated
	}
	domain, ok := s.UserService.(UserProfileNamesService)
	if !ok || nilProfilePort(domain) {
		return nil, user.ErrProfileUpdateUnavailable
	}
	response, err := s.UserService.GetUserByID(ctx, &user.GetUserByIDRequest{ID: actor})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if response == nil || response.User == nil || response.User.ID != actor {
		return nil, user.ErrProfileUpdateUnavailable
	}
	owner := *response.User
	if owner.Status != user.AccountStatusKeyActive {
		return nil, router.ErrRouteDenied
	}
	if owner.EmailRevision < 0 || session != nil && (session.EmailRevision != owner.EmailRevision || !auth.MatchesUserType(session.UserType, &owner)) {
		return nil, router.ErrRouteUnauthenticated
	}
	input := user.UpdateProfileNamesRequest{UserID: actor, FirstName: first, LastName: last, ExpectedEmail: owner.Email, ExpectedRevision: owner.EmailRevision, ExpectedType: owner.Type, ExpectedStatus: owner.Status}
	updated, err := domain.UpdateProfileNames(ctx, &input)
	if err != nil {
		return nil, err
	}
	// Both domain readers return the configured representation of legacy Type.
	// Legacy admission permissiveness must not allow an adapter to change Type.
	if updated == nil || updated.ID != actor || updated.Email != owner.Email || updated.EmailRevision != owner.EmailRevision || updated.Status != owner.Status || updated.Type != owner.Type {
		return nil, user.ErrProfileUpdateUnavailable
	}
	if !nilProfilePort(s.AuditService) {
		if err := s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{ActorId: actor, Action: "user.updated", TargetId: actor, TargetType: audit.TargetTypeUser}); err != nil {
			logger.AcquireOperationFrom(ctx, "external/usermanager", "update-user-profile").Warn("profile-audit-failed")
		}
	}
	copy := *updated
	return &UpdateUserProfileResponse{UpdateUserResponse: &user.UpdateUserResponse{User: &copy}}, nil
}
