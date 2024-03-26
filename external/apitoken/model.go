package apitoken

import (
	"crypto/sha256"
	"math/rand"
	"time"
	"unsafe"

	"github.com/ooaklee/ghatd/external/toolbox"
)

var (
	src = rand.NewSource(time.Now().UnixNano())
	// userAPITokenStatusChoices valid status for user's api token
	userAPITokenStatusChoices = []string{UserTokenStatusKeyActive, UserTokenStatusKeyRevoked}
)

const (
	// letterBytes possible values for key
	letterBytes   = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ123467890_"
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 8                    // # of letter indices fitting in 63 bits
	tokenLength   = 21
)

// APITokenRequester information about token requesting resource
type APITokenRequester struct {
	UserID              string `json:"user_id" validate:"uuid4"`
	NanoId              string
	UserAPIToken        string
	UserAPITokenEncoded []byte
	IsValid             bool
}

// UserAPIToken holds access token information for user
// plain-text value is NOT saved to DB.
type UserAPIToken struct {
	ID              string `json:"id" bson:"_id"`
	Value           string `json:"value,omitempty" bson:"-"`
	ValueSHA        []byte `json:"value_sha" bson:"value_sha,omitempty"`
	Status          string `json:"status" bson:"status"`
	Description     string `json:"description" bson:"description,omitempty"`
	CreatedAt       string `json:"created_at" bson:"created_at,omitempty"`
	LastUsedAt      string `json:"last_used_at" bson:"last_used_at,omitempty"`
	CreatedByID     string `json:"created_by_id" bson:"created_by_id,omitempty"`
	CreatedByNanoId string `json:"-" bson:"created_by_nid,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty" bson:"updated_at,omitempty"`
	TtlExpiresAt    string `json:"ttl_expires_at,omitempty" bson:"ttl_expires_at,omitempty"`
}

// IsShortLivedToken is checking whether the user token
// has been assigned a expiry time
func (u *UserAPIToken) IsShortLivedToken() bool {
	return u.TtlExpiresAt != ""
}

// Generate creates a core token, populated with Value, ValueSHA, and Status.
func (u *UserAPIToken) Generate() *UserAPIToken {

	keyAsByte, keyAsString := randStringBytesMaskImprSrcUnsafe(tokenLength)

	hasher := sha256.New()
	_, _ = hasher.Write(keyAsByte)

	u.Value = keyAsString
	u.ValueSHA = hasher.Sum(nil)

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

// randStringBytesMaskImprSrcUnsafe generates a random combination of chars from letterBytes
// lifted from https://stackoverflow.com/a/31832326 and tweaked
func randStringBytesMaskImprSrcUnsafe(n int) ([]byte, string) {
	b := make([]byte, n)
	// A src.Int63() generates 63 random bits, enough for letterIdxMax characters!
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	//nolint
	return b, *(*string)(unsafe.Pointer(&b))
}
