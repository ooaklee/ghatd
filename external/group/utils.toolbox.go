package group

import (
	"time"

	"github.com/ooaklee/ghatd/external/toolbox"
)

// DefaultIDGenerator implementation using toolbox.
type DefaultIDGenerator struct{}

// GenerateUUID returns a toolbox-generated UUIDv4.
func (d *DefaultIDGenerator) GenerateUUID() string {
	return toolbox.GenerateUuidV4()
}

// GenerateNanoID returns a toolbox-generated NanoID.
func (d *DefaultIDGenerator) GenerateNanoID() string {
	return toolbox.GenerateNanoId()
}

// NewDefaultIDGenerator creates a new default ID generator
func NewDefaultIDGenerator() IDGenerator {
	return &DefaultIDGenerator{}
}

// DefaultTimeProvider implementation
type DefaultTimeProvider struct{}

// Now returns the current local time from the host clock.
func (d *DefaultTimeProvider) Now() time.Time {
	return time.Now()
}

// NowUTC returns the current time formatted as an RFC3339 UTC string.
func (d *DefaultTimeProvider) NowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// NewDefaultTimeProvider creates a new default time provider
func NewDefaultTimeProvider() TimeProvider {
	return &DefaultTimeProvider{}
}

// DefaultStringUtils implementation using toolbox
type DefaultStringUtils struct{}

// ToTitleCase converts a string to title case via the toolbox.
func (d *DefaultStringUtils) ToTitleCase(s string) string {
	return toolbox.StringConvertToTitleCase(s)
}

// ToLowerCase returns a standardised lowercase form of the string.
func (d *DefaultStringUtils) ToLowerCase(s string) string {
	return toolbox.StringStandardisedToLower(s)
}

// ToUpperCase returns a standardised uppercase form of the string.
func (d *DefaultStringUtils) ToUpperCase(s string) string {
	return toolbox.StringStandardisedToUpper(s)
}

// InSlice reports whether item equals any element of slice.
func (d *DefaultStringUtils) InSlice(item string, slice []string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// NewDefaultStringUtils creates a new default string utils
func NewDefaultStringUtils() StringUtils {
	return &DefaultStringUtils{}
}
