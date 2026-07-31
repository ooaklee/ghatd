package user

import (
	"crypto/rand"
	"strings"
	"time"

	"github.com/google/uuid"
	gonanoid "github.com/matoous/go-nanoid/v2"
)

// Default implementations of the interfaces for immediate use

// DefaultTimeFormatRFC3339NanoUTC is the time format we use for all
// our date time. It will be used for parsing our UTC date time.
const DefaultTimeFormatRFC3339NanoUTC string = "2006-01-02T15:04:05.999999999"

// DefaultIDGenerator provides default ID generation
type DefaultIDGenerator struct{}

// GenerateUUID generates a new UUID v4
func (g *DefaultIDGenerator) GenerateUUID() string {
	return uuid.New().String()
}

// GenerateNanoID generates a simple nano ID (can be enhanced with proper nano ID library)
func (g *DefaultIDGenerator) GenerateNanoID() string {

	// nolint no need to check error as not returned
	id, err := gonanoid.New()
	if err == nil {
		return id
	}

	// Fallback to basic random string if nanoid generation fails
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	const length = 21

	bytes := make([]byte, length)
	rand.Read(bytes)

	for i, b := range bytes {
		bytes[i] = charset[b%byte(len(charset))]
	}

	return string(bytes)
}

// DefaultTimeProvider provides default time operations
type DefaultTimeProvider struct{}

// Now returns current time
func (t *DefaultTimeProvider) Now() time.Time {
	return time.Now()
}

// NowUTC returns current UTC time as ISO string
func (t *DefaultTimeProvider) NowUTC() string {
	return time.Now().UTC().Format(DefaultTimeFormatRFC3339NanoUTC)
}

// DefaultStringUtils provides default string utilities
type DefaultStringUtils struct{}

// ToTitleCase converts string to title case
func (s *DefaultStringUtils) ToTitleCase(str string) string {
	return strings.Title(strings.ToLower(str))
}

// ToLowerCase converts string to lowercase
func (s *DefaultStringUtils) ToLowerCase(str string) string {
	return strings.ToLower(str)
}

// ToUpperCase converts string to uppercase
func (s *DefaultStringUtils) ToUpperCase(str string) string {
	return strings.ToUpper(str)
}

// InSlice checks if string exists in slice
func (s *DefaultStringUtils) InSlice(item string, slice []string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
