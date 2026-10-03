package accessmanager

import (
	"context"
	"maps"
	"slices"
	"strings"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/emailmanager"
	"github.com/ooaklee/ghatd/external/logger"
	user "github.com/ooaklee/ghatd/external/user/v2"
)

// deliverInitialEmail snapshots transport scalars before calling adapters and
// confirms lookup identity before issuing any proof. Status selection remains
// the existing ACTIVE/login and PROVISIONED/verification policy.
func (s *Service) deliverInitialEmail(ctx context.Context, req *CreateInitalLoginOrVerificationTokenEmailRequest) error {
	if ctx == nil || req == nil || strings.TrimSpace(req.Email) == "" {
		return ErrInvalidUserEmail
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || nilAccessDependency(s.UserService) {
		return ErrLoginEmailUnavailable
	}
	input := *req
	email := strings.ToLower(strings.TrimSpace(input.Email))
	result, err := s.UserService.GetUserByEmail(ctx, &user.GetUserByEmailRequest{Email: email})
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if result == nil || result.User == nil || strings.TrimSpace(result.User.ID) == "" || result.User.Email != email || result.User.EmailRevision < 0 {
		return ErrLoginEmailUnavailable
	}
	switch result.User.Status {
	case user.AccountStatusKeyActive:
		_, err = s.CreateInitalLoginToken(ctx, result.User, input.Dashboard, input.RequestUrl)
	case user.AccountStatusKeyProvisioned:
		_, err = s.CreateEmailVerificationToken(ctx, &CreateEmailVerificationTokenRequest{User: result.User, IsDashboardRequest: input.Dashboard, RequestUrl: input.RequestUrl})
	default:
		return ErrUserStatusUncaught
	}
	return err
}

// copyEmailProofAccount isolates ordinary mutable model fields from signers.
// Nested arbitrary extension values and injected startup dependencies remain
// read-only; serialization would change those values' Go types.
func copyEmailProofAccount(source *user.UniversalUser) *user.UniversalUser {
	v := *source
	v.Roles = slices.Clone(source.Roles)
	v.OAuthIdentities = slices.Clone(source.OAuthIdentities)
	v.OAuthIdentityKeys = slices.Clone(source.OAuthIdentityKeys)
	v.Extensions = maps.Clone(source.Extensions)
	if source.PersonalInfo != nil {
		x := *source.PersonalInfo
		v.PersonalInfo = &x
	}
	if source.Verification != nil {
		x := *source.Verification
		v.Verification = &x
	}
	if source.Metadata != nil {
		x := *source.Metadata
		x.CustomTimestamps = maps.Clone(x.CustomTimestamps)
		v.Metadata = &x
	}
	if source.HandleMetadata != nil {
		x := *source.HandleMetadata
		v.HandleMetadata = &x
	}
	return &v
}

// deliverLoginProof keeps selected ownership and delivery values independent of
// signer-owned models and receipts. Each phase checks cancellation before the
// next side effect; a mail adapter's accepted result survives late cancellation.
// Cooldown release is legacy best-effort, not an ownership-fenced lease. Code
// allocation likewise retains the documented non-atomic legacy reservation.
func (s *Service) deliverLoginProof(ctx context.Context, account *user.UniversalUser, dashboard bool, requestURL string) (string, error) {
	if ctx == nil || account == nil || strings.TrimSpace(account.ID) == "" || strings.TrimSpace(account.Email) == "" || account.EmailRevision < 0 {
		return "", ErrBadRequest
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if s == nil || nilAccessDependency(s.AuthService) || nilAccessDependency(s.EphemeralStore) || nilAccessDependency(s.EmailManager) {
		return "", ErrLoginEmailUnavailable
	}
	value := copyEmailProofAccount(account)
	owner, email := value.ID, value.Email
	acquired, err := s.EphemeralStore.AcquireLoginEmailCooldown(ctx, owner, dashboard, requestURL, loginEmailCooldownTTL)
	if err != nil {
		return "", err
	}
	if !acquired {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		return "", nil
	}
	accepted := false
	defer func() {
		if !accepted {
			if _, err := s.EphemeralStore.ReleaseLoginEmailCooldown(ctx, owner, dashboard, requestURL); err != nil {
				logger.AcquireOperationFrom(ctx, "external/accessmanager", "create-initial-login-proof").Warn("login-email-cooldown-release-failed")
			}
		}
	}()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	receipt, err := s.AuthService.CreateInitalToken(ctx, value)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if receipt == nil || receipt.EphemeralToken == "" || receipt.EphemeralUUID == "" || receipt.EtTTL <= 0 {
		return "", ErrLoginEmailUnavailable
	}
	proof := *receipt
	if err := s.EphemeralStore.StoreToken(ctx, proof.EphemeralUUID, owner, proof.EtTTL); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	code, err := accessmanagerhelpers.GenerateUniqueCode(ctx, s.EphemeralStore, proof.EtTTL)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := s.EphemeralStore.StoreCodeMapping(ctx, code, proof.EphemeralToken, proof.EtTTL); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := s.EmailManager.SendLoginEmail(ctx, &emailmanager.SendLoginEmailRequest{Email: email, UserId: owner, Token: proof.EphemeralToken, Code: code, IsDashboardRequest: dashboard, RequestUrl: requestURL}); err != nil {
		return "", err
	}
	accepted = true
	return proof.EphemeralToken, nil
}
