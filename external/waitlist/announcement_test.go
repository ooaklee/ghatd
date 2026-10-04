package waitlist

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/emailprovider"
	grouter "github.com/ooaklee/ghatd/external/router"
	"github.com/stretchr/testify/require"
)

type announcementMemoryStore struct {
	mu         sync.Mutex
	entries    []Entry
	previews   map[string]Announcement
	active     *Announcement
	states     map[string]string
	tokens     map[string]string
	suppressed map[string]bool
	finishErr  error
}

func newAnnouncementMemoryStore(n int) *announcementMemoryStore {
	s := &announcementMemoryStore{previews: map[string]Announcement{}, states: map[string]string{}, tokens: map[string]string{}, suppressed: map[string]bool{}}
	for i := 0; i < n; i++ {
		s.entries = append(s.entries, Entry{ID: fmt.Sprint(i), Email: fmt.Sprintf("person-%d@example.com", i), JoinedAt: time.Now().Add(-time.Hour)})
	}
	return s
}
func (s *announcementMemoryStore) Join(context.Context, string) error { return nil }
func (s *announcementMemoryStore) Export(context.Context) ([]Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []Entry{}
	for _, e := range s.entries {
		if !s.suppressed[e.ID] {
			out = append(out, e)
		}
	}
	return out, nil
}
func (s *announcementMemoryStore) SavePreview(_ context.Context, a Announcement) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.previews[a.ID] = a
	return nil
}
func (s *announcementMemoryStore) Preview(_ context.Context, id string) (Announcement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.previews[id]
	if !ok {
		return a, ErrPreviewExpired
	}
	return a, nil
}
func (s *announcementMemoryStore) Started(context.Context) (*Announcement, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active, nil
}
func (s *announcementMemoryStore) Start(_ context.Context, a Announcement) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil && s.active.ID != a.ID {
		return ErrAnnouncementFrozen
	}
	s.active = &a
	return nil
}
func (s *announcementMemoryStore) Claim(_ context.Context, e Entry, _ string, token string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.states[e.ID] != "" || s.suppressed[e.ID] {
		return false, nil
	}
	s.states[e.ID] = "sending"
	s.tokens[token] = e.ID
	return true, nil
}
func (s *announcementMemoryStore) Finish(_ context.Context, id, state, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finishErr != nil {
		return s.finishErr
	}
	s.states[id] = state
	return nil
}
func (s *announcementMemoryStore) DeliveryStates(context.Context) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	for id, state := range s.states {
		out[id] = state
	}
	return out, nil
}
func (s *announcementMemoryStore) Unsubscribe(_ context.Context, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.tokens[hash]; ok {
		s.suppressed[id] = true
	}
	return nil
}

type announcementTestProvider struct {
	mu          sync.Mutex
	sent        []*emailprovider.Email
	err         error
	falseResult bool
}

func (p *announcementTestProvider) Send(_ context.Context, email *emailprovider.Email) (*emailprovider.SendResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sent = append(p.sent, email)
	return &emailprovider.SendResult{Success: !p.falseResult, MessageID: "test-message"}, p.err
}
func (p *announcementTestProvider) Name() string                   { return "TEST" }
func (p *announcementTestProvider) IsHealthy(context.Context) bool { return true }
func (p *announcementTestProvider) count() int                     { p.mu.Lock(); defer p.mu.Unlock(); return len(p.sent) }

func announcementFixture(n int) (*AnnouncementService, *announcementMemoryStore, *announcementTestProvider) {
	s := newAnnouncementMemoryStore(n)
	p := &announcementTestProvider{}
	return &AnnouncementService{Store: s, Provider: p, From: "hello@example.com", FrontendURL: "https://example.com", Enabled: true}, s, p
}
func announcementDraft() Announcement {
	return Announcement{Subject: "Your first look", Message: "Ready to try <script>alert(1)</script>\nA new paragraph.", URL: "https://example.com/prerelease"}
}

func TestAnnouncementPreviewDoesNotSendAndEscapesCopy(t *testing.T) {
	for _, tc := range []struct{ name, message, escaped string }{
		{"script", "Try <script>alert(1)</script>", "&lt;script&gt;"},
		{"HTML", "Try <strong>early access</strong>", "&lt;strong&gt;"},
		{"ordinary text", "Ready for early access.", "Ready for early access."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, p := announcementFixture(3)
			draft := announcementDraft()
			draft.Message = tc.message
			preview, err := s.Prepare(context.Background(), draft)
			require.NoError(t, err)
			require.Equal(t, 3, preview.RecipientCount)
			require.Zero(t, p.count())
			require.NotContains(t, preview.HTML, "<script>")
			require.Contains(t, preview.HTML, tc.escaped)
			require.NotContains(t, preview.HTML, "Up to 35%")
		})
	}
}

// Stateful exception: successive bounded batches and later enrollment exercise
// the same frozen campaign; fixture isolation would lose the no-resend proof.
func TestAnnouncementBatchesIncludeNewSignupsAndDoNotResend(t *testing.T) {
	s, store, p := announcementFixture(12)
	ctx := context.Background()
	preview, err := s.Prepare(ctx, announcementDraft())
	require.NoError(t, err)
	// Signups arriving after preview remain eligible for the announcement.
	store.entries = append(store.entries, Entry{ID: "late", Email: "late@example.com", JoinedAt: time.Now().Add(-time.Hour)})
	first, err := s.Dispatch(ctx, preview.ID)
	require.NoError(t, err)
	require.Equal(t, 10, first.Accepted)
	require.Equal(t, 3, first.Pending)
	second, err := s.Dispatch(ctx, preview.ID)
	require.NoError(t, err)
	require.Equal(t, 13, second.Accepted)
	require.Zero(t, second.Pending)
	_, err = s.Dispatch(ctx, preview.ID)
	require.NoError(t, err)
	require.Equal(t, 13, p.count())
	store.entries = append(store.entries, Entry{ID: "new-arrival", Email: "new@example.com", JoinedAt: time.Now()})
	resumed, err := s.Dispatch(ctx, preview.ID)
	require.NoError(t, err)
	require.Equal(t, 14, resumed.Accepted)
	require.Equal(t, 14, p.count())
	_, err = s.Prepare(ctx, announcementDraft())
	require.ErrorIs(t, err, ErrAnnouncementFrozen)
	current, err := s.Current(ctx)
	require.NoError(t, err)
	require.Equal(t, preview.ID, current.ID)
}

// Concurrency exception: one shared campaign is the contention resource under test.
func TestAnnouncementConcurrentRequestsClaimEachAddressOnce(t *testing.T) {
	s, _, p := announcementFixture(23)
	ctx := context.Background()
	preview, err := s.Prepare(ctx, announcementDraft())
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Go(func() { _, err := s.Dispatch(ctx, preview.ID); errs <- err })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.Equal(t, 23, p.count())
}

func TestAnnouncementUnknownOutcomeAndFailedPersistenceNeverResend(t *testing.T) {
	for _, scenario := range []string{"provider-error", "unsuccessful-result", "persistence-error"} {
		t.Run(scenario, func(t *testing.T) {
			s, store, p := announcementFixture(1)
			ctx := context.Background()
			preview, err := s.Prepare(ctx, announcementDraft())
			require.NoError(t, err)
			switch scenario {
			case "provider-error":
				p.err = errors.New("transport timeout")
			case "unsuccessful-result":
				p.falseResult = true
			case "persistence-error":
				store.finishErr = errors.New("database unavailable")
			}
			_, _ = s.Dispatch(ctx, preview.ID)
			store.finishErr = nil
			summary, err := s.Dispatch(ctx, preview.ID)
			require.NoError(t, err)
			require.Equal(t, 1, p.count())
			require.Equal(t, 1, summary.NeedsReview)
			require.Zero(t, summary.Accepted)
		})
	}
}

func TestAnnouncementExpiredUnstartedPreviewAndDisabledSending(t *testing.T) {
	for _, tc := range []struct {
		name    string
		expired bool
		want    error
	}{
		{"expired", true, ErrPreviewExpired}, {"disabled", false, ErrSendingDisabled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, p := announcementFixture(1)
			ctx := context.Background()
			preview, err := s.Prepare(ctx, announcementDraft())
			require.NoError(t, err)
			if tc.expired {
				s.Now = func() time.Time { return preview.PreparedAt.Add(2 * time.Hour) }
			} else {
				s.Enabled = false
			}
			_, err = s.Dispatch(ctx, preview.ID)
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, p.count())
		})
	}
}

// Stateful exception: preview then local capture validates the disabled-remote
// switch against a real local-output provider and its resulting inbox record.
func TestAnnouncementLocalModeRequiresLocalProvider(t *testing.T) {
	s, _, _ := announcementFixture(1)
	s.Enabled = false
	require.Equal(t, "disabled", s.mode())
	local := emailprovider.NewLoggingEmailProvider(nil)
	s.Provider = local
	s.FrontendURL = "http://127.0.0.1:5179"
	require.Equal(t, "local", s.mode())
	preview, err := s.Prepare(context.Background(), announcementDraft())
	require.NoError(t, err)
	summary, err := s.Dispatch(context.Background(), preview.ID)
	require.NoError(t, err)
	require.Equal(t, "local", summary.Mode)
	require.Equal(t, 1, summary.Accepted)
	require.Equal(t, 1, local.Inbox().Count())
}

func TestAnnouncementInvalidContentAndSenderCannotSend(t *testing.T) {
	for _, tc := range []struct{ name, subject, url string }{
		{"unsafe URL", "Launch", "javascript:alert(1)"},
		{"header injection", "subject\r\nbcc: other@example.com", "https://example.com"},
		{"userinfo", "Launch", "https://user:password@example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, p := announcementFixture(1)
			a := announcementDraft()
			a.Subject, a.URL = tc.subject, tc.url
			_, err := s.Prepare(context.Background(), a)
			require.ErrorIs(t, err, ErrInvalidAnnouncement)
			require.Zero(t, p.count())
		})
	}
	for _, tc := range []struct{ name, from, url string }{
		{"invalid frontend", "hello@example.com", "javascript:alert(1)"},
		{"invalid sender", "name\r\nbcc: bad@example.com", "https://example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _ := announcementFixture(1)
			s.From, s.FrontendURL = tc.from, tc.url
			require.Equal(t, "disabled", s.mode())
		})
	}
}

// Stateful exception: the unsubscribe token is extracted from the captured send;
// later suppression must retain that same historical provider acceptance.
func TestAnnouncementUnsubscribeUsesOpaqueTokenAndPreservesDeliveryHistory(t *testing.T) {
	s, store, p := announcementFixture(1)
	ctx := context.Background()
	preview, err := s.Prepare(ctx, announcementDraft())
	require.NoError(t, err)
	_, err = s.Dispatch(ctx, preview.ID)
	require.NoError(t, err)
	marker := "/waitlist/unsubscribe#"
	parts := strings.Split(p.sent[0].HTMLBody, marker)
	require.Len(t, parts, 2)
	token := strings.Split(parts[1], "\"")[0]
	require.Len(t, token, 26)
	require.NoError(t, store.Unsubscribe(ctx, tokenHash(token)))
	require.NoError(t, store.Unsubscribe(ctx, tokenHash(token)))
	entries, err := store.Export(ctx)
	require.NoError(t, err)
	require.Empty(t, entries)
	summary, err := s.Summary(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, summary.Suppressed)
	require.Equal(t, 1, summary.Accepted) // Consent changes do not undo earlier delivery.
}

func TestAnnouncementRoutesRequireAdminAndUnsubscribeRequiresPost(t *testing.T) {
	s, _, p := announcementFixture(1)
	router := grouter.NewRouter(nil, nil)
	pass := func(next http.Handler) http.Handler { return next }
	deny := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	}
	require.NoError(t, AttachAnnouncementRoutes(router, s, pass, deny))
	for _, endpoint := range []string{"/announcement", "/announcement/preview", "/announcement/send"} {
		t.Run(endpoint, func(t *testing.T) {
			method := http.MethodPost
			if endpoint == "/announcement" {
				method = http.MethodGet
			}
			response := httptest.NewRecorder()
			router.GetRouter().ServeHTTP(response, httptest.NewRequest(method, "/api/v1/waitlist"+endpoint, strings.NewReader(`{}`)))
			require.Equal(t, http.StatusUnauthorized, response.Code)
		})
	}
	response := httptest.NewRecorder()
	router.GetRouter().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/waitlist/unsubscribe?token="+randomToken(), nil))
	require.Equal(t, http.StatusMethodNotAllowed, response.Code)
	require.Zero(t, p.count())
}
