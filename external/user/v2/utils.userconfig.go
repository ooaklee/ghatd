package user

// DefaultUserConfig returns a sensible default configuration
func DefaultUserConfig() *UserConfig {
	return &UserConfig{
		DefaultStatus: "PROVISIONED",
		StatusTransitions: map[string][]string{
			"ACTIVE":      {"PROVISIONED"},
			"DEACTIVATED": {"PROVISIONED", "ACTIVE", "LOCKED_OUT", "RECOVERY", "SUSPENDED"},
			"SUSPENDED":   {"ACTIVE"},
			"LOCKED_OUT":  {"ACTIVE"},
			"RECOVERY":    {"ACTIVE"},
		},
		RequiredFields:            []string{"email"},
		ValidRoles:                []string{"ADMIN", "USER"},
		EmailVerificationRequired: true,
		MultipleIdentifiers:       true,
	}
}

// WebAppUserConfig returns configuration suitable for web applications
func WebAppUserConfig() *UserConfig {
	return &UserConfig{
		DefaultStatus: "PROVISIONED",
		StatusTransitions: map[string][]string{
			"ACTIVE":      {"PROVISIONED", "DEACTIVATED"},
			"SUSPENDED":   {"ACTIVE"},
			"DEACTIVATED": {"ACTIVE", "SUSPENDED"},
			"UNSUSPEND":   {"SUSPENDED"},
		},
		RequiredFields:            []string{"email", "first_name", "last_name"},
		ValidRoles:                []string{"ADMIN", "USER"},
		EmailVerificationRequired: true,
		MultipleIdentifiers:       false,
	}
}

// APIServiceUserConfig returns configuration suitable for API services
func APIServiceUserConfig() *UserConfig {
	return &UserConfig{
		DefaultStatus: "ACTIVE",
		StatusTransitions: map[string][]string{
			"ACTIVE":    {"PROVISIONED"},
			"SUSPENDED": {"ACTIVE"},
			"DISABLED":  {"ACTIVE", "SUSPENDED"},
		},
		RequiredFields:            []string{"email"},
		ValidRoles:                []string{"SERVICE", "CLIENT", "ADMIN"},
		EmailVerificationRequired: false,
		MultipleIdentifiers:       true,
	}
}

// MicroserviceUserConfig returns minimal configuration for microservices
func MicroserviceUserConfig() *UserConfig {
	return &UserConfig{
		DefaultStatus: "ACTIVE",
		StatusTransitions: map[string][]string{
			"ACTIVE":   {},
			"INACTIVE": {"ACTIVE"},
		},
		RequiredFields:            []string{"email"},
		ValidRoles:                []string{}, // Allow any roles
		EmailVerificationRequired: false,
		MultipleIdentifiers:       true,
	}
}
