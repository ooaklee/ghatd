package partnermanagerhelper

import (
	"context"
	"net/http"

	"github.com/ooaklee/ghatd/external/accessmanager"
	"github.com/ooaklee/ghatd/external/partnermanager"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
)

// SignupConfig supplies the explicit consent-evidence cookie and immutable
// creation capture policy. The same cookie name must be supplied to the host's
// consented referral issuer. Installing its reader issues no cookie or grant.
type SignupConfig struct {
	Enabled            bool
	EvidenceCookieName string
	Capture            userv2.SignupAttributionConfig
}

// ConfigureSignupCapture binds the same owning user service used by password
// and browser OAuth creation, before any handler or worker starts. Native OAuth
// without a browser cookie captures empty evidence. Existing accounts are never
// backfilled; the owning consumer still verifies signed attribution evidence.
// Disabled construction checks cancellation only and acquires no capability.
func ConfigureSignupCapture(ctx context.Context, cfg SignupConfig, users *userv2.Service, access *accessmanager.Service) error {
	if ctx == nil {
		return partnermanager.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !cfg.Enabled {
		return nil
	}
	if users == nil || access == nil || access.UserService != users {
		return partnermanager.ErrUnavailable
	}
	if cfg.EvidenceCookieName == "" || len(cfg.EvidenceCookieName) > 128 || (&http.Cookie{Name: cfg.EvidenceCookieName, Value: "probe"}).Valid() != nil {
		return accessmanager.ErrBadRequest
	}
	if _, err := users.WithSignupAttribution(cfg.Capture); err != nil {
		return err
	}
	if _, err := access.WithSignupEvidenceCookie(cfg.EvidenceCookieName); err != nil {
		return err
	}
	return ctx.Err()
}
