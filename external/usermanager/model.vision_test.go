package usermanager

import (
	"testing"

	"github.com/stretchr/testify/assert"

	userv2 "github.com/ooaklee/ghatd/external/user/v2"
)

func TestNewVisionUserAbbreviatesPublicDisplayName(t *testing.T) {
	tests := []struct {
		name         string
		personalInfo *userv2.PersonalInfo
		expected     string
	}{
		{
			name:         "full name",
			personalInfo: &userv2.PersonalInfo{FullName: "Leon Silcott"},
			expected:     "Leon S.",
		},
		{
			name: "structured names",
			personalInfo: &userv2.PersonalInfo{
				FirstName: "Mary Jane",
				LastName:  "Watson",
				FullName:  "Mary Jane Watson",
			},
			expected: "Mary Jane W.",
		},
		{
			name:         "first and last name fallback",
			personalInfo: &userv2.PersonalInfo{FirstName: "Leon", LastName: "Silcott"},
			expected:     "Leon S.",
		},
		{
			name:         "single name",
			personalInfo: &userv2.PersonalInfo{FullName: "Madonna"},
			expected:     "Madonna",
		},
		{
			name:         "unicode surname initial",
			personalInfo: &userv2.PersonalInfo{FirstName: "Ana", LastName: "élan"},
			expected:     "Ana É.",
		},
		{
			name:         "surname only",
			personalInfo: &userv2.PersonalInfo{LastName: "Silcott"},
			expected:     "S.",
		},
		{
			name:         "blank name",
			personalInfo: &userv2.PersonalInfo{},
			expected:     "",
		},
		{
			name:         "missing personal info",
			personalInfo: nil,
			expected:     "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			user := &userv2.UniversalUser{
				NanoID:       "nano-id",
				PersonalInfo: test.personalInfo,
			}

			result := newVisionUser(user)

			assert.Equal(t, test.expected, result.FullName)
			assert.Equal(t, "nano-id", result.NanoID)
		})
	}
}
