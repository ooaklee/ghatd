package accessmanager

import (
	"context"
	"fmt"
	"html"
	"reflect"
	"strings"

	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/emailmanager"
	"github.com/ooaklee/ghatd/external/logger"
	user "github.com/ooaklee/ghatd/external/user/v2"
)

// emailChangeUsers keeps the security mutation in the user domain. A legacy
// adapter lacking this capability cannot fall back to full-profile replacement.
type emailChangeUsers interface {
	// ChangeUserEmail atomically updates the authorized account snapshot and
	// invalidates old email-bound proofs through its live revision.
	ChangeUserEmail(context.Context, *user.ChangeUserEmailRequest) (*user.UniversalUser, error)
}

// nilAccessDependency rejects typed-nil adapters before invoking their methods.
func nilAccessDependency(value any) bool {
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

// emailActorMatches requires explicit authenticated context and rejects
// contradictory account evidence. In-process callers must publish identity only
// after authenticating it, using the same helpers as middleware. An ActorID alone
// is not proof of authentication; cached roles do not replace the live read.
func emailActorMatches(ctx context.Context, actor string) bool {
	if strings.TrimSpace(actor) == "" || accesshelpers.AcquireAuthenticatedUserIDFrom(ctx) != actor {
		return false
	}
	if ctx.Value(accesshelpers.RequestorUserKey) != nil {
		account := accesshelpers.AcquireUserFrom(ctx)
		if account == nil || account.ID != actor || accesshelpers.AcquireAuthenticatedUserIDFrom(ctx) != actor {
			return false
		}
	}
	return true
}

// UpdateUserEmail authorizes the caller independently of the selected account,
// delegates the conditional mailbox/revision write, then performs best-effort
// delivery and cleanup. A successful receipt means the address changed even if
// later actions failed; their flags must be checked, not retried as a new change.
// Authority is checked before the write, not atomically with a different admin
// account. Mongo, mail, Redis and audit are not one distributed transaction.
func (s *Service) UpdateUserEmail(ctx context.Context, req *UpdateUserEmailRequest) (*UpdateUserEmailResponse, error) {
	if ctx == nil || req == nil {
		return nil, ErrBadRequest
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r := *req
	if !emailActorMatches(ctx, r.ActorID) {
		return nil, ErrUnauthorizedUnableToAttainRequestorID
	}
	if strings.TrimSpace(r.TargetUserID) == "" {
		return nil, ErrInvalidUserID
	}
	if s == nil || nilAccessDependency(s.UserService) || nilAccessDependency(s.EphemeralStore) || nilAccessDependency(s.AuthService) || nilAccessDependency(s.EmailManager) || nilAccessDependency(s.AuditService) {
		return nil, user.ErrEmailChangeUnavailable
	}
	users, ok := s.UserService.(emailChangeUsers)
	if !ok {
		return nil, user.ErrEmailChangeUnavailable
	}
	actor, err := s.UserService.GetUserByID(ctx, &user.GetUserByIDRequest{ID: r.ActorID})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if actor == nil || actor.User == nil || actor.User.ID != r.ActorID {
		return nil, user.ErrEmailChangeUnavailable
	}
	if actor.User.Status != user.AccountStatusKeyActive || (r.ActorID != r.TargetUserID && !actor.User.IsAdmin()) {
		return nil, ErrForbiddenUnableToAction
	}
	target := actor
	if r.TargetUserID != r.ActorID {
		target, err = s.UserService.GetUserByID(ctx, &user.GetUserByIDRequest{ID: r.TargetUserID})
		if err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if target == nil || target.User == nil || target.User.ID != r.TargetUserID {
		return nil, user.ErrEmailChangeUnavailable
	}
	// All authority/expected fields are scalar snapshots, detached from adapters.
	before := *target.User
	command := user.ChangeUserEmailRequest{UserID: r.TargetUserID, Email: r.Email, ExpectedEmail: before.Email, ExpectedRevision: before.EmailRevision, ExpectedStatus: before.Status, ExpectedType: before.Type}
	updated, err := users.ChangeUserEmail(ctx, &command)
	if err != nil {
		return nil, err
	}
	if updated == nil || updated.ID != r.TargetUserID || updated.Email != user.NormalizeEmail(r.Email) || updated.Type != before.Type || updated.EmailRevision <= before.EmailRevision || updated.EmailRevision-before.EmailRevision != 1 || updated.Status == "" || updated.Verification == nil || updated.Verification.EmailVerified || updated.Verification.EmailVerifiedAt != "" {
		return nil, user.ErrEmailChangeUnavailable
	}
	account := *updated
	result := &UpdateUserEmailResponse{Changed: true, SignOutRequired: r.ActorID == r.TargetUserID}
	log := logger.AcquireOperationFrom(ctx, "external/accessmanager", "change-user-email")
	// Revision checks invalidate old signed credentials even if cleanup is down.
	// Empty exemptions never preserve an administrator's cookies on the target.
	if ctx.Err() == nil {
		result.SessionCleanupComplete = s.EphemeralStore.DeleteAllTokenExceptedSpecified(ctx, r.TargetUserID, nil) == nil
	}
	if !result.SessionCleanupComplete {
		log.Warn("email-change-session-cleanup-incomplete")
	}
	if ctx.Err() == nil {
		_, err = s.CreateEmailVerificationToken(ctx, &CreateEmailVerificationTokenRequest{User: &account})
		result.VerificationEmailSent = err == nil
	}
	if !result.VerificationEmailSent {
		log.Warn("email-change-verification-delivery-incomplete")
	}
	if ctx.Err() == nil {
		// The template is HTML; even authenticated input is not trusted markup.
		body := fmt.Sprintf(UpdateUserEmailOldEmailNotificationBodyTmpl, html.EscapeString(before.Email), html.EscapeString(account.Email), html.EscapeString(r.TargetUserID))
		result.PreviousAddressNotified = s.EmailManager.SendCustomEmail(ctx, &emailmanager.SendCustomEmailRequest{EmailSubject: "Account email changed", EmailPreview: "Your account email address has changed", EmailTo: before.Email, EmailBody: body, WithFooter: true, UserId: r.TargetUserID, RecipientType: string(audit.User)}) == nil
	}
	if !result.PreviousAddressNotified {
		log.Warn("email-change-previous-address-notification-incomplete")
	}
	if ctx.Err() == nil {
		result.AuditRecorded = s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{ActorId: r.ActorID, Action: audit.UserAccountChangeEmail, TargetId: r.TargetUserID, TargetType: audit.User, Domain: "accessmanager", Details: map[string]interface{}{"email_revision": account.EmailRevision}}) == nil
	}
	if !result.AuditRecorded {
		log.Warn("email-change-audit-incomplete")
	}
	return result, nil
}
