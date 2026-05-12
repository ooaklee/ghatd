package starter

import (
	"context"
	"time"

	accessmiddleware "github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/ooaklee/reply/v2"
)

// HardenedRateLimitStore is the ephemeral storage contract needed by the
// accessmanager hardened rate-limit middleware.
type HardenedRateLimitStore interface {
	TrackHardenedAttempt(ctx context.Context, ip, code string, maxAttempts int, window time.Duration) error
	BlockIP(ctx context.Context, ip string, duration time.Duration) error
	IsIPBlocked(ctx context.Context, ip string) (bool, error)
}

// Middleware groups standard GHATD middleware suites. Individual middleware
// functions remain exposed by their owning suite.
type Middleware struct {
	AccessManager *accessmiddleware.Suite
}

// NewMiddlewareRequest holds dependencies for middleware construction.
type NewMiddlewareRequest struct {
	Services *Services

	EphemeralStore HardenedRateLimitStore
	ErrorMaps      []reply.ErrorManifest

	Environment              string
	CookiePrefixAuthToken    string
	CookiePrefixRefreshToken string
	CookieDomain             string

	MaxAttempts    int
	WindowDuration time.Duration
	BlockDuration  time.Duration
}

// NewMiddleware creates the standard middleware container.
func NewMiddleware(r *NewMiddlewareRequest) (*Middleware, error) {
	if r == nil {
		return nil, ErrNilMiddlewareRequest
	}
	if r.Services == nil {
		return nil, ErrNilServices
	}
	if r.Services.AccessManager == nil {
		return nil, ErrNilAccessManagerService
	}
	ephemeralStore, err := resolveMiddlewareEphemeralStore(r)
	if err != nil {
		return nil, err
	}

	suite, err := accessmiddleware.NewSuite(&accessmiddleware.NewSuiteRequest{
		Service:                  r.Services.AccessManager,
		EphemeralStore:           ephemeralStore,
		ErrorMaps:                r.ErrorMaps,
		Environment:              r.Environment,
		CookiePrefixAuthToken:    r.CookiePrefixAuthToken,
		CookiePrefixRefreshToken: r.CookiePrefixRefreshToken,
		CookieDomain:             r.CookieDomain,
		MaxAttempts:              r.MaxAttempts,
		WindowDuration:           r.WindowDuration,
		BlockDuration:            r.BlockDuration,
	})
	if err != nil {
		return nil, err
	}

	return &Middleware{
		AccessManager: suite,
	}, nil
}

// resolveMiddlewareEphemeralStore returns the explicit store from the request
// when set, or derives one from the service-layer ephemeral store.
func resolveMiddlewareEphemeralStore(r *NewMiddlewareRequest) (HardenedRateLimitStore, error) {
	if r.EphemeralStore != nil {
		return r.EphemeralStore, nil
	}
	if r.Services.EphemeralStore == nil {
		return nil, ErrNilEphemeralStore
	}

	ephemeralStore, ok := r.Services.EphemeralStore.(HardenedRateLimitStore)
	if !ok {
		return nil, ErrInvalidEphemeralStore
	}

	return ephemeralStore, nil
}
