package middleware

import (
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	bundles "github.com/ooaklee/ghatd/external/errormanifest/bundles"
	"github.com/ooaklee/reply/v2"
)

var (
	// ErrNilRequest is returned when NewSuite is called without a request.
	ErrNilRequest = errors.New("accessmanager middleware suite: request is required")

	// ErrNilService is returned when NewSuite is called without an accessmanager service.
	ErrNilService = errors.New("accessmanager middleware suite: service is required")

	// ErrNilEphemeralStore is returned when NewSuite is called without an ephemeral store.
	ErrNilEphemeralStore = errors.New("accessmanager middleware suite: ephemeral store is required")
)

// Suite composes the accessmanager Middleware and HardenedRateLimitProtection
// into named mux.MiddlewareFunc fields for convenient wiring.
type Suite struct {
	AdminOnly                          mux.MiddlewareFunc
	ActiveOnly                         mux.MiddlewareFunc
	Authenticated                      mux.MiddlewareFunc
	RateLimitOrActive                  mux.MiddlewareFunc
	ActiveValidApiTokenOrJWT           mux.MiddlewareFunc
	ActiveValidApiTokenOrAuthenticated mux.MiddlewareFunc
	AdminApiTokenOrJWT                 mux.MiddlewareFunc
	CustomMeEndpointValidApiTokenOrJWT mux.MiddlewareFunc
	HardenedRateLimit                  mux.MiddlewareFunc
}

// NewSuiteRequest holds the dependencies for creating a Suite.
type NewSuiteRequest struct {
	Service                  accessManagerService
	EphemeralStore           hardenedRateLimitEphemeralStore
	ErrorMaps                []reply.ErrorManifest
	Environment              string
	CookiePrefixAuthToken    string
	CookiePrefixRefreshToken string
	CookieDomain             string
	MaxAttempts              int
	WindowDuration           time.Duration
	BlockDuration            time.Duration
}

// NewSuite creates a Suite, validating required dependencies. When ErrorMaps
// is nil, bundles.AuthMiddleware() is used as the default. Pass a non-nil
// ErrorMaps slice, including an empty slice, to fully own suite error mapping.
func NewSuite(r *NewSuiteRequest) (*Suite, error) {
	if r == nil {
		return nil, ErrNilRequest
	}
	if r.Service == nil {
		return nil, ErrNilService
	}
	if r.EphemeralStore == nil {
		return nil, ErrNilEphemeralStore
	}

	errorMaps := r.ErrorMaps
	if errorMaps == nil {
		errorMaps = bundles.AuthMiddleware()
	}

	mw := NewMiddleware(&NewMiddlewareRequest{
		Service:                  r.Service,
		ErrorMaps:                errorMaps,
		Environment:              r.Environment,
		CookiePrefixAuthToken:    r.CookiePrefixAuthToken,
		CookiePrefixRefreshToken: r.CookiePrefixRefreshToken,
		CookieDomain:             r.CookieDomain,
	})

	hrl := NewHardenedRateLimitProtection(&NewHardenedRateLimitProtectionRequest{
		EphemeralStore: r.EphemeralStore,
		ErrorMaps:      errorMaps,
		MaxAttempts:    r.MaxAttempts,
		WindowDuration: r.WindowDuration,
		BlockDuration:  r.BlockDuration,
	})

	customMaps := BuildCustomMeEndpointErrorMap(errorMaps)

	return &Suite{
		AdminOnly:                          func(next http.Handler) http.Handler { return mw.AdminJWTRequired(next) },
		ActiveOnly:                         func(next http.Handler) http.Handler { return mw.ActiveJWTRequired(next) },
		Authenticated:                      func(next http.Handler) http.Handler { return mw.JWTRequired(next) },
		RateLimitOrActive:                  func(next http.Handler) http.Handler { return mw.RateLimitOrActiveJWTRequired(next) },
		ActiveValidApiTokenOrJWT:           func(next http.Handler) http.Handler { return mw.ActiveValidApiTokenOrJWTRequired(next) },
		ActiveValidApiTokenOrAuthenticated: func(next http.Handler) http.Handler { return mw.ActiveValidApiTokenOrAuthenticated(next) },
		AdminApiTokenOrJWT:                 func(next http.Handler) http.Handler { return mw.AdminApiTokenOrJWTRequired(next) },
		CustomMeEndpointValidApiTokenOrJWT: mw.CustomMeEndpointValidApiTokenOrJWTMiddleware(customMaps),
		HardenedRateLimit:                  hrl.Middleware(),
	}, nil
}
