package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/ephemeral"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/reply/v2"
	"go.uber.org/zap"
)

// hardenedRateLimitEphemeralStore defines the methods required from ephemeral storage
// for hardened rate limiting of code verification endpoints.
type hardenedRateLimitEphemeralStore interface {
	TrackHardenedAttempt(ctx context.Context, ip, code string, maxAttempts int, window time.Duration) error
	BlockIP(ctx context.Context, ip string, duration time.Duration) error
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

			logger := logger.AcquirePackageFrom(r.Context(), "external/accessmanager/middleware")

			clientIP := getValidClientIP(r)

			blocked, err := h.ephemeralStore.IsIPBlocked(r.Context(), clientIP)
			if err != nil {
				logger.Error("failed-to-check-ip-block-status",
					zap.String("client-ip", clientIP),
					zap.Error(err),
				)
				h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
				return
			}

			if blocked {
				logger.Warn("blocked-ip-attempted-verification",
					zap.String("client-ip", clientIP),
				)

				h.getBaseResponseHandler().NewHTTPErrorResponse(w, ephemeral.ErrHardenedRateLimitExceeded)
				return
			}

			code := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("c")))

			err = h.ephemeralStore.TrackHardenedAttempt(r.Context(), clientIP, code, h.maxAttempts, h.windowDuration)
			if err != nil {
				logger.Warn("rate-limit-exceeded-blocking-ip",
					zap.String("client-ip", clientIP),
					zap.String("code", code),
					zap.Int("max-attempts", h.maxAttempts),
					zap.Error(err),
				)

				blockErr := h.ephemeralStore.BlockIP(r.Context(), clientIP, h.blockDuration)
				if blockErr != nil {
					logger.Error("failed-to-block-ip-after-rate-limit-exceeded",
						zap.String("client-ip", clientIP),
						zap.Error(blockErr),
					)
				}

				h.getBaseResponseHandler().NewHTTPErrorResponse(w, err)
				return
			}

			logger.Info("verification-attempt",
				zap.String("client-ip", clientIP),
				zap.String("code", code),
			)

			next.ServeHTTP(w, r)
		})
	}
}

// getBaseResponseHandler returns a response handler configured with the rate limit error maps.
func (h *HardenedRateLimitProtection) getBaseResponseHandler() *reply.Replier {
	return reply.NewReplier(h.errorMaps)
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
