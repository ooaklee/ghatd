package apitoken

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/PaesslerAG/jsonpath"
	"github.com/mergestat/timediff"
	"github.com/ooaklee/ghatd/external/toolbox"
)

var (
	// userAPITokenStatusChoices valid status for user's api token
	userAPITokenStatusChoices = []string{UserTokenStatusKeyActive, UserTokenStatusKeyRevoked}
)

// APITokenRequester information about token requesting resource
type APITokenRequester struct {
	// TokenID is the matched persistent credential ID, never its secret or digest.
	TokenID string `json:"-"`
	// UserID is the stored credential owner, checked against the resolved account.
	UserID              string `json:"user_id" validate:"uuid4"`
	NanoId              string
	UserAPIToken        string
	UserAPITokenEncoded []byte
	IsValid             bool
}

// CredentialDetails carries verified API identity without bearer material.
// It is a request-time snapshot; recheck the token and owner for later work.
type CredentialDetails struct {
	// TokenID identifies the individual credential for delegated grants and audit.
	TokenID string
	// UserID is the current owning account's immutable ID.
	UserID string
}

// UserAPIToken holds access token information for user
// plain-text value is NOT saved to DB.
type UserAPIToken struct {
	ID              string `json:"id" bson:"_id"`
	Value           string `json:"value,omitempty" bson:"-"`
	ValueSHA        []byte `json:"-" bson:"value_sha,omitempty"`
	Status          string `json:"status" bson:"status"`
	Description     string `json:"description" bson:"description,omitempty"`
	CreatedAt       string `json:"created_at" bson:"created_at,omitempty"`
	LastUsedAt      string `json:"last_used_at" bson:"last_used_at,omitempty"`
	CreatedByID     string `json:"created_by_id" bson:"created_by_id,omitempty"`
	CreatedByNanoId string `json:"-" bson:"created_by_nid,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty" bson:"updated_at,omitempty"`
	TtlExpiresAt    string `json:"ttl_expires_at,omitempty" bson:"ttl_expires_at,omitempty"`

	// HumanReadableLastUsedAt is the difference between now (UTC) and when the token was last used
	HumanReadableLastUsedAt string `json:"human_readable_last_used_at,omitempty" bson:"-"`

	// HumanReadableUpdatedAt is the difference between now (UTC) and when the token was last updated
	HumanReadableUpdatedAt string `json:"human_readable_updated_at,omitempty" bson:"-"`

	// HumanReadableTtlExpiresAt is the difference between now (UTC) and when the token will expire
	HumanReadableTtlExpiresAt string `json:"human_readable_ttl_expires_at,omitempty" bson:"-"`
}

// GetAttributeByJsonPath returns the value of the attribute at the given JSON path
// It marshals the User struct to JSON, then uses the jsonpath package to extract the value at the given path.
// If there is an error during the marshaling or jsonpath extraction, it returns the error.
func (u *UserAPIToken) GetAttributeByJsonPath(jsonPath string) (any, error) {
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

// GenerateHumanReadable derives display-only timestamps without changing stored
// dates. RFC3339 offsets and fractional seconds are accepted. Invalid or absent
// dates clear the corresponding display field instead of inventing an age.
func (u *UserAPIToken) GenerateHumanReadable() *UserAPIToken {
	render := func(raw string) string {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return ""
		}
		return timediff.TimeDiff(parsed)
	}
	u.HumanReadableLastUsedAt = render(u.LastUsedAt)
	u.HumanReadableUpdatedAt = render(u.UpdatedAt)
	u.HumanReadableTtlExpiresAt = render(u.TtlExpiresAt)
	return u
}

// IsShortLivedToken is checking whether the user token
// has been assigned a expiry time
func (u *UserAPIToken) IsShortLivedToken() bool {
	return u.TtlExpiresAt != ""
}

// Generate creates a cryptographically random token and its SHA-256 digest.
// The secret contains 256 bits of entropy and generation is concurrency-safe.
// Status and ownership are configured separately; old secrets remain verifiable.
func (u *UserAPIToken) Generate() *UserAPIToken {
	secret := make([]byte, 32)
	// crypto/rand.Read never returns an error on supported Go versions; a
	// failure of the operating-system entropy source terminates the process.
	_, _ = rand.Read(secret)
	u.Value = base64.RawURLEncoding.EncodeToString(secret)
	digest := sha256.Sum256([]byte(u.Value))
	u.ValueSHA = digest[:]

	return u
}

// SetUpdatedAtTimeToNow sets the updatedAt time to now (UTC)
func (u *UserAPIToken) SetUpdatedAtTimeToNow() *UserAPIToken {
	u.UpdatedAt = toolbox.TimeNowUTC()
	return u
}

// GenerateNewUUID creates a new UUID for UserAPIToken
func (u *UserAPIToken) GenerateNewUUID() *UserAPIToken {
	u.ID = toolbox.GenerateUuidV4()
	return u
}

// GenerateNewCodename creates a codename for UserAPIToken
func (u *UserAPIToken) GenerateNewCodename() *UserAPIToken {
	u.Description = toolbox.GenerateAnimalCodedName()
	return u
}

// SetLastUsedAtTimeToNow sets the LastUsedAt time to now (UTC)
func (u *UserAPIToken) SetLastUsedAtTimeToNow() *UserAPIToken {
	u.LastUsedAt = toolbox.TimeNowUTC()
	return u
}

// SetCreatedAtTimeToNow sets the createdAt time to now (UTC)
func (u *UserAPIToken) SetCreatedAtTimeToNow() *UserAPIToken {
	u.CreatedAt = toolbox.TimeNowUTC()
	return u
}

// SetStatus sets the status on the API token, if
// invalid option is passed will default to revoke
func (u *UserAPIToken) SetStatus(status string) *UserAPIToken {
	if toolbox.StringInSlice(status, userAPITokenStatusChoices) {
		u.Status = status
		return u
	}

	u.Status = UserTokenStatusKeyRevoked
	return u
}
