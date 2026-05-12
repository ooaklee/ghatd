package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/ephemeral"
	"github.com/ooaklee/reply/v2"
)

func TestNewSuite(t *testing.T) {
	t.Parallel()

	validService := &mockAccessManagerService{}
	validEphemeralStore := &mockHardenedRateLimitStore{}

	baseValidReq := &NewSuiteRequest{
		Service:                  validService,
		EphemeralStore:           validEphemeralStore,
		Environment:              "test",
		CookiePrefixAuthToken:    "auth",
		CookiePrefixRefreshToken: "refresh",
		CookieDomain:             "example.com",
	}

	customTuningState := &rateLimitTuningState{}

	tests := []struct {
		name    string
		req     *NewSuiteRequest
		wantErr error
		check   func(t *testing.T, s *Suite)
	}{
		{
			name:    "Failure - nil service returns error",
			req:     &NewSuiteRequest{},
			wantErr: ErrNilService,
		},
		{
			name: "Failure - nil ephemeral store returns error",
			req: &NewSuiteRequest{
				Service: validService,
			},
			wantErr: ErrNilEphemeralStore,
		},
		{
			name:    "Failure - nil request returns error",
			req:     nil,
			wantErr: ErrNilRequest,
		},
		{
			name: "Success - valid minimal config creates suite with default error maps",
			req: func() *NewSuiteRequest {
				cp := *baseValidReq
				return &cp
			}(),
			wantErr: nil,
			check: func(t *testing.T, s *Suite) {
				if s == nil {
					t.Fatal("expected non-nil suite")
				}
				if s.AdminOnly == nil {
					t.Error("expected non-nil AdminOnly")
				}
				if s.ActiveOnly == nil {
					t.Error("expected non-nil ActiveOnly")
				}
				if s.Authenticated == nil {
					t.Error("expected non-nil Authenticated")
				}
				if s.RateLimitOrActive == nil {
					t.Error("expected non-nil RateLimitOrActive")
				}
				if s.ActiveValidApiTokenOrJWT == nil {
					t.Error("expected non-nil ActiveValidApiTokenOrJWT")
				}
				if s.ActiveValidApiTokenOrAuthenticated == nil {
					t.Error("expected non-nil ActiveValidApiTokenOrAuthenticated")
				}
				if s.AdminApiTokenOrJWT == nil {
					t.Error("expected non-nil AdminApiTokenOrJWT")
				}
				if s.CustomMeEndpointValidApiTokenOrJWT == nil {
					t.Error("expected non-nil CustomMeEndpointValidApiTokenOrJWT")
				}
				if s.HardenedRateLimit == nil {
					t.Error("expected non-nil HardenedRateLimit")
				}
			},
		},
		{
			name: "Success - custom error maps are used by suite middleware",
			req: func() *NewSuiteRequest {
				cp := *baseValidReq
				cp.EphemeralStore = rateLimitExceededStore()
				cp.ErrorMaps = []reply.ErrorManifest{
					{
						ephemeral.ErrHardenedRateLimitExceeded: {Title: "Custom", StatusCode: http.StatusTeapot},
					},
				}
				return &cp
			}(),
			wantErr: nil,
			check: func(t *testing.T, s *Suite) {
				assertHardenedRateLimitStatus(t, s, http.StatusTeapot)
			},
		},
		{
			name: "Success - default error maps are used when none supplied",
			req: func() *NewSuiteRequest {
				cp := *baseValidReq
				cp.EphemeralStore = rateLimitExceededStore()
				cp.ErrorMaps = nil
				return &cp
			}(),
			wantErr: nil,
			check: func(t *testing.T, s *Suite) {
				assertHardenedRateLimitStatus(t, s, http.StatusTooManyRequests)
			},
		},
		{
			name: "Success - valid config accepts custom max attempts and durations",
			req: func() *NewSuiteRequest {
				cp := *baseValidReq
				cp.EphemeralStore = rateLimitTuningStore(customTuningState)
				cp.MaxAttempts = 10
				cp.WindowDuration = 30 * time.Minute
				cp.BlockDuration = 15 * time.Minute
				cp.ErrorMaps = []reply.ErrorManifest{
					{
						ephemeral.ErrHardenedRateLimitExceeded: {Title: "Too Many Requests", StatusCode: http.StatusTooManyRequests},
					},
				}
				return &cp
			}(),
			wantErr: nil,
			check: func(t *testing.T, s *Suite) {
				assertHardenedRateLimitStatus(t, s, http.StatusTooManyRequests)
				if customTuningState.maxAttempts != 10 {
					t.Fatalf("expected max attempts 10, got %d", customTuningState.maxAttempts)
				}
				if customTuningState.windowDuration != 30*time.Minute {
					t.Fatalf("expected window duration 30m, got %v", customTuningState.windowDuration)
				}
				if customTuningState.blockDuration != 15*time.Minute {
					t.Fatalf("expected block duration 15m, got %v", customTuningState.blockDuration)
				}
			},
		},
		{
			name: "Success - all suite middleware funcs are present",
			req: func() *NewSuiteRequest {
				cp := *baseValidReq
				return &cp
			}(),
			wantErr: nil,
			check: func(t *testing.T, s *Suite) {
				fields := []struct {
					name string
					fn   mux.MiddlewareFunc
				}{
					{"AdminOnly", s.AdminOnly},
					{"ActiveOnly", s.ActiveOnly},
					{"Authenticated", s.Authenticated},
					{"RateLimitOrActive", s.RateLimitOrActive},
					{"ActiveValidApiTokenOrJWT", s.ActiveValidApiTokenOrJWT},
					{"ActiveValidApiTokenOrAuthenticated", s.ActiveValidApiTokenOrAuthenticated},
					{"AdminApiTokenOrJWT", s.AdminApiTokenOrJWT},
					{"CustomMeEndpointValidApiTokenOrJWT", s.CustomMeEndpointValidApiTokenOrJWT},
					{"HardenedRateLimit", s.HardenedRateLimit},
				}
				for _, f := range fields {
					if f.fn == nil {
						t.Errorf("expected non-nil %s", f.name)
						continue
					}
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, err := NewSuite(tt.req)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.check != nil {
				tt.check(t, s)
			}
		})
	}
}

type rateLimitTuningState struct {
	maxAttempts    int
	windowDuration time.Duration
	blockDuration  time.Duration
}

func rateLimitTuningStore(state *rateLimitTuningState) *mockHardenedRateLimitStore {
	return &mockHardenedRateLimitStore{
		isIPBlockedFunc: func(_ context.Context, _ string) (bool, error) {
			return false, nil
		},
		trackHardenedAttemptFunc: func(_ context.Context, _, _ string, maxAttempts int, window time.Duration) error {
			state.maxAttempts = maxAttempts
			state.windowDuration = window
			return ephemeral.ErrHardenedRateLimitExceeded
		},
		blockIPFunc: func(_ context.Context, _ string, duration time.Duration) error {
			state.blockDuration = duration
			return nil
		},
	}
}

func rateLimitExceededStore() *mockHardenedRateLimitStore {
	return &mockHardenedRateLimitStore{
		isIPBlockedFunc: func(_ context.Context, _ string) (bool, error) {
			return false, nil
		},
		trackHardenedAttemptFunc: func(_ context.Context, _, _ string, _ int, _ time.Duration) error {
			return ephemeral.ErrHardenedRateLimitExceeded
		},
		blockIPFunc: func(_ context.Context, _ string, _ time.Duration) error {
			return nil
		},
	}
}

func assertHardenedRateLimitStatus(t *testing.T, suite *Suite, wantStatus int) {
	t.Helper()

	if suite == nil {
		t.Fatal("expected non-nil suite")
	}
	if suite.HardenedRateLimit == nil {
		t.Fatal("expected non-nil HardenedRateLimit")
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/verify?c=ABC123DE", nil)
	req.Header.Set("Cf-Connecting-Ip", "10.0.0.1")
	rec := httptest.NewRecorder()

	suite.HardenedRateLimit(next).ServeHTTP(rec, req)

	if rec.Code != wantStatus {
		t.Fatalf("expected status %d, got %d", wantStatus, rec.Code)
	}
}
