package partnermanagerhelper

import (
	"context"
	"strings"
	"testing"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/stretchr/testify/require"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
)

func TestSignupCaptureExplicitCookieAndOwningService(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		want        error
	}{
		{"explicit_cookie_and_capture", "", nil}, {"empty_cookie", "empty-cookie", accessmanager.ErrBadRequest}, {"invalid_cookie", "invalid-cookie", accessmanager.ErrBadRequest}, {"oversized_cookie", "oversize", accessmanager.ErrBadRequest},
		{"different_access_user_owner", "different", partnermanager.ErrUnavailable}, {"missing_user", "nil-user", partnermanager.ErrUnavailable}, {"missing_access", "nil-access", partnermanager.ErrUnavailable},
		{"disabled_requires_no_owner", "disabled", nil}, {"disabled_observes_cancellation", "disabled-cancel", context.Canceled}, {"nil_context", "nil-context", partnermanager.ErrUnavailable},
		{"invalid_program", "program", userv2.ErrSignupEvidenceInvalid}, {"duplicate_account_types", "types", userv2.ErrSignupEvidenceInvalid}, {"missing_capture_repository", "repository", userv2.ErrSignupEvidenceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A disconnected real repository proves installation has no storage I/O.
			users := userv2.NewService(userv2.NewRepository(nil), nil, userv2.DefaultUserConfig(), &userv2.DefaultIDGenerator{}, &userv2.DefaultTimeProvider{}, &userv2.DefaultStringUtils{}, "")
			access := &accessmanager.Service{UserService: users}
			cfg := SignupConfig{Enabled: true, EvidenceCookieName: "host-consented-signup", Capture: userv2.SignupAttributionConfig{ProgramID: "program", IndividualAccountTypes: []string{"individual"}}}
			ctx := t.Context()
			switch tc.state {
			case "empty-cookie":
				cfg.EvidenceCookieName = ""
			case "invalid-cookie":
				cfg.EvidenceCookieName = "bad cookie"
			case "oversize":
				cfg.EvidenceCookieName = strings.Repeat("a", 129)
			case "different":
				access.UserService = &userv2.Service{}
			case "nil-user":
				users = nil
			case "nil-access":
				access = nil
			case "disabled", "disabled-cancel":
				cfg.Enabled = false
				users = nil
				access = nil
				if tc.state == "disabled-cancel" {
					var cancel context.CancelFunc
					ctx, cancel = context.WithCancel(ctx)
					cancel()
				}
			case "nil-context":
				ctx = nil
			case "program":
				cfg.Capture.ProgramID = ""
			case "types":
				cfg.Capture.IndividualAccountTypes = []string{"individual", "individual"}
			case "repository":
				users.UserRepository = nil
			}
			err := ConfigureSignupCapture(ctx, cfg, users, access)
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
