package accessmanager

import (
	"context"
	"time"

	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/logger"
	user "github.com/ooaklee/ghatd/external/user/v2"
)

// loginStateUsers keeps account transitions in the user domain. Custom adapters
// must implement both narrow commands; there is no broad UpdateUser fallback.
type loginStateUsers interface {
	// RecordFreshLogin applies a fresh-login account transition in the user domain
	// for the supplied account snapshot, returning the updated universal user.
	RecordFreshLogin(context.Context, *user.AccountSnapshot) (*user.UniversalUser, error)
	// ActivateVerifiedEmail applies the verified-email activation transition in the
	// user domain for the supplied account snapshot, returning the updated
	// universal user.
	ActivateVerifiedEmail(context.Context, *user.AccountSnapshot) (*user.UniversalUser, error)
}

// completeProofSession persists the account transition before minting claims from
// its confirmed post-image. The caller must already have consumed the proof.
// Mongo, signing and Redis are separate operations: any failure requires a fresh
// proof, never replay or restoration. This is not atomic session-family revocation.
func (s *Service) completeProofSession(ctx context.Context, account *user.UniversalUser, activate bool) (*auth.TokenDetails, error) {
	if err := s.proofDependencies(ctx); err != nil {
		return nil, err
	}
	if account == nil || account.ID == "" || account.Email == "" || account.EmailRevision < 0 {
		return nil, ErrSessionVerificationUnavailable
	}
	source := user.AccountStatusKeyActive
	if activate {
		source = user.AccountStatusKeyProvisioned
	}
	if account.Status != source {
		return nil, ErrUserStatusUncaught
	}
	users, ok := s.UserService.(loginStateUsers)
	if !ok {
		return nil, user.ErrLoginStateUnavailable
	}
	request := user.AccountSnapshot{UserID: account.ID, Email: account.Email, EmailRevision: account.EmailRevision, Type: account.Type, Status: source}
	expected := request
	var confirmed *user.UniversalUser
	var err error
	if activate {
		confirmed, err = users.ActivateVerifiedEmail(ctx, &request)
	} else {
		confirmed, err = users.RecordFreshLogin(ctx, &request)
	}
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if confirmed == nil || confirmed.ID != expected.UserID || confirmed.Email != expected.Email || confirmed.EmailRevision != expected.EmailRevision || confirmed.Type != expected.Type || confirmed.Status != user.AccountStatusKeyActive || confirmed.Metadata == nil {
		return nil, user.ErrLoginStateUnavailable
	}
	stamp := confirmed.Metadata.LastFreshLoginAt
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil || at.IsZero() || stamp != at.UTC().Format(time.RFC3339Nano) || confirmed.Metadata.LastLoginAt != stamp {
		return nil, user.ErrLoginStateUnavailable
	}
	if activate && (confirmed.Verification == nil || !confirmed.Verification.EmailVerified || confirmed.Verification.EmailVerifiedAt != stamp || confirmed.Metadata.ActivatedAt != stamp || confirmed.Metadata.StatusChangedAt != stamp || confirmed.Metadata.UpdatedAt != stamp) {
		return nil, user.ErrLoginStateUnavailable
	}
	tokens, err := s.createSessionToken(ctx, confirmed, at)
	if err != nil {
		return nil, err
	}
	if tokens.AccessToken == "" || tokens.RefreshToken == "" {
		return nil, ErrSessionVerificationUnavailable
	}
	if err := s.EphemeralStore.CreateAuth(ctx, expected.UserID, tokens); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Both proof endpoints create a login session. Audit once in this shared
	// manager path; delivery is best-effort and never leaks its native diagnostic.
	if !nilAccessDependency(s.AuditService) {
		if err := s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{ActorId: audit.AuditActorIdSystem, Action: audit.UserLogin, TargetId: expected.UserID, TargetType: audit.User, Domain: "accessmanager"}); err != nil {
			logger.AcquireOperationFrom(ctx, "external/accessmanager", "complete-proof-session").Warn("login-audit-delivery-failed")
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return tokens, nil
}
