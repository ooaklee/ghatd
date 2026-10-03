package accessmanager

import (
	"context"
	"errors"
	"net/http"
	"strings"

	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/ephemeral"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
)

// MapRequestToLogoutUserRequest selects exactly one access credential and an
// optional refresh cookie. Duplicate cookies/headers, malformed bearer headers,
// and differing cookie/bearer values are ambiguous and rejected, never silently
// substituted. It does not authenticate, mutate headers, or read a JSON body.
func MapRequestToLogoutUserRequest(r *http.Request, accessName, refreshName string) (*LogoutUserRequest, error) {
	if r == nil {
		return nil, ErrBadRequest
	}
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	value := &LogoutUserRequest{}
	counts := map[string]int{}
	for _, cookie := range r.Cookies() {
		if cookie.Name != accessName && cookie.Name != refreshName {
			continue
		}
		counts[cookie.Name]++
		if counts[cookie.Name] > 1 {
			return nil, ErrBadRequest
		}
		if cookie.Name == accessName {
			value.AccessToken = cookie.Value
		} else {
			value.RefreshToken = cookie.Value
		}
	}
	if headers := r.Header.Values("Authorization"); len(headers) > 0 {
		if len(headers) != 1 {
			return nil, ErrInvalidAuthToken
		}
		parts := strings.Fields(headers[0])
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return nil, ErrInvalidAuthToken
		}
		if value.AccessToken != "" && value.AccessToken != parts[1] {
			return nil, ErrInvalidAuthToken
		}
		value.AccessToken = parts[1]
	}
	return value, nil
}

// LogoutUser verifies deletion-only credentials, derives the actor from their
// signatures and removes only those records. Stale account state/expired tokens
// do not prevent cleanup. Missing records are idempotent success; operational
// failures remain native and may represent partial or uncertain writes. This is
// not atomic session-family revocation and never creates/refreshes a session.
func (s *Service) LogoutUser(ctx context.Context, r *LogoutUserRequest) error {
	if s == nil || ctx == nil {
		return ErrSessionVerificationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil {
		return ErrBadRequest
	}
	input := *r
	if input.AccessToken == "" && input.RefreshToken == "" {
		return nil
	}
	verifier, ok := s.AuthService.(auth.SessionRemovalVerifier)
	if !ok || nilAccessDependency(verifier) || nilAccessDependency(s.EphemeralStore) {
		return ErrSessionVerificationUnavailable
	}
	var records []*auth.SessionRemovalDetails
	actor := ""
	for _, credential := range []struct{ raw, use string }{{input.RefreshToken, auth.TokenUseRefresh}, {input.AccessToken, auth.TokenUseAccess}} {
		if credential.raw == "" {
			continue
		}
		value, err := verifier.ExtractSessionRemovalMetadata(ctx, credential.raw, credential.use)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if value == nil || value.TokenUse != credential.use || !validLogoutKey(value.UserID, value.TokenID) {
			return ErrSessionVerificationUnavailable
		}
		if actor != "" && actor != value.UserID {
			return ErrForbiddenUnableToAction
		}
		actor = value.UserID
		copy := *value
		records = append(records, &copy)
	}
	// Verify every supplied credential before any write. If one removal fails,
	// still attempt the other unless canceled, then report all native causes.
	var failures []error
	for _, value := range records {
		if err := ctx.Err(); err != nil {
			failures = append(failures, err)
			break
		}
		count, err := s.EphemeralStore.DeleteAuth(ctx, toolbox.CombinedUuidFormat(value.UserID, value.TokenID))
		if err != nil {
			failures = append(failures, err)
		} else if count < 0 || count > 1 {
			failures = append(failures, ErrSessionVerificationUnavailable)
		}
	}
	if err := ctx.Err(); err != nil {
		failures = append(failures, err)
	}
	if len(failures) == 1 {
		logger.AcquireOperationFrom(ctx, "external/accessmanager", "logout").Warn("logout-removal-incomplete")
		return failures[0]
	}
	if len(failures) > 1 {
		logger.AcquireOperationFrom(ctx, "external/accessmanager", "logout").Warn("logout-removal-incomplete")
		return errors.Join(failures...)
	}
	s.recordLogout(ctx, actor)
	return nil
}

// LogoutUserOthers authorizes current self-service session management and
// rechecks both retained records before an owner-scoped sweep. Context publishing
// alone is not cryptographic verification; credentials and live owner are checked
// again here. Same-owner access/refresh possession does not prove a signed pair:
// legacy tokens have no pair ID. SCAN does not fence concurrent login/rotation.
func (s *Service) LogoutUserOthers(ctx context.Context, r *LogoutUserOthersRequest) error {
	if s == nil || ctx == nil {
		return ErrSessionVerificationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if r == nil {
		return ErrBadRequest
	}
	input := *r
	if err := tokenManagementSession(ctx, input.ActorID, input.UserID); err != nil {
		return err
	}
	if nilAccessDependency(s.AuthService) || nilAccessDependency(s.EphemeralStore) {
		return ErrSessionVerificationUnavailable
	}
	owner, err := s.tokenManagementOwner(ctx, input.ActorID)
	if err != nil {
		return err
	}
	access, err := s.AuthService.ExtractAccessTokenMetadataByString(ctx, input.AuthToken)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	session := accesshelpers.AcquireSessionFrom(ctx)
	if access == nil || !access.IsSessionCredential() || !validLogoutKey(access.UserID, access.AccessUUID) || access.UserID != owner.ID || access.AccessUUID != session.AccessUUID || access.EmailRevision != owner.EmailRevision || !auth.MatchesUserType(access.UserType, owner) {
		return ErrUnauthorizedUnableToAttainRequestorID
	}
	accessCopy := *access
	refresh, err := s.AuthService.ExtractRefreshTokenMetadataByString(ctx, input.RefreshToken)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if refresh == nil || !validLogoutKey(refresh.UserID, refresh.RefreshUUID) || refresh.UserID != owner.ID || (refresh.TokenUse != "" && refresh.TokenUse != auth.TokenUseRefresh) || refresh.EmailRevision != owner.EmailRevision || !auth.MatchesUserType(refresh.UserType, owner) {
		return ErrUnauthorizedUnableToAttainRequestorID
	}
	refreshID := refresh.RefreshUUID
	for _, details := range []*auth.TokenAccessDetails{&accessCopy, {UserID: owner.ID, AccessUUID: refreshID}} {
		liveOwner, err := s.EphemeralStore.FetchAuth(ctx, details)
		if err != nil {
			if ephemeral.IsAuthNotFound(err) {
				return ErrUnauthorizedUnableToAttainRequestorID
			}
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if liveOwner != owner.ID {
			return ErrUnauthorizedUnableToAttainRequestorID
		}
	}
	err = s.EphemeralStore.DeleteAllTokenExceptedSpecified(ctx, owner.ID, []string{toolbox.CombinedUuidFormat(owner.ID, accessCopy.AccessUUID), toolbox.CombinedUuidFormat(owner.ID, refreshID)})
	if err != nil {
		return err
	}
	return ctx.Err()
}

// validLogoutKey prevents token/account delimiters from changing store namespaces.
func validLogoutKey(owner, token string) bool {
	return strings.TrimSpace(owner) != "" && strings.TrimSpace(token) != "" && !strings.Contains(owner+token, ":")
}

// recordLogout is best-effort attribution after confirmed per-key cleanup. Audit
// failure cannot undo removal and is logged without credentials or diagnostics.
func (s *Service) recordLogout(ctx context.Context, actor string) {
	if nilAccessDependency(s.AuditService) {
		return
	}
	if err := s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{ActorId: actor, Action: audit.UserLogout, TargetId: actor, TargetType: audit.User, Domain: "accessmanager"}); err != nil {
		logger.AcquireOperationFrom(ctx, "external/accessmanager", "logout").Warn("logout-audit-failed")
	}
}
