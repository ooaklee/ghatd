package user

const (
	UserConfigTypeDefault      = "default"
	UserConfigTypeWebApp       = "web_app"
	UserConfigTypeAPIService   = "api_service"
	UserConfigTypeMicroservice = "microservice"
	UserConfigTypeCustom       = "custom"
)

// DefaultUserConfig returns a sensible default configuration
func DefaultUserConfig() *UserConfig {
	return &UserConfig{
		Type:          UserConfigTypeDefault,
		DefaultStatus: "PROVISIONED",
		StatusTransitions: map[string][]string{
			"ACTIVE":       {"PROVISIONED"},
			"DEACTIVATED":  {"PROVISIONED", "ACTIVE", "LOCKED_OUT", "RECOVERY", "SUSPENDED"},
			"SUSPENDED":    {"ACTIVE"},
			"EMAIL_CHANGE": {"PROVISIONED", "ACTIVE"},
			"LOCKED_OUT":   {"ACTIVE"},
			"RECOVERY":     {"ACTIVE"},
		},
		RequiredFields:            []string{"email", "first_name", "last_name"},
		DefaultRole:               "USER",
		ValidRoles:                []string{"ADMIN", "USER"},
		EmailVerificationRequired: true,
		MultipleIdentifiers:       true,
	}
}

// WebAppUserConfig returns configuration suitable for web applications
func WebAppUserConfig() *UserConfig {
	return &UserConfig{
		Type:          UserConfigTypeWebApp,
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
		Type:          UserConfigTypeAPIService,
		DefaultStatus: "ACTIVE",
		StatusTransitions: map[string][]string{
			"ACTIVE":       {"PROVISIONED", "DEACTIVATED"},
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
		Type:          UserConfigTypeMicroservice,
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

// BuiltInUserConfigs returns the standard supported config presets.
func BuiltInUserConfigs() []*UserConfig {
	return []*UserConfig{
		DefaultUserConfig(),
		WebAppUserConfig(),
		APIServiceUserConfig(),
		MicroserviceUserConfig(),
	}
}

func ensureUserConfigType(config *UserConfig) *UserConfig {
	if config == nil {
		return nil
	}

	if config.Type == "" {
		config.Type = UserConfigTypeCustom
	}

	return config
}

func registerUserConfigs(configs ...*UserConfig) []*UserConfig {
	merged := make([]*UserConfig, 0, len(configs))
	merged = append(merged, configs...)

	seen := make(map[string]struct{})
	registered := make([]*UserConfig, 0, len(merged))
	defaultConfig := DefaultUserConfig()

	for _, config := range merged {
		if config == nil {
			continue
		}

		config = ensureUserConfigType(config)
		configType := config.GetType(defaultConfig)
		if _, exists := seen[configType]; exists {
			continue
		}

		registered = append(registered, config)
		seen[configType] = struct{}{}
	}

	if len(registered) == 0 {
		return []*UserConfig{defaultConfig}
	}

	return registered
}
