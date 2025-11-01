package group

import (
	"time"

	"github.com/ooaklee/ghatd/external/toolbox"
)

// DefaultIDGenerator implementation using toolbox
type DefaultIDGenerator struct{}

func (d *DefaultIDGenerator) GenerateUUID() string {
	return toolbox.GenerateUuidV4()
}

func (d *DefaultIDGenerator) GenerateNanoID() string {
	return toolbox.GenerateNanoId()
}

// NewDefaultIDGenerator creates a new default ID generator
func NewDefaultIDGenerator() IDGenerator {
	return &DefaultIDGenerator{}
}

// DefaultTimeProvider implementation
type DefaultTimeProvider struct{}

func (d *DefaultTimeProvider) Now() time.Time {
	return time.Now()
}

func (d *DefaultTimeProvider) NowUTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// NewDefaultTimeProvider creates a new default time provider
func NewDefaultTimeProvider() TimeProvider {
	return &DefaultTimeProvider{}
}

// DefaultStringUtils implementation using toolbox
type DefaultStringUtils struct{}

func (d *DefaultStringUtils) ToTitleCase(s string) string {
	return toolbox.StringConvertToTitleCase(s)
}

func (d *DefaultStringUtils) ToLowerCase(s string) string {
	return toolbox.StringStandardisedToLower(s)
}

func (d *DefaultStringUtils) ToUpperCase(s string) string {
	return toolbox.StringStandardisedToUpper(s)
}

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
