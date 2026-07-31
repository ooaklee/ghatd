package usermanager

import (
	"strings"

	userv2 "github.com/ooaklee/ghatd/external/user/v2"
)

// VisionUser is the public user summary associated with a vision item. Raw
// user UUIDs and email addresses are intentionally excluded.
type VisionUser struct {
	NanoID string `json:"nano_id"`
	// FullName contains a privacy-safe display name, such as "Leon S.".
	FullName string `json:"full_name,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
}

// newVisionUser constructs a public user summary from a UniversalUser,
// excluding raw UUIDs and email addresses.
func newVisionUser(user *userv2.UniversalUser) VisionUser {
	if user == nil {
		return VisionUser{}
	}

	result := VisionUser{NanoID: strings.TrimSpace(user.NanoID)}
	if user.PersonalInfo != nil {
		result.FullName = abbreviatedVisionName(
			user.PersonalInfo.FirstName,
			user.PersonalInfo.LastName,
			user.PersonalInfo.FullName,
		)
		result.Avatar = user.PersonalInfo.Avatar
	}
	return result
}

// abbreviatedVisionName returns a privacy-safe display name containing the
// user's first name and surname initial. It prefers structured name fields,
// falls back to the first and last words of fullName, and preserves single names.
func abbreviatedVisionName(firstName, lastName, fullName string) string {
	firstName = strings.TrimSpace(firstName)
	lastName = strings.TrimSpace(lastName)

	fullNameParts := strings.Fields(fullName)
	if firstName == "" && len(fullNameParts) > 0 {
		firstName = fullNameParts[0]
	}
	if lastName == "" && len(fullNameParts) > 1 {
		lastName = fullNameParts[len(fullNameParts)-1]
	}

	if lastName == "" {
		return firstName
	}

	lastNameRunes := []rune(lastName)
	lastNameInitial := strings.ToUpper(string(lastNameRunes[0])) + "."
	if firstName == "" {
		return lastNameInitial
	}
	return firstName + " " + lastNameInitial
}
