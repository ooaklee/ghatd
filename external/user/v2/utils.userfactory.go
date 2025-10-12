package user

import "fmt"

// UserFactory provides convenient user creation
type UserFactory struct {
	config       *UserConfig
	idGenerator  IDGenerator
	timeProvider TimeProvider
	stringUtils  StringUtils
}

// NewUserFactory creates a new user factory with default implementations
func NewUserFactory(config *UserConfig) *UserFactory {
	if config == nil {
		config = DefaultUserConfig()
	}

	return &UserFactory{
		config:       config,
		idGenerator:  &DefaultIDGenerator{},
		timeProvider: &DefaultTimeProvider{},
		stringUtils:  &DefaultStringUtils{},
	}
}

// NewUserFactoryWithDependencies creates a factory with custom implementations
func NewUserFactoryWithDependencies(
	config *UserConfig,
	idGenerator IDGenerator,
	timeProvider TimeProvider,
	stringUtils StringUtils,
) *UserFactory {
	if config == nil {
		config = DefaultUserConfig()
	}

	return &UserFactory{
		config:       config,
		idGenerator:  idGenerator,
		timeProvider: timeProvider,
		stringUtils:  stringUtils,
	}
}

// CreateUser creates a new user with initial setup
func (f *UserFactory) CreateUser(email string) *UniversalUser {
	user := NewUniversalUser(f.config, f.idGenerator, f.timeProvider, f.stringUtils)

	user.Email = f.stringUtils.ToLowerCase(email)
	user.GenerateNewUUID()

	if f.config.MultipleIdentifiers {
		user.GenerateNewNanoID()
	}

	user.SetInitialState()

	return user
}

// CreateUserWithPersonalInfo creates a user with personal information
func (f *UserFactory) CreateUserWithPersonalInfo(email, firstName, lastName string) *UniversalUser {
	user := f.CreateUser(email)

	user.PersonalInfo.FirstName = f.stringUtils.ToTitleCase(firstName)
	user.PersonalInfo.LastName = f.stringUtils.ToTitleCase(lastName)
	user.PersonalInfo.FullName = fmt.Sprintf("%s %s",
		user.PersonalInfo.FirstName,
		user.PersonalInfo.LastName)

	return user
}

// LoadExistingUser loads an existing user and sets up dependencies
func (f *UserFactory) LoadExistingUser(user *UniversalUser) *UniversalUser {
	return user.SetDependencies(f.config, f.idGenerator, f.timeProvider, f.stringUtils)
}
