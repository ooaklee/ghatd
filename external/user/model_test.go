package user_test

import (
	"regexp"
	"testing"

	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ooaklee/ghatd/external/user"
	"github.com/stretchr/testify/assert"
)

func TestUser_GenerateNewUUID(t *testing.T) {
	t.Run("Success - Generated UUID", func(t *testing.T) {
		u := user.User{}

		u.GenerateNewUUID()

		assert.NotEmpty(t, u.ID)

		assert.Regexp(t, regexp.MustCompile(toolbox.UuidV4Regex), u.ID)
	})
}

func TestUser_VerifyEmailNow(t *testing.T) {
	t.Run("Success - set Verified email", func(t *testing.T) {
		u := user.User{}

		u.VerifyEmailNow()

		assert.NotEmpty(t, u.Verified.EmailVerifiedAt)
		assert.True(t, u.Verified.EmailVerified)

		assert.Regexp(t, regexp.MustCompile(toolbox.TimeNowUTCAsStringRegex), u.Verified.EmailVerifiedAt)
	})
}

func TestUser_UnverifyEmailNow(t *testing.T) {
	t.Run("Success - set Unverified email", func(t *testing.T) {
		u := user.User{}

		u.UnverifyEmailNow()

		assert.Empty(t, u.Verified.EmailVerifiedAt)
		assert.False(t, u.Verified.EmailVerified)
	})
}

func TestUser_SetCreatedAtTimeToNow(t *testing.T) {
	t.Run("Success - set created at time", func(t *testing.T) {
		u := user.User{}

		u.SetCreatedAtTimeToNow()

		assert.NotEmpty(t, u.Meta.CreatedAt)

		assert.Regexp(t, regexp.MustCompile(toolbox.TimeNowUTCAsStringRegex), u.Meta.CreatedAt)
	})
}

func TestUser_SetUpdatedAtTimeToNow(t *testing.T) {
	t.Run("Success - set update at time", func(t *testing.T) {
		u := user.User{}

		u.SetUpdatedAtTimeToNow()

		assert.NotEmpty(t, u.Meta.UpdatedAt)

		assert.Regexp(t, regexp.MustCompile(toolbox.TimeNowUTCAsStringRegex), u.Meta.UpdatedAt)
	})
}

func TestUser_SetLastLoginAtTimeToNow(t *testing.T) {
	t.Run("Success - set last login at time", func(t *testing.T) {
		u := user.User{}

		u.SetLastLoginAtTimeToNow()

		assert.NotEmpty(t, u.Meta.LastLoginAt)

		assert.Regexp(t, regexp.MustCompile(toolbox.TimeNowUTCAsStringRegex), u.Meta.LastLoginAt)
	})
}

func TestUser_SetActivatedAtTimeToNow(t *testing.T) {
	t.Run("Success - set activated at time", func(t *testing.T) {
		u := user.User{}

		u.SetActivatedAtTimeToNow()

		assert.NotEmpty(t, u.Meta.ActivatedAt)

		assert.Regexp(t, regexp.MustCompile(toolbox.TimeNowUTCAsStringRegex), u.Meta.ActivatedAt)
	})
}

func TestUser_SetStatusChangedAtTimeToNow(t *testing.T) {
	t.Run("Success - set status changed at time", func(t *testing.T) {
		u := user.User{}

		u.SetStatusChangedAtTimeToNow()

		assert.NotEmpty(t, u.Meta.StatusChangedAt)

		assert.Regexp(t, regexp.MustCompile(toolbox.TimeNowUTCAsStringRegex), u.Meta.StatusChangedAt)
	})
}

func TestUser_SetLastFreshLoginAtTimeToNow(t *testing.T) {
	t.Run("Success - set fresh login at time", func(t *testing.T) {
		u := user.User{}

		u.SetLastFreshLoginAtTimeToNow()

		assert.NotEmpty(t, u.Meta.LastFreshLoginAt)

		assert.Regexp(t, regexp.MustCompile(toolbox.TimeNowUTCAsStringRegex), u.Meta.LastFreshLoginAt)
	})
}

func TestUser_SetInitialState(t *testing.T) {
	t.Run("Success - set initial state", func(t *testing.T) {
		u := user.User{}

		u.SetInitialState()

		assert.Equal(t, u.Status, "PROVISIONED")

	})
}

// TODO: Add test for user statuses updates
