package user

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/ooaklee/ghatd/external/toolbox"

	"github.com/PaesslerAG/jsonpath"
)

// statusChoices valid status for user account
var statusChoices = []string{AccountStatusKeyProvisioned, AccountStatusKeyActive, AccountStatusKeyDeactivated, AccountStatusKeyLockedOut, AccountStatusKeyRecovery, AccountStatusKeySuspended}

// StatusValidOrigins outlines the statuses a status can migrate from
var StatusValidOrigins = map[string][]string{
	AccountStatusKeyActive: {
		AccountStatusKeyProvisioned,
	},
	AccountStatusValidOriginKeyReactivate: {
		AccountStatusKeyDeactivated,
	},
	AccountStatusKeyDeactivated: {
		AccountStatusKeyProvisioned, AccountStatusKeyActive, AccountStatusKeyLockedOut, AccountStatusKeyRecovery, AccountStatusKeySuspended,
	},
	AccountStatusKeySuspended: {
		AccountStatusKeyActive,
	},
	AccountStatusValidOriginKeyUnsuspend: {
		AccountStatusKeySuspended,
	},
	AccountStatusValidOriginKeyUnlock: {
		AccountStatusKeyLockedOut,
	},
	AccountStatusKeyRecovery: {
		AccountStatusKeyActive,
	},
	AccountStatusKeyLockedOut: {
		AccountStatusKeyActive,
	},
	AccountStatusValidOriginKeyEmailChange: {
		AccountStatusKeyActive,
	},
	AccountStatusValidOriginKeyVerifyEmail: {
		AccountStatusKeyProvisioned,
	},
}

// User represents platform users
type User struct {
	ID        string   `json:"id" bson:"_id"`
	NanoId    string   `json:"-" bson:"_nano_id"`
	FirstName string   `json:"first_name" bson:"first_name,omitempty"`
	LastName  string   `json:"last_name" bson:"last_name,omitempty"`
	Email     string   `json:"email" bson:"email,omitempty"`
	Roles     []string `json:"roles" bson:"roles"`
	Status    string   `json:"status" bson:"status,omitempty"`

	Verified UserVerifcationStatus `json:"verified" bson:"verified,omitempty"`
	Meta     UserMeta              `json:"meta" bson:"meta,omitempty"`
}

// GetAsProfile returns user profile representation of the user
func (u *User) GetAsProfile() *UserProfile {
	return &UserProfile{
		ID:            u.ID,
		FirstName:     u.FirstName,
		LastName:      u.LastName,
		Status:        u.Status,
		Roles:         u.Roles,
		Email:         u.Email,
		EmailVerified: u.Verified.EmailVerified,
	}
}

// GetAsMicroProfile returns user micro profile representation of the user
func (u *User) GetAsMicroProfile() *UserMicroProfile {
	return &UserMicroProfile{
		ID:     u.ID,
		Roles:  u.Roles,
		Status: u.Status,
	}
}

// UserMeta holds metadeta about user
type UserMeta struct {
	CreatedAt        string `json:"created_at" bson:"created_at,omitempty"`
	UpdatedAt        string `json:"updated_at,omitempty" bson:"updated_at,omitempty"`
	LastLoginAt      string `json:"last_login_at,omitempty" bson:"last_login_at,omitempty"`
	ActivatedAt      string `json:"activated_at,omitempty" bson:"activated_at,omitempty"`
	StatusChangedAt  string `json:"status_changed_at,omitempty" bson:"status_changed_at,omitempty"`
	LastFreshLoginAt string `json:"last_fresh_login_at,omitempty" bson:"last_fresh_login_at,omitempty"`
}

// GetAttributeByJsonPath returns the value of the attribute at the given JSON path
// It marshals the User struct to JSON, then uses the jsonpath package to extract the value at the given path.
// If there is an error during the marshaling or jsonpath extraction, it returns the error.
func (u *User) GetAttributeByJsonPath(jsonPath string) (any, error) {
	jsonDataByteAsMap := make(map[string]interface{})

	jsonDataByte, err := json.Marshal(u)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(jsonDataByte, &jsonDataByteAsMap)
	if err != nil {
		return nil, err
	}

	result, err := jsonpath.Get(jsonPath, jsonDataByteAsMap)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// UserVerifcationStatus holds verification status for user's
// email and household
type UserVerifcationStatus struct {
	EmailVerified   bool   `json:"email_verified" bson:"email_verified,omitempty"`
	EmailVerifiedAt string `json:"email_verified_at,omitempty" bson:"emailed_verified_at,omitempty"`
}

// IsAdmin returns whether user is an Admin
func (u *User) IsAdmin() bool {
	return toolbox.StringInSlice(UserRoleAdmin, u.Roles)
}

// GenerateNewUUID creates a new UUID for User
func (u *User) GenerateNewUUID() *User {
	u.ID = toolbox.GenerateUuidV4()
	return u
}

// GenerateNewNanoId is generatig a nano UUID for the user
func (u *User) GenerateNewNanoId() *User {
	u.NanoId = toolbox.GenerateNanoId()
	return u
}

// VerifyEmailNow sets the EmailVerifiedAt time to now (UTC)
// and EmailVerified to true
func (u *User) VerifyEmailNow() *User {
	u.Verified.EmailVerifiedAt = toolbox.TimeNowUTC()
	u.Verified.EmailVerified = true
	return u
}

// UnverifyEmailNow sets the EmailVerifiedAt time to empty
// and EmailVerified to false
func (u *User) UnverifyEmailNow() *User {
	u.Verified.EmailVerifiedAt = ""
	u.Verified.EmailVerified = false
	return u
}

// SetCreatedAtTimeToNow sets the createdAt time to now (UTC)
func (u *User) SetCreatedAtTimeToNow() *User {
	u.Meta.CreatedAt = toolbox.TimeNowUTC()
	return u
}

// SetUpdatedAtTimeToNow sets the updatedAt time to now (UTC)
func (u *User) SetUpdatedAtTimeToNow() *User {
	u.Meta.UpdatedAt = toolbox.TimeNowUTC()
	return u
}

// SetLastLoginAtTimeToNow sets the last login At time to now (UTC)
func (u *User) SetLastLoginAtTimeToNow() *User {
	u.Meta.LastLoginAt = toolbox.TimeNowUTC()
	return u
}

// SetActivatedAtTimeToNow sets the activated At time to now (UTC)
func (u *User) SetActivatedAtTimeToNow() *User {
	u.Meta.ActivatedAt = toolbox.TimeNowUTC()
	return u
}

// SetStatusChangedAtTimeToNow sets the status changed At time to now (UTC)
func (u *User) SetStatusChangedAtTimeToNow() *User {
	u.Meta.StatusChangedAt = toolbox.TimeNowUTC()
	return u
}

// SetLastFreshLoginAtTimeToNow sets the last fresh login At time to now (UTC)
func (u *User) SetLastFreshLoginAtTimeToNow() *User {
	u.Meta.LastFreshLoginAt = toolbox.TimeNowUTC()
	return u
}

// SetInitialState sets the initial state of a new user
func (u *User) SetInitialState() *User {
	u.Status = AccountStatusKeyProvisioned
	return u
}

// UpdateStatus sets the current status of the user to the desired status
// TODO: Create tests
func (u *User) UpdateStatus(desiredStatus string) (*User, error) {

	// Get history of valid status that can migrate to desired status
	err := u.validateSourceStatus(desiredStatus)
	if err != nil {
		return u, errors.New(ErrKeyInvalidUserOriginStatus)
	}

	// Check if current user status permitts the requested change
	switch desiredStatus {
	case AccountStatusValidOriginKeyReactivate, AccountStatusValidOriginKeyUnsuspend, AccountStatusValidOriginKeyUnlock, AccountStatusValidOriginKeyEmailChange, AccountStatusValidOriginKeyVerifyEmail:
		// Make sure user was previously ACTIVE before REACTIVATE
		if desiredStatus == AccountStatusValidOriginKeyReactivate {
			if u.Meta.ActivatedAt == "" {
				return nil, errors.New(ErrKeyUserNeverActivated)
			}
			return u.setStatus(AccountStatusKeyActive), nil
		}

		if desiredStatus == AccountStatusValidOriginKeyUnsuspend || desiredStatus == AccountStatusValidOriginKeyUnlock {
			return u.setStatus(AccountStatusKeyActive), nil
		}

		if desiredStatus == AccountStatusValidOriginKeyEmailChange {
			u.UnverifyEmailNow()
			return u.setStatus(AccountStatusKeyProvisioned), nil
		}

		if desiredStatus == AccountStatusValidOriginKeyVerifyEmail {
			u.VerifyEmailNow()
			return u.setStatus(AccountStatusKeyActive), nil
		}

	default:
		u.setStatus(desiredStatus)
		return u, nil
	}

	return u, errors.New(ErrKeyInvalidUserOriginStatus)
}

// validateSourceStatus returns valid status, or errors if status empty
func (u *User) validateSourceStatus(desiredStatus string) error {
	viableSourceStatus := StatusValidOrigins[desiredStatus]
	if len(viableSourceStatus) == 0 {
		return errors.New(ErrKeyInvalidUserOriginStatus)
	}

	if !toolbox.StringInSlice(u.Status, viableSourceStatus) {
		return errors.New(ErrKeyInvalidUserOriginStatus)
	}

	return nil
}

// setStatus sets the status of the user to the passed status
func (u *User) setStatus(s string) *User {
	if strings.Contains(strings.Join(statusChoices, " "), s) {
		u.Status = s
		u.SetUpdatedAtTimeToNow()

		if s == AccountStatusKeyActive {
			u.SetActivatedAtTimeToNow()
		}

		u.SetStatusChangedAtTimeToNow()
	}
	return u
}

// UserMicroProfile holds user's micro metadata
type UserMicroProfile struct {
	ID     string   `json:"id"`
	Roles  []string `json:"roles"`
	Status string   `json:"status"`
}

// UserProfile holds user's profile metadata
type UserProfile struct {
	ID            string   `json:"id"`
	FirstName     string   `json:"first_name"`
	LastName      string   `json:"last_name"`
	Status        string   `json:"status"`
	Roles         []string `json:"roles"`
	Email         string   `json:"email"`
	EmailVerified bool     `json:"email_verified" `
	UpdatedAt     string   `json:"updated_at,omitempty"`
}

// GetUserId returns user's Uuidv4
func (u *User) GetUserId() string {
	return u.ID
}

// GetUserStatus returns user's account status
func (u *User) GetUserStatus() string {
	return u.Status
}
