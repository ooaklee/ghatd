package auth_test

import (
	"regexp"
	"testing"

	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/stretchr/testify/assert"
)

func TestTokenDetails_GenerateEmailVerificationUUID(t *testing.T) {
	t.Run("Success - generate verification uuid", func(t *testing.T) {
		td := auth.TokenDetails{}

		td.GenerateEmailVerificationUUID()

		assert.Regexp(t, regexp.MustCompile(toolbox.UuidV4Regex), td.EmailVerificationUUID)

	})
}

func TestTokenDetails_GenerateEphemeralUUID(t *testing.T) {
	t.Run("Success - generate ephemeral uuid", func(t *testing.T) {
		td := auth.TokenDetails{}

		td.GenerateEphemeralUUID()

		assert.Regexp(t, regexp.MustCompile(toolbox.UuidV4Regex), td.EphemeralUUID)

	})
}

func TestTokenDetails_GenerateRefreshUUID(t *testing.T) {
	t.Run("Success - generate refresh uuid", func(t *testing.T) {
		td := auth.TokenDetails{}

		td.GenerateRefreshUUID()

		assert.Regexp(t, regexp.MustCompile(toolbox.UuidV4Regex), td.RefreshUUID)

	})
}

func TestTokenDetails_GenerateAccessUUID(t *testing.T) {
	t.Run("Success - generate access uuid", func(t *testing.T) {
		td := auth.TokenDetails{}

		td.GenerateAccessUUID()

		assert.Regexp(t, regexp.MustCompile(toolbox.UuidV4Regex), td.AccessUUID)

	})
}
