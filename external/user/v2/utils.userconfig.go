package user

// DefaultUserConfig returns a sensible default configuration
func DefaultUserConfig() *UserConfig {
	return &UserConfig{
		DefaultStatus: "PROVISIONED",
		StatusTransitions: map[string][]string{
			"ACTIVE":       {"PROVISIONED"},
			"DEACTIVATED":  {"PROVISIONED", "ACTIVE", "LOCKED_OUT", "RECOVERY", "SUSPENDED"},
			"SUSPENDED":    {"ACTIVE"},
			"EMAIL_CHANGE": {"PROVISIONED", "ACTIVE"},
			"LOCKED_OUT":   {"ACTIVE"},
			"RECOVERY":     {"ACTIVE"},
		},
		RequiredFields:            []string{"email"},
		DefaultRole:               "USER",
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
			"ACTIVE":       {"PROVISIONED", "DEACTIVATED"},
			"SUSPENDED":    {"ACTIVE"},
			"DEACTIVATED":  {"ACTIVE", "SUSPENDED"},
			"UNSUSPEND":    {"SUSPENDED"},
			"EMAIL_CHANGE": {"PROVISIONED", "ACTIVE"},
		},
		RequiredFields:            []string{"email", "first_name", "last_name"},
		DefaultRole:               "USER",
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
			"ACTIVE":       {"PROVISIONED"},
			"SUSPENDED":    {"ACTIVE"},
			"DEACTIVATED":  {"ACTIVE", "SUSPENDED"},
			"EMAIL_CHANGE": {"PROVISIONED", "ACTIVE"},
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
			"ACTIVE":       {},
			"DEACTIVATED":  {"ACTIVE"},
			"EMAIL_CHANGE": {"DEACTIVATED", "ACTIVE"},
		},
		RequiredFields:            []string{"email"},
		ValidRoles:                []string{}, // Allow any roles
		EmailVerificationRequired: false,
		MultipleIdentifiers:       true,
	}
}
