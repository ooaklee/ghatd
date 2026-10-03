package accessmanager

import (
	"context"

	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/toolbox"
	user "github.com/ooaklee/ghatd/external/user/v2"
)

// proofDependencies checks the adapters needed to create a session from a proof.
// Cancellation stops admission even when a custom adapter ignores context.
func (s *Service) proofDependencies(ctx context.Context) error {
	if ctx == nil || s == nil || nilAccessDependency(s.AuthService) || nilAccessDependency(s.EphemeralStore) || nilAccessDependency(s.UserService) {
		return ErrSessionVerificationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, ok := s.UserService.(loginStateUsers); !ok {
		return user.ErrLoginStateUnavailable
	}
	return nil
}

// proofAccount checks a trusted signed identity against the current account.
// Missing legacy type remains unbound, not reconstructed from client input.
func (s *Service) proofAccount(ctx context.Context, id, kind string, revision int64) (*user.UniversalUser, error) {
	if ctx == nil || s == nil || nilAccessDependency(s.UserService) {
		return nil, ErrSessionVerificationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, auth.ErrUnauthorized
	}
	response, err := s.UserService.GetUserByID(ctx, &user.GetUserByIDRequest{ID: id})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if response == nil || response.User == nil || response.User.ID != id {
		return nil, ErrSessionVerificationUnavailable
	}
	if response.User.EmailRevision != revision || !auth.MatchesUserType(kind, response.User) {
		return nil, ErrOAuthReauthenticationRequired
	}
	return response.User, nil
}

// consumeLoginProof claims one validated proof before account mutations or
// minting. DeleteAuth must atomically delete the exact key and return its count.
// Zero means consumed/expired; any uncertain or invalid result denies issuance.
// The proof is never restored after a downstream failure. This does not make
// cache consumption atomic with account persistence or session creation.
func (s *Service) consumeLoginProof(ctx context.Context, proof *TokenAsStringValidatorResponse) error {
	if ctx == nil || s == nil || s.EphemeralStore == nil {
		return ErrSessionVerificationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if proof == nil || proof.UserID == "" || proof.TokenID == "" {
		return ErrSessionVerificationUnavailable
	}
	deleted, err := s.EphemeralStore.DeleteAuth(ctx, toolbox.CombinedUuidFormat(proof.UserID, proof.TokenID))
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	switch deleted {
	case 1:
		return nil
	case 0:
		return ErrUnauthorizedTokenNotFoundInStore
	default:
		return ErrSessionVerificationUnavailable
	}
}
