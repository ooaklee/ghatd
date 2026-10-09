package telenumcoder

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/catalogue"
	"github.com/ooaklee/ghatd/external/cataloguestore"
	"github.com/stretchr/testify/require"
)

func phoneFixture(t *testing.T, options ...Option) *Service {
	t.Helper()
	repo, err := NewRepository(cataloguestore.NewMemoryStore(nil))
	require.NoError(t, err)
	s, err := NewService(repo, nil, options...)
	require.NoError(t, err)
	require.NoError(t, s.Migrate(context.Background()))
	return s
}

// Parser inputs use independent named fixtures. The final mutation sequence
// checks one current country policy and copied immutable definitions across
// calls; it is retained together because those later reads depend on that state.
func TestNationalAndInternationalNumbersRespectActualCountry(t *testing.T) {
	s := phoneFixture(t)
	ctx := context.Background()
	for _, test := range []struct{ phone, region, number, country string }{
		{"07400 123456", "GB", "+447400123456", "GB"},
		{"+1 (202) 555-0123", "GB", "+12025550123", "US"},
		{"+1 876 210 1234", "US", "+18762101234", "JM"},
	} {
		t.Run(test.phone, func(t *testing.T) {
			result, err := phoneFixture(t).Normalise(t.Context(), test.phone, test.region)
			require.NoError(t, err, test.phone)
			require.Equal(t, Number{test.number, test.country}, result)
		})
	}
	us, err := s.Get(ctx, "US")
	require.NoError(t, err)
	us.Enabled = false
	_, err = s.Update(ctx, UpdateRequest{Record: us, ExpectedRevision: us.Revision, ActorID: "admin"})
	require.NoError(t, err)
	_, err = s.Normalise(ctx, "+12025550123", "GB")
	require.ErrorIs(t, err, catalogue.ErrNotSelectable)
	_, err = s.Normalise(ctx, "2025550123", "US")
	require.ErrorIs(t, err, catalogue.ErrNotSelectable)
	for _, value := range []string{"123", "+44abc7400123456", "+447400123456 ext 123", "+447400123456;123", "", "+999123456789"} {
		_, err = s.Normalise(ctx, value, "GB")
		require.ErrorIs(t, err, ErrInvalidNumber, value)
	}
	// Different NANP territories share +1, but have independently selectable IDs.
	jm, ok := Definition("JM")
	require.True(t, ok)
	require.Equal(t, "+1", jm.CallingCode)
	require.Contains(t, jm.DialPrefixes, "+1876")
	jm.DialPrefixes[0] = "changed"
	again, _ := Definition("JM")
	require.NotContains(t, again.DialPrefixes, "changed")
	gb, err := s.Get(ctx, "GB")
	require.NoError(t, err)
	gb.CallingCode = "+1"
	_, err = s.Update(ctx, UpdateRequest{Record: gb, ExpectedRevision: 1, ActorID: "admin"})
	require.ErrorIs(t, err, catalogue.ErrInvalidPayload)
}

type lookupFunc func(context.Context, string) (State, error)

func (f lookupFunc) Check(ctx context.Context, number string) (State, error) { return f(ctx, number) }
func TestReachabilityNeverConfusesValidationWithDelivery(t *testing.T) {
	for _, test := range []struct {
		name     string
		provider Reachability
		want     State
	}{
		{"unconfigured", nil, Unavailable},
		{"reachable", lookupFunc(func(_ context.Context, n string) (State, error) {
			require.Equal(t, "+447400123456", n)
			return Reachable, nil
		}), Reachable},
		{"unreachable", lookupFunc(func(context.Context, string) (State, error) { return Unreachable, nil }), Unreachable},
		{"provider failure", lookupFunc(func(context.Context, string) (State, error) { return Reachable, errors.New("private provider error") }), Unavailable},
		{"unknown response", lookupFunc(func(context.Context, string) (State, error) { return "pending", nil }), Unavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := phoneFixture(t, WithReachability(test.provider))
			result, err := s.Check(context.Background(), "07400123456", "GB")
			require.NoError(t, err)
			require.Equal(t, test.want, result.State)
			require.Equal(t, "sms", result.Channel)
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := phoneFixture(t).Check(ctx, "07400123456", "GB")
	require.ErrorIs(t, err, context.Canceled)
}
func TestHTTPReachabilityContractAndFailureModes(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		want       State
		fails      bool
	}{
		{"reachable", `{"state":"reachable"}`, 200, Reachable, false},
		{"unreachable", `{"state":"unreachable"}`, 200, Unreachable, false},
		{"unavailable", `{"state":"unavailable"}`, 200, Unavailable, false},
		{"unknown", `{"state":"maybe"}`, 200, Unavailable, true},
		{"unexpected field", `{"state":"reachable","token":"secret"}`, 200, Unavailable, true},
		{"trailing", `{"state":"reachable"}{}`, 200, Unavailable, true},
		{"malformed", `{`, 200, Unavailable, true},
		{"upstream failure", `private provider error`, 503, Unavailable, true},
		{"redirect", ``, 302, Unavailable, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				require.Equal(t, "POST", r.Method)
				require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				require.Empty(t, r.URL.RawQuery)
				var input map[string]string
				require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
				require.Equal(t, map[string]string{"number": "+447400123456", "channel": "sms"}, input)
				w.Header().Set("Location", "/should-not-follow")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			p, err := NewHTTPReachability(HTTPConfig{Endpoint: server.URL, Client: server.Client(), Token: "test-token"})
			require.NoError(t, err)
			state, err := p.Check(context.Background(), "+447400123456")
			require.Equal(t, test.want, state)
			if test.fails {
				require.ErrorIs(t, err, ErrLookupUnavailable)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, 1, calls)
		})
	}
}
func TestHTTPReachabilityConfiguration(t *testing.T) {
	for _, tc := range []struct{ name, endpoint string }{
		{"http", "http://lookup.example"}, {"userinfo", "https://user:pass@lookup.example"},
		{"query", "https://lookup.example?phone=secret"}, {"fragment", "https://lookup.example#fragment"}, {"empty", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewHTTPReachability(HTTPConfig{Endpoint: tc.endpoint})
			require.ErrorIs(t, err, ErrLookupUnavailable)
		})
	}
}
func TestHTTPReachabilityTimeoutAndCancellation(t *testing.T) {
	for _, mode := range []string{"timeout", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case <-r.Context().Done():
				case <-time.After(time.Second):
				}
			}))
			t.Cleanup(server.Close)
			p, err := NewHTTPReachability(HTTPConfig{Endpoint: server.URL, Client: server.Client(), Timeout: 100 * time.Millisecond})
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := ErrLookupUnavailable
			if mode == "cancelled" {
				cancel()
				want = context.Canceled
			}
			state, err := p.Check(ctx, "+447400123456")
			require.Equal(t, Unavailable, state)
			require.ErrorIs(t, err, want)
		})
	}
}
