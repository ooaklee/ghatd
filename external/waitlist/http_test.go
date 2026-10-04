package waitlist

import (
	"context"
	"encoding/csv"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	grouter "github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/require"
)

type fakeStore struct {
	emails  []string
	entries []Entry
	err     error
}

func (s *fakeStore) Join(_ context.Context, email string) error {
	s.emails = append(s.emails, email)
	return s.err
}
func (s *fakeStore) Export(context.Context) ([]Entry, error) { return s.entries, s.err }

func TestSignupValidationAndConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType string
		want                    int
	}{
		{"valid", `{"email":" Person@Example.com ","consent":true,"source":"landing"}`, "application/json", 201},
		{"missing consent", `{"email":"person@example.com","source":"landing"}`, "application/json", 400},
		{"invalid email", `{"email":"bad","consent":true,"source":"landing"}`, "application/json", 400},
		{"display name", `{"email":"Name <person@example.com>","consent":true,"source":"landing"}`, "application/json", 400},
		{"invalid domain", `{"email":"person@.com","consent":true,"source":"landing"}`, "application/json", 400},
		{"foreign source", `{"email":"person@example.com","consent":true,"source":"elsewhere"}`, "application/json", 400},
		{"forged eligibility", `{"email":"person@example.com","consent":true,"source":"landing","unrecognisedField":true}`, "application/json", 400},
		{"trailing JSON", `{"email":"person@example.com","consent":true,"source":"landing"} {}`, "application/json", 400},
		{"oversized", `{"email":"` + strings.Repeat("a", 4096) + `@example.com","consent":true,"source":"landing"}`, "application/json", 400},
		{"form submission", "email=person@example.com", "application/x-www-form-urlencoded", 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{}
			r := httptest.NewRequest(http.MethodPost, "/api/v1/waitlist", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.contentType)
			w := httptest.NewRecorder()
			(&handler{store: store}).join(w, r)
			require.Equal(t, tc.want, w.Code)
			require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
			if tc.want == 201 {
				require.Equal(t, []string{"person@example.com"}, store.emails)
				require.JSONEq(t, `{"data":{"status":"accepted"}}`, w.Body.String())
			} else {
				require.Empty(t, store.emails)
			}
		})
	}
}

func TestSignupFailureDoesNotConfirmOrLeakData(t *testing.T) {
	for _, failure := range []struct {
		name string
		err  error
	}{
		{"database failure", errors.New("database detail person@example.com")},
		{"cancelled persistence", context.Canceled},
		{"timed out persistence", context.DeadlineExceeded},
	} {
		t.Run(failure.name, func(t *testing.T) {
			store := &fakeStore{err: failure.err}
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/v1/waitlist", strings.NewReader(`{"email":"person@example.com","consent":true,"source":"landing"}`))
			r.Header.Set("Content-Type", "application/json")
			(&handler{store: store}).join(w, r)
			require.Equal(t, http.StatusServiceUnavailable, w.Code)
			require.NotContains(t, w.Body.String(), "accepted")
			require.NotContains(t, w.Body.String(), "person@example.com")
		})
	}
}

func TestRoutesGuardExportAndRateLimitSignup(t *testing.T) {
	store := &fakeStore{}
	router := grouter.NewRouter(nil, nil)
	deny := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	}
	rateLimit := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) })
	}
	require.NoError(t, AttachRoutes(router, store, rateLimit, deny))
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{http.MethodPost, "/api/v1/waitlist", 429},
		{http.MethodGet, "/api/v1/waitlist/export", 401},
		{http.MethodOptions, "/api/v1/waitlist", 429},
		{http.MethodGet, "/api/v1/waitlist", 404},
	} {
		w := httptest.NewRecorder()
		router.GetRouter().ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		require.Equal(t, tc.want, w.Code)
	}
	require.Empty(t, store.emails)
}

// This single CSV projection invariant checks column alignment and escaping
// together; separate cases would duplicate the same end-to-end representation.
func TestAdminExportIncludesConsentAndDelivery(t *testing.T) {
	store := &fakeStore{entries: []Entry{
		{Email: "early@example.com", JoinedAt: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), ConsentVersion: ConsentVersion, Source: "landing", AnnouncementState: "accepted", ProviderMessageID: "=untrusted-provider-value"},
		{Email: "+later@example.com", ConsentVersion: ConsentVersion, Source: "landing"},
	}}
	w := httptest.NewRecorder()
	(&handler{store: store}).export(w, httptest.NewRequest(http.MethodGet, "/api/v1/waitlist/export", nil))
	require.Equal(t, 200, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	rows, err := csv.NewReader(w.Body).ReadAll()
	require.NoError(t, err)
	require.Equal(t, []string{"early@example.com", "2026-10-03T12:00:00Z", ConsentVersion, "landing", "accepted", "'=untrusted-provider-value"}, rows[1])
	require.Equal(t, "'+later@example.com", rows[2][0], "prevent spreadsheet formula execution")
	require.Equal(t, ConsentVersion, rows[2][2])
}
