package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/ephemeral"
	"github.com/ooaklee/reply"
)

// mockHardenedRateLimitStore implements hardenedRateLimitEphemeralStore for testing
type mockHardenedRateLimitStore struct {
	trackHardenedAttemptFunc func(ctx context.Context, ip, code string, maxAttempts int, window time.Duration) error
	blockIPFunc              func(ctx context.Context, ip string, duration time.Duration) error
	isIPBlockedFunc          func(ctx context.Context, ip string) (bool, error)
}

func (m *mockHardenedRateLimitStore) TrackHardenedAttempt(ctx context.Context, ip, code string, maxAttempts int, window time.Duration) error {
	if m.trackHardenedAttemptFunc != nil {
		return m.trackHardenedAttemptFunc(ctx, ip, code, maxAttempts, window)
	}
	return nil
}

func (m *mockHardenedRateLimitStore) BlockIP(ctx context.Context, ip string, duration time.Duration) error {
	if m.blockIPFunc != nil {
		return m.blockIPFunc(ctx, ip, duration)
	}
	return nil
}

func (m *mockHardenedRateLimitStore) IsIPBlocked(ctx context.Context, ip string) (bool, error) {
	if m.isIPBlockedFunc != nil {
		return m.isIPBlockedFunc(ctx, ip)
	}
	return false, nil
}

func TestNewHardenedRateLimitProtection_Defaults(t *testing.T) {
	store := &mockHardenedRateLimitStore{}
	hrl := NewHardenedRateLimitProtection(&NewHardenedRateLimitProtectionRequest{
		EphemeralStore: store,
		ErrorMaps:      []reply.ErrorManifest{ephemeral.EphemeralStoreErrorMap},
	})

	if hrl.maxAttempts != 5 {
		t.Errorf("expected default maxAttempts 5, got %d", hrl.maxAttempts)
	}
	if hrl.windowDuration != time.Hour {
		t.Errorf("expected default windowDuration 1h, got %v", hrl.windowDuration)
	}
	if hrl.blockDuration != time.Hour {
		t.Errorf("expected default blockDuration 1h, got %v", hrl.blockDuration)
	}
}

func TestNewHardenedRateLimitProtection_Custom(t *testing.T) {
	store := &mockHardenedRateLimitStore{}
	hrl := NewHardenedRateLimitProtection(&NewHardenedRateLimitProtectionRequest{
		EphemeralStore: store,
		ErrorMaps:      []reply.ErrorManifest{ephemeral.EphemeralStoreErrorMap},
		MaxAttempts:    10,
		WindowDuration: 2 * time.Hour,
		BlockDuration:  30 * time.Minute,
	})

	if hrl.maxAttempts != 10 {
		t.Errorf("expected maxAttempts 10, got %d", hrl.maxAttempts)
	}
	if hrl.windowDuration != 2*time.Hour {
		t.Errorf("expected windowDuration 2h, got %v", hrl.windowDuration)
	}
	if hrl.blockDuration != 30*time.Minute {
		t.Errorf("expected blockDuration 30m, got %v", hrl.blockDuration)
	}
}

func TestHardenedRateLimitProtection_PassThrough(t *testing.T) {
	var handlerCalled bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	store := &mockHardenedRateLimitStore{}
	hrl := NewHardenedRateLimitProtection(&NewHardenedRateLimitProtectionRequest{
		EphemeralStore: store,
		ErrorMaps:      []reply.ErrorManifest{ephemeral.EphemeralStoreErrorMap},
	})

	middleware := hrl.Middleware()(next)

	req := httptest.NewRequest(http.MethodGet, "/verify?c=ABC123DE", nil)
	req.Header.Set("Cf-Connecting-Ip", "10.0.0.1")
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if !handlerCalled {
		t.Error("expected handler to be called")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestHardenedRateLimitProtection_NoCode_PassThrough(t *testing.T) {
	var handlerCalled bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	store := &mockHardenedRateLimitStore{}
	hrl := NewHardenedRateLimitProtection(&NewHardenedRateLimitProtectionRequest{
		EphemeralStore: store,
		ErrorMaps:      []reply.ErrorManifest{ephemeral.EphemeralStoreErrorMap},
	})

	middleware := hrl.Middleware()(next)

	req := httptest.NewRequest(http.MethodGet, "/verify?t=some-token", nil)
	req.Header.Set("Cf-Connecting-Ip", "10.0.0.1")
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if !handlerCalled {
		t.Error("expected handler to be called when code is absent")
	}
}

func TestHardenedRateLimitProtection_IPBlocked(t *testing.T) {
	var handlerCalled bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	store := &mockHardenedRateLimitStore{
		isIPBlockedFunc: func(ctx context.Context, ip string) (bool, error) {
			return true, nil
		},
	}

	hrl := NewHardenedRateLimitProtection(&NewHardenedRateLimitProtectionRequest{
		EphemeralStore: store,
		ErrorMaps:      []reply.ErrorManifest{ephemeral.EphemeralStoreErrorMap},
	})

	middleware := hrl.Middleware()(next)

	req := httptest.NewRequest(http.MethodGet, "/verify?c=ABC123DE", nil)
	req.Header.Set("Cf-Connecting-Ip", "10.0.0.1")
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if handlerCalled {
		t.Error("expected handler NOT to be called when IP is blocked")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected status 429, got %d", rec.Code)
	}
}

func TestHardenedRateLimitProtection_IsIPBlockedError(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	store := &mockHardenedRateLimitStore{
		isIPBlockedFunc: func(ctx context.Context, ip string) (bool, error) {
			return false, errors.New("redis unavailable")
		},
	}

	hrl := NewHardenedRateLimitProtection(&NewHardenedRateLimitProtectionRequest{
		EphemeralStore: store,
		ErrorMaps:      []reply.ErrorManifest{},
	})

	middleware := hrl.Middleware()(next)

	req := httptest.NewRequest(http.MethodGet, "/verify?c=ABC123DE", nil)
	req.Header.Set("Cf-Connecting-Ip", "10.0.0.1")
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500 on storage error, got %d", rec.Code)
	}
}

func TestHardenedRateLimitProtection_RateLimitExceeded(t *testing.T) {
	var handlerCalled bool
	blockIPCalled := false

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	store := &mockHardenedRateLimitStore{
		isIPBlockedFunc: func(ctx context.Context, ip string) (bool, error) {
			return false, nil
		},
		trackHardenedAttemptFunc: func(ctx context.Context, ip, code string, maxAttempts int, window time.Duration) error {
			return errors.New(ephemeral.ErrKeyHardenedRateLimitExceeded)
		},
		blockIPFunc: func(ctx context.Context, ip string, duration time.Duration) error {
			blockIPCalled = true
			return nil
		},
	}

	hrl := NewHardenedRateLimitProtection(&NewHardenedRateLimitProtectionRequest{
		EphemeralStore: store,
		ErrorMaps:      []reply.ErrorManifest{ephemeral.EphemeralStoreErrorMap},
	})

	middleware := hrl.Middleware()(next)

	req := httptest.NewRequest(http.MethodGet, "/verify?c=ABC123DE", nil)
	req.Header.Set("Cf-Connecting-Ip", "10.0.0.1")
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if handlerCalled {
		t.Error("expected handler NOT to be called when rate limit exceeded")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected status 429, got %d", rec.Code)
	}
	if !blockIPCalled {
		t.Error("expected BlockIP to be called when rate limit exceeded")
	}
}

func TestHardenedRateLimitProtection_BlockIPErrorNonFatal(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	store := &mockHardenedRateLimitStore{
		isIPBlockedFunc: func(ctx context.Context, ip string) (bool, error) {
			return false, nil
		},
		trackHardenedAttemptFunc: func(ctx context.Context, ip, code string, maxAttempts int, window time.Duration) error {
			return errors.New(ephemeral.ErrKeyHardenedRateLimitExceeded)
		},
		blockIPFunc: func(ctx context.Context, ip string, duration time.Duration) error {
			return errors.New("block failed")
		},
	}

	hrl := NewHardenedRateLimitProtection(&NewHardenedRateLimitProtectionRequest{
		EphemeralStore: store,
		ErrorMaps:      []reply.ErrorManifest{ephemeral.EphemeralStoreErrorMap},
	})

	middleware := hrl.Middleware()(next)

	req := httptest.NewRequest(http.MethodGet, "/verify?c=ABC123DE", nil)
	req.Header.Set("Cf-Connecting-Ip", "10.0.0.1")
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429 even when BlockIP fails, got %d", rec.Code)
	}
}

func TestHardenedRateLimitProtection_IPFromRemoteAddr(t *testing.T) {
	var capturedIP string
	store := &mockHardenedRateLimitStore{
		trackHardenedAttemptFunc: func(ctx context.Context, ip, code string, maxAttempts int, window time.Duration) error {
			capturedIP = ip
			return nil
		},
	}

	hrl := NewHardenedRateLimitProtection(&NewHardenedRateLimitProtectionRequest{
		EphemeralStore: store,
		ErrorMaps:      []reply.ErrorManifest{ephemeral.EphemeralStoreErrorMap},
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := hrl.Middleware()(next)

	req := httptest.NewRequest(http.MethodGet, "/verify?c=XYZ789AB", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if capturedIP != "192.168.1.1:12345" {
		t.Errorf("expected RemoteAddr IP, got %q", capturedIP)
	}
}

func TestHardenedRateLimitProtection_IPFromCloudflareHeader(t *testing.T) {
	var capturedIP string
	store := &mockHardenedRateLimitStore{
		trackHardenedAttemptFunc: func(ctx context.Context, ip, code string, maxAttempts int, window time.Duration) error {
			capturedIP = ip
			return nil
		},
	}

	hrl := NewHardenedRateLimitProtection(&NewHardenedRateLimitProtectionRequest{
		EphemeralStore: store,
		ErrorMaps:      []reply.ErrorManifest{ephemeral.EphemeralStoreErrorMap},
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := hrl.Middleware()(next)

	req := httptest.NewRequest(http.MethodGet, "/verify?c=XYZ789AB", nil)
	req.Header.Set("Cf-Connecting-Ip", "1.2.3.4")
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if capturedIP != "1.2.3.4" {
		t.Errorf("expected Cloudflare IP, got %q", capturedIP)
	}
}

func TestHardenedRateLimitProtection_CodeUppercaseNormalization(t *testing.T) {
	var capturedCode string
	store := &mockHardenedRateLimitStore{
		trackHardenedAttemptFunc: func(ctx context.Context, ip, code string, maxAttempts int, window time.Duration) error {
			capturedCode = code
			return nil
		},
	}

	hrl := NewHardenedRateLimitProtection(&NewHardenedRateLimitProtectionRequest{
		EphemeralStore: store,
		ErrorMaps:      []reply.ErrorManifest{ephemeral.EphemeralStoreErrorMap},
	})

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := hrl.Middleware()(next)

	req := httptest.NewRequest(http.MethodGet, "/verify?c=abc123de", nil)
	req.Header.Set("Cf-Connecting-Ip", "10.0.0.1")
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if capturedCode != "ABC123DE" {
		t.Errorf("expected uppercase code, got %q", capturedCode)
	}
}
