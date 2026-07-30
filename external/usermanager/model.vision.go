package usermanager

import (
	"strings"

	userv2 "github.com/ooaklee/ghatd/external/user/v2"
)

// VisionUser is the public user summary associated with a vision item. Raw
// user UUIDs and email addresses are intentionally excluded.
type VisionUser struct {
	NanoID   string `json:"nano_id"`
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
		result.FullName = strings.TrimSpace(user.PersonalInfo.FullName)
		if result.FullName == "" {
			result.FullName = strings.TrimSpace(strings.Join([]string{
				user.PersonalInfo.FirstName,
				user.PersonalInfo.LastName,
			}, " "))
		}
		result.Avatar = user.PersonalInfo.Avatar
	}
	return result
}
