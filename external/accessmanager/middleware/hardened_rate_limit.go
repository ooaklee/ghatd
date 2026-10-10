package middleware

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/ephemeral"
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/reply/v2"
	"go.uber.org/zap"
)

// hardenedRateLimitEphemeralStore defines the methods required from ephemeral storage
// for hardened rate limiting of code verification endpoints.
type hardenedRateLimitEphemeralStore interface {
	// TrackHardenedAttempt records a verification attempt for the given IP and code
	// against the maximum attempt count within the window, as required for hardened
	// rate limiting.
	TrackHardenedAttempt(ctx context.Context, ip, code string, maxAttempts int, window time.Duration) error
	// BlockIP blocks the given IP address in ephemeral storage for the specified
	// duration as part of hardened rate limiting.
	BlockIP(ctx context.Context, ip string, duration time.Duration) error
	// IsIPBlocked reports whether the given IP address is currently blocked in
	// ephemeral storage.
	IsIPBlocked(ctx context.Context, ip string) (bool, error)
}

// HardenedRateLimitProtection provides brute-force protection for code verification endpoints
// by tracking attempts per IP address and per code within a configurable time window.
// When the threshold is exceeded, the IP is temporarily blocked.
type HardenedRateLimitProtection struct {
	ephemeralStore hardenedRateLimitEphemeralStore
	errorMaps      []reply.ErrorManifest
	maxAttempts    int
	windowDuration time.Duration
	blockDuration  time.Duration
}

// NewHardenedRateLimitProtectionRequest holds configuration for creating a hardened rate limit middleware.
type NewHardenedRateLimitProtectionRequest struct {

	// EphemeralStore handles storing and retrieving rate limit counters in cache
	EphemeralStore hardenedRateLimitEphemeralStore

	// ErrorMaps holds the error manifests for translating errors to HTTP responses
	ErrorMaps []reply.ErrorManifest

	// MaxAttempts is the maximum number of verification attempts allowed per IP and per code
	// within the configured time window (default: 5)
	MaxAttempts int

	// WindowDuration is the sliding time window for counting attempts (default: 1 hour)
	WindowDuration time.Duration

	// BlockDuration is how long an IP is blocked after exceeding the limit (default: 1 hour)
	BlockDuration time.Duration
}

// NewHardenedRateLimitProtection creates a new hardened rate limit middleware.
func NewHardenedRateLimitProtection(r *NewHardenedRateLimitProtectionRequest) *HardenedRateLimitProtection {
	if r.MaxAttempts <= 0 {
		r.MaxAttempts = 5
	}
	if r.WindowDuration <= 0 {
		r.WindowDuration = time.Hour
	}
	if r.BlockDuration <= 0 {
		r.BlockDuration = time.Hour
	}

	return &HardenedRateLimitProtection{
		ephemeralStore: r.EphemeralStore,
		errorMaps:      r.ErrorMaps,
		maxAttempts:    r.MaxAttempts,
		windowDuration: r.WindowDuration,
		blockDuration:  r.BlockDuration,
	}
}

// Middleware returns a gorilla/mux middleware function that enforces hardened rate limiting
// on endpoints handling 8-character code verification. It tracks attempts by IP address and
// by the submitted code, and temporarily blocks further requests when the threshold is exceeded.
func (h *HardenedRateLimitProtection) Middleware() mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := r.Context().Err(); err != nil {
				h.fail(w, err)
				return
			}

			logger := logger.AcquirePackageFrom(r.Context(), "external/accessmanager/middleware")

			clientIP := getValidClientIP(r)

			blocked, err := h.ephemeralStore.IsIPBlocked(r.Context(), clientIP)
			if contextErr := r.Context().Err(); contextErr != nil {
				h.fail(w, contextErr)
				return
			}
			if err != nil {
				logger.Error("failed-to-check-ip-block-status")
				h.fail(w, err)
				return
			}

			if blocked {
				logger.Warn("blocked-ip-attempted-verification",
					zap.String("client-ip", clientIP),
				)

				h.fail(w, ephemeral.ErrHardenedRateLimitExceeded)
				return
			}

			code := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("c")))

			err = h.ephemeralStore.TrackHardenedAttempt(r.Context(), clientIP, code, h.maxAttempts, h.windowDuration)
			if contextErr := r.Context().Err(); contextErr != nil {
				h.fail(w, contextErr)
				return
			}
			if err != nil {
				// A failed counter read/write does not prove abuse. Deny this
				// request, but never create an IP ban merely because storage failed.
				if !isHardenedLimitExceeded(err) {
					logger.Error("verification-rate-limit-storage-failed")
					h.fail(w, err)
					return
				}
				logger.Warn("rate-limit-exceeded-blocking-ip",
					zap.String("client-ip", clientIP),
					zap.Bool("code-present", code != ""),
					zap.Int("code-length", len(code)),
					zap.Int("max-attempts", h.maxAttempts),
				)

				blockErr := h.ephemeralStore.BlockIP(r.Context(), clientIP, h.blockDuration)
				if blockErr != nil {
					logger.Error("failed-to-block-ip-after-rate-limit-exceeded")
				}

				h.fail(w, err)
				return
			}

			logger.Info("verification-attempt",
				zap.String("client-ip", clientIP),
				zap.Bool("code-present", code != ""),
				zap.Int("code-length", len(code)),
			)

			next.ServeHTTP(w, r)
		})
	}
}

// isHardenedLimitExceeded accepts one ordinarily wrapped threshold sentinel.
// A joined outage or custom Is-only match cannot justify a consequential ban.
// Bounded traversal also rejects cycles and typed-nil adapter errors.
func isHardenedLimitExceeded(err error) bool {
	for depth := 0; err != nil && depth < 64; depth++ {
		value := reflect.ValueOf(err)
		switch value.Kind() {
		case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
			if value.IsNil() {
				return false
			}
		}
		if err == ephemeral.ErrHardenedRateLimitExceeded {
			return true
		}
		if _, joined := err.(interface{ Unwrap() []error }); joined {
			return false
		}
		err = errors.Unwrap(err)
	}
	return false
}

// getBaseResponseHandler returns a response handler configured with the rate limit error maps.
func (h *HardenedRateLimitProtection) getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(h.errorMaps)
}

// fail retains configured response overrides without exposing wrapped driver
// diagnostics through the HTTP response or the replier's fallback logging.
func (h *HardenedRateLimitProtection) fail(w http.ResponseWriter, err error) {
	_ = h.getBaseResponseHandler().NewHTTPErrorResponse(w, errormanifest.CanonicalError(err, h.errorMaps))
}

// getValidClientIP returns the best IP address to reference a requester by.
// An assumption is made that the request will always be proxied through Cloudflare.
func getValidClientIP(r *http.Request) string {
	headers := r.Header

	if cfIP, ok := headers[common.ClouflareForwardingIPAddressHttpHeader]; ok && len(cfIP) > 0 {
		return cfIP[0]
	}

	return r.RemoteAddr
}
