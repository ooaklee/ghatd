package emailtemplater

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestEmailTemplater(t *testing.T) *EmailTemplater {
	t.Helper()

	templater, err := NewEmailTemplater(&Config{
		FrontEndDomainName:            "https://app.example.com",
		DashboardDomainName:           "https://dashboard.example.com",
		EmailVerificationFullEndpoint: "https://app.example.com/v0/auth/verify",
		DashboardVerificationURIPath:  "https://dashboard.example.com/v0/auth/verify",
		FromEmailAddress:              "hello@example.com",
		NoReplyEmailAddress:           "noreply@example.com",
		BusinessEntityName:            "Example",
		BusinessEntityWebsite:         "https://example.com",
		LoginEmailSubject:             "Login",
		WelcomeEmailSubject:           "Welcome",
		Templates:                     map[EmailTemplateType]string{},
		DynamicTemplates:              map[EmailTemplateType]func(string, string, string, bool, int, string, string) string{},
	})
	require.NoError(t, err)

	return templater
}

func TestGenerateLoginURLPreservesRelativeRequestURL(t *testing.T) {
	t.Parallel()

	templater := newTestEmailTemplater(t)

	loginURL := templater.generateLoginURL("token-value", false, "/app/plan?slug=pro")

	assert.Equal(
		t,
		"https://app.example.com/v0/auth/verify?type=1&__t=token-value&request_url=%2Fapp%2Fplan%3Fslug%3Dpro",
		loginURL,
	)
}

func TestGenerateLoginURLConvertsSameOriginAbsoluteRequestURL(t *testing.T) {
	t.Parallel()

	templater := newTestEmailTemplater(t)

	loginURL := templater.generateLoginURL("token-value", false, "https://app.example.com/app/plan?slug=pro")

	assert.Equal(
		t,
		"https://app.example.com/v0/auth/verify?type=1&__t=token-value&request_url=%2Fapp%2Fplan%3Fslug%3Dpro",
		loginURL,
	)
}

func TestGenerateLoginURLDropsExternalRequestURL(t *testing.T) {
	t.Parallel()

	templater := newTestEmailTemplater(t)

	loginURL := templater.generateLoginURL("token-value", false, "https://example-tunnel.trycloudflare.com/app/plan")

	assert.Equal(t, "https://app.example.com/v0/auth/verify?type=1&__t=token-value", loginURL)
}

func TestGenerateVerificationEmailSubstitutesPreservesRetryRequestURL(t *testing.T) {
	t.Parallel()

	templater := newTestEmailTemplater(t)

	substitutes := templater.generateVerificationEmailSubstitutes("Ada", "Lovelace", "token-value", "ABC123DE", false, "/app/plan?slug=pro")

	assert.Equal(
		t,
		"https://app.example.com/auth/login?request_url=%2Fapp%2Fplan%3Fslug%3Dpro",
		substitutes.LoginURL,
	)
	assert.Equal(
		t,
		"https://app.example.com/v0/auth/verify?type=2&__t=token-value&request_url=%2Fapp%2Fplan%3Fslug%3Dpro",
		substitutes.VerificationURL,
	)
}
