package user

import (
	"slices"
	"time"

	"github.com/ooaklee/ghatd/external/toolbox"
)

// UniversalUserToolbox is a utility struct for user operations
type UniversalUserToolbox struct {
	IDGenerator
	TimeProvider
	StringUtils
}

// NewUniversalUserToolbox create a new universal toolbox with all
// necessary implementations
func NewUniversalUserToolbox() *UniversalUserToolbox {
	return &UniversalUserToolbox{}
}

// GenerateNanoID generates nano ID using toolbox
func (t *UniversalUserToolbox) GenerateNanoID() string {
	return toolbox.GenerateNanoId()
}

// GenerateUUID generates UUID v4 using toolbox
func (t *UniversalUserToolbox) GenerateUUID() string {
	return toolbox.GenerateUuidV4()
}

// Now returns current time
func (t *UniversalUserToolbox) Now() time.Time {
	return time.Now()
}

// NowUTC returns now as a string in a standardised format using the toolbox
func (t *UniversalUserToolbox) NowUTC() string {
	return toolbox.TimeNowUTC()
}

// ToTitleCase formats provided string in title case using toolbox
func (t *UniversalUserToolbox) ToTitleCase(s string) string {
	return toolbox.StringConvertToTitleCase(s)
}

// ToLowerCase formats the provided string to lowercase using the toolbox
func (t *UniversalUserToolbox) ToLowerCase(s string) string {
	return toolbox.StringStandardisedToLower(s)
}

// ToUpperCase formats the provided string to uppercase using the toolbox
func (t *UniversalUserToolbox) ToUpperCase(s string) string {
	return toolbox.StringStandardisedToUpper(s)
}

// InSlice checks if the given string is in the slice
func (t *UniversalUserToolbox) InSlice(item string, slice []string) bool {
	return slices.Contains(slice, item)
}
