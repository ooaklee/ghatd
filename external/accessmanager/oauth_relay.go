package accessmanager

import user "github.com/ooaklee/ghatd/external/user/v2"

// relayFallbackProviders returns linked, enabled alternatives to the selected
// provider only when persistence can guard them atomically during disconnection.
func (s *Service) relayFallbackProviders(account *user.UniversalUser, removing string) []string {
	capability, ok := s.UserService.(user.OAuthRelayFallbackRepository)
	if !ok || !capability.SupportsOAuthRelayFallback() {
		return nil
	}
	var remaining []string
	for _, enabled := range s.OAuthProviders() {
		if enabled == removing {
			continue
		}
		for _, identity := range account.OAuthIdentities {
			if identity.Provider == enabled {
				remaining = append(remaining, enabled)
				break
			}
		}
	}
	return remaining
}

// requireIndependentFallback prevents an Apple relay becoming the sole sign-in
// method, including when a different provider is removed later. It checks the
// candidate email as well as the current one, so another relay is not a bypass.
func (s *Service) requireIndependentFallback(account *user.UniversalUser, removing, email string) error {
	if user.IsApplePrivateRelayEmail(email) && len(s.relayFallbackProviders(account, removing)) == 0 {
		return user.ErrOAuthReplacementEmailRequired
	}
	return nil
}

// replacementEmailRequiredFor advertises which linked providers need an
// independent email before removal, using the same policy as final confirmation.
func (s *Service) replacementEmailRequiredFor(account *user.UniversalUser) []string {
	providers := []string{}
	for _, provider := range connectedProviderNames(account) {
		if s.requireIndependentFallback(account, provider, account.Email) != nil {
			providers = append(providers, provider)
		}
	}
	return providers
}
