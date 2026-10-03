package group

import (
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAutoActionValidationMappedErrors keeps model validation classified as
// actionable client errors rather than unknown internal failures.
func TestAutoActionValidationMappedErrors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings GroupSettings
		want     error
		code     string
	}{
		{"mutually exclusive", GroupSettings{AutoJoinByEmailDomainEnabled: true, AutoInviteByEmailDomainEnabled: true}, ErrBothAutoJoinAndAutoInviteEnabled, "GRP0-039"},
		{"missing domains", GroupSettings{AutoJoinByEmailDomainEnabled: true}, ErrInvalidEmailDomain, "GRP0-040"},
		{"empty domain", GroupSettings{AutoInviteByEmailDomainEnabled: true, AutoActionEmailDomains: []string{" "}}, ErrInvalidEmailDomain, "GRP0-040"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.settings.ValidateAutoActionConfig()
			require.ErrorIs(t, err, tc.want)
			rec := httptest.NewRecorder()
			require.NoError(t, (&Handler{}).NewHTTPErrorResponse(rec, err))
			require.Equal(t, 400, rec.Code)
			require.Contains(t, rec.Body.String(), tc.code)
		})
	}
}
