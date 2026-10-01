package user

import (
	"errors"
	"strings"
)

// ErrOAuthReplacementEmailRequired means removing a provider would leave an
// Apple relay address as the only sign-in method. Verify an independent email
// before retrying; no account fields have changed.
var ErrOAuthReplacementEmailRequired = errors.New("OAuthReplacementEmailRequired")

// IsApplePrivateRelayEmail recognises the exact Sign in with Apple relay
// domains. It does not reject ordinary iCloud addresses or independent privacy
// aliases: their forwarding does not depend on the app's Apple authorisation.
func IsApplePrivateRelayEmail(email string) bool {
	_, domain, found := strings.Cut(normaliseUserEmail(email), "@")
	return found && (domain == "privaterelay.appleid.com" || domain == "private.icloud.com")
}

// OAuthRelayFallbackRepository advertises atomic enforcement of the remaining
// enabled-provider allowlist in DisconnectOAuthProvider. Custom repositories
// without this optional capability must require an independent email instead.
type OAuthRelayFallbackRepository interface {
	// SupportsOAuthRelayFallback is true only when disconnection atomically
	// checks AllowedRelayFallbackProviders against the remaining identities.
	SupportsOAuthRelayFallback() bool
}

// SupportsOAuthRelayFallback reports that Mongo checks the remaining usable
// provider in the same write that removes the selected identity.
func (r *Repository) SupportsOAuthRelayFallback() bool { return true }

// SupportsOAuthRelayFallback propagates the repository's optional atomic relay
// safeguard. Older custom repositories conservatively require a new email.
func (s *Service) SupportsOAuthRelayFallback() bool {
	repo, ok := s.UserRepository.(OAuthRelayFallbackRepository)
	return ok && repo.SupportsOAuthRelayFallback()
}
