package emailprovider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Test-only credentials are deliberately synthetic; no test reads a live key.
const birdTestKey = "bk_eu1_TEST_ONLY_NOT_A_REAL_CREDENTIAL"
const birdTestReceipt = `{"id":"em_testreceipt","status":"accepted","accepted_count":1}`

type birdTestTransport func(*http.Request) (*http.Response, error)

func (f birdTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func birdTestResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func birdTestEmail() *Email {
	return &Email{From: "Sender <sender@example.test>", To: "recipient@example.test", ReplyTo: "reply@example.test", Subject: "Sign in", HTMLBody: "<p>Test</p>", TextBody: "Test"}
}

func TestBirdConfiguration(t *testing.T) {
	for _, tc := range []struct{ name, key, base, endpoint string }{
		{"eu inferred", birdTestKey, "", "https://eu1.platform.bird.com/v1/email/messages"},
		{"us inferred", "bk_us1_TEST_ONLY", "", "https://us1.platform.bird.com/v1/email/messages"},
		{"explicit", birdTestKey, "https://eu1.platform.bird.com/", "https://eu1.platform.bird.com/v1/email/messages"},
		{"missing", "", "", ""}, {"unknown region", "bk_other_TEST", "", ""},
		{"empty payload", "bk_eu1_", "", ""}, {"header injection", birdTestKey + "\r\nInjected: 1", "", ""},
		{"wrong region", birdTestKey, "https://us1.platform.bird.com", ""},
		{"http", birdTestKey, "http://eu1.platform.bird.com", ""},
		{"suffix host", birdTestKey, "https://eu1.platform.bird.com.evil.test", ""},
		{"userinfo", birdTestKey, "https://secret@eu1.platform.bird.com", ""},
		{"path", birdTestKey, "https://eu1.platform.bird.com/v1", ""},
		{"query", birdTestKey, "https://eu1.platform.bird.com?secret=1", ""},
		{"fragment", birdTestKey, "https://eu1.platform.bird.com#secret", ""},
		{"oversized key", "bk_eu1_" + strings.Repeat("x", 513), "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewBirdClient(&NewBirdClientRequest{APIKey: tc.key, BaseURL: tc.base})
			if tc.endpoint == "" {
				require.Nil(t, client)
				require.ErrorIs(t, err, ErrEmailProviderUnavailable)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.endpoint, client.endpoint)
		})
	}
}

func TestBirdClientPolicyOwnership(t *testing.T) {
	for _, tc := range []struct {
		name          string
		timeout, want time.Duration
		override      bool
	}{
		{"default", 0, 30 * time.Second, false}, {"short", time.Second, time.Second, false},
		{"capped", time.Hour, 30 * time.Second, false}, {"negative", -time.Second, 30 * time.Second, false},
		{"override", time.Second, time.Second, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := &http.Transport{}
			other := &http.Transport{}
			jar := &cookieJarStub{}
			source := &http.Client{Transport: base, Timeout: tc.timeout, Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return nil }}
			request := &NewBirdClientRequest{APIKey: birdTestKey}
			copy := request.WithHTTPClient(source)
			require.Nil(t, request.HTTPClient)
			if tc.override {
				copy.Transport = other
			}
			client, err := NewBirdClient(copy)
			require.NoError(t, err)
			require.NotSame(t, source, client.httpClient)
			require.Equal(t, tc.want, client.httpClient.Timeout)
			require.Nil(t, client.httpClient.Jar)
			require.ErrorIs(t, client.httpClient.CheckRedirect(nil, nil), http.ErrUseLastResponse)
			if tc.override {
				require.Same(t, other, client.httpClient.Transport)
			} else {
				require.Same(t, base, client.httpClient.Transport)
			}
			require.Same(t, base, source.Transport)
			require.Same(t, jar, source.Jar)
			require.Equal(t, tc.timeout, source.Timeout)
			require.NoError(t, source.CheckRedirect(nil, nil))
		})
	}
}

func TestBirdSendMapping(t *testing.T) {
	for _, tc := range []struct{ name, html, text, reply string }{
		{"multipart", "<p>Test</p>", "Test", "Support <support@example.test>"},
		{"text only", "", "Plain text", ""}, {"html only", "<p>Test</p>", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			email := birdTestEmail()
			email.HTMLBody, email.TextBody, email.ReplyTo = tc.html, tc.text, tc.reply
			original := *email
			calls := 0
			client, err := NewBirdClient(&NewBirdClientRequest{APIKey: birdTestKey, Transport: birdTestTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, http.MethodPost, r.Method)
				require.Equal(t, "https://eu1.platform.bird.com/v1/email/messages", r.URL.String())
				require.Equal(t, "Bearer "+birdTestKey, r.Header.Get("Authorization"))
				require.Equal(t, "application/json", r.Header.Get("Content-Type"))
				require.Empty(t, r.Header.Get("Cookie"))
				require.Empty(t, r.Header.Get("Idempotency-Key"))
				require.Nil(t, r.GetBody)
				var got map[string]any
				require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
				require.Equal(t, email.From, got["from"])
				require.Equal(t, []any{email.To}, got["to"])
				require.Equal(t, email.Subject, got["subject"])
				require.Equal(t, "transactional", got["category"])
				require.Equal(t, false, got["track_clicks"])
				require.Equal(t, false, got["track_opens"])
				for key, value := range map[string]string{"html": tc.html, "text": tc.text} {
					if value == "" {
						require.NotContains(t, got, key)
					} else {
						require.Equal(t, value, got[key])
					}
				}
				if tc.reply == "" {
					require.NotContains(t, got, "reply_to")
				} else {
					require.Equal(t, []any{tc.reply}, got["reply_to"])
				}
				return birdTestResponse(202, birdTestReceipt), nil
			})})
			require.NoError(t, err)
			result, err := NewBirdEmailProvider(client).Send(context.Background(), email)
			require.NoError(t, err)
			require.Equal(t, &SendResult{Provider: "BIRD", MessageID: "em_testreceipt", Success: true}, result)
			require.Equal(t, 1, calls)
			require.Equal(t, original, *email)
		})
	}
}

func TestBirdInvalidEmailsNeverDispatch(t *testing.T) {
	for _, tc := range []struct {
		name         string
		email        *Email
		field, value string
		want         error
	}{
		{"nil", nil, "", "", ErrEmailProviderInvalidEmail},
		{"recipient missing", birdTestEmail(), "To", "", ErrEmailProviderMissingRecipient},
		{"sender missing", birdTestEmail(), "From", "", ErrEmailProviderMissingFrom},
		{"subject missing", birdTestEmail(), "Subject", "", ErrEmailProviderMissingSubject},
		{"body missing", &Email{From: "a@example.test", To: "b@example.test", Subject: "Test"}, "", "", ErrEmailProviderMissingBody},
		{"bad recipient", birdTestEmail(), "To", "a@example.test,b@example.test", ErrEmailProviderInvalidEmail},
		{"bad reply", birdTestEmail(), "ReplyTo", "invalid", ErrEmailProviderInvalidEmail},
		{"header injection", birdTestEmail(), "Subject", "Test\r\nBcc: evil@example.test", ErrEmailProviderInvalidEmail},
		{"blank subject", birdTestEmail(), "Subject", "  ", ErrEmailProviderInvalidEmail},
		{"bad utf8", birdTestEmail(), "TextBody", string([]byte{0xff}), ErrEmailProviderInvalidEmail},
		{"oversized", birdTestEmail(), "TextBody", strings.Repeat("x", birdRequestLimit), ErrEmailProviderInvalidEmail},
		{"encoded oversized", birdTestEmail(), "TextBody", strings.Repeat("<", birdRequestLimit/2), ErrEmailProviderInvalidEmail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.email != nil {
				switch tc.field {
				case "To":
					tc.email.To = tc.value
				case "From":
					tc.email.From = tc.value
				case "Subject":
					tc.email.Subject = tc.value
				case "ReplyTo":
					tc.email.ReplyTo = tc.value
				case "TextBody":
					tc.email.TextBody = tc.value
				}
			}
			client, err := NewBirdClient(&NewBirdClientRequest{APIKey: birdTestKey, Transport: birdTestTransport(func(*http.Request) (*http.Response, error) {
				t.Error("unexpected dispatch")
				return nil, errors.New("private")
			})})
			require.NoError(t, err)
			result, err := NewBirdEmailProvider(client).Send(context.Background(), tc.email)
			require.ErrorIs(t, err, tc.want)
			require.Same(t, err, result.Error)
			require.False(t, result.Success)
		})
	}
}

type birdReadCloser struct {
	io.Reader
	closed bool
}

func (r *birdReadCloser) Close() error { r.closed = true; return nil }

type birdErrorReader struct{}

func (birdErrorReader) Read([]byte) (int, error) { return 0, errors.New("private-stream-secret") }

func TestBirdResponses(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		body      string
		readError bool
	}{
		{"unauthorized", 401, "private", false}, {"forbidden", 403, "private", false}, {"wrong region", 421, "private", false},
		{"validation", 422, "private", false}, {"throttled", 429, "private", false}, {"server", 500, "private", false},
		{"redirect", 307, "private", false}, {"unexpected success", 200, birdTestReceipt, false},
		{"empty", 202, "", false}, {"malformed", 202, "private not JSON", false},
		{"missing receipt", 202, `{}`, false}, {"wrong status", 202, `{"id":"em_test","status":"delivered","accepted_count":1}`, false},
		{"wrong count", 202, `{"id":"em_test","status":"accepted","accepted_count":2}`, false},
		{"unsafe id", 202, `{"id":"em_private@example.test","status":"accepted","accepted_count":1}`, false},
		{"oversized", 202, strings.Repeat(" ", birdResponseLimit) + birdTestReceipt, false},
		{"trailing JSON", 202, birdTestReceipt + `{}`, false}, {"stream error", 202, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &birdReadCloser{Reader: strings.NewReader(tc.body)}
			if tc.readError {
				body.Reader = birdErrorReader{}
			}
			calls := 0
			client, err := NewBirdClient(&NewBirdClientRequest{APIKey: birdTestKey, Transport: birdTestTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: tc.status, Body: body, Header: http.Header{"Location": []string{"https://evil.example.test"}}}, nil
			})})
			require.NoError(t, err)
			result, err := NewBirdEmailProvider(client).Send(context.Background(), birdTestEmail())
			require.ErrorIs(t, err, ErrEmailProviderSendFailed)
			require.NotContains(t, err.Error(), "private")
			require.False(t, result.Success)
			require.Empty(t, result.MessageID)
			require.True(t, body.closed)
			require.Equal(t, 1, calls)
		})
	}
}

func TestBirdCancellation(t *testing.T) {
	for _, tc := range []struct {
		name             string
		before, deadline bool
	}{
		{"before dispatch", true, false}, {"in flight", false, false}, {"deadline", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.deadline {
				var stop context.CancelFunc
				ctx, stop = context.WithTimeout(ctx, 10*time.Millisecond)
				defer stop()
			}
			if tc.before {
				cancel()
			}
			calls := 0
			client, err := NewBirdClient(&NewBirdClientRequest{APIKey: birdTestKey, Transport: birdTestTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if !tc.deadline {
					cancel()
				}
				<-r.Context().Done()
				return nil, errors.New("private cancellation cause")
			})})
			require.NoError(t, err)
			_, err = client.SendContext(ctx, birdTestEmail())
			require.ErrorIs(t, err, ErrEmailProviderSendFailed)
			if tc.deadline {
				require.ErrorIs(t, err, context.DeadlineExceeded)
			} else {
				require.ErrorIs(t, err, context.Canceled)
			}
			require.NotContains(t, err.Error(), "private")
			if tc.before {
				require.Zero(t, calls)
			} else {
				require.Equal(t, 1, calls)
			}
		})
	}
}

func TestBirdUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider *BirdEmailProvider
	}{
		{"nil provider", nil}, {"nil client", NewBirdEmailProvider(nil)}, {"zero client", NewBirdEmailProvider(&BirdClient{})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.False(t, tc.provider.IsHealthy(context.Background()))
			result, err := tc.provider.Send(context.Background(), birdTestEmail())
			require.ErrorIs(t, err, ErrEmailProviderUnavailable)
			require.False(t, result.Success)
		})
	}
	for _, tc := range []struct {
		name    string
		request *NewBirdClientRequest
	}{
		{"nil", nil}, {"zero", &NewBirdClientRequest{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := NewBirdClient(tc.request)
			require.Nil(t, client)
			require.ErrorIs(t, err, ErrEmailProviderUnavailable)
			if tc.request == nil {
				require.Nil(t, tc.request.WithHTTPClient(http.DefaultClient))
			}
		})
	}
}

func TestBirdConcurrentContexts(t *testing.T) {
	for _, count := range []int{2, 20} {
		t.Run(strconv.Itoa(count)+" concurrent sends", func(t *testing.T) {
			type key struct{}
			var calls atomic.Int32
			client, err := NewBirdClient(&NewBirdClientRequest{APIKey: birdTestKey, Transport: birdTestTransport(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				var msg birdMessage
				if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
					return nil, err
				}
				if msg.Subject != r.Context().Value(key{}) {
					return nil, errors.New("context mismatch")
				}
				return birdTestResponse(202, birdTestReceipt), nil
			})})
			require.NoError(t, err)
			var wg sync.WaitGroup
			errs := make(chan error, count)
			for i := range count {
				wg.Add(1)
				go func() {
					defer wg.Done()
					email := birdTestEmail()
					email.Subject = time.Duration(i).String()
					_, err := client.SendContext(context.WithValue(context.Background(), key{}, email.Subject), email)
					errs <- err
				}()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				require.NoError(t, err)
			}
			require.Equal(t, int32(count), calls.Load())
		})
	}
}

func TestBirdContextReadiness(t *testing.T) {
	for _, tc := range []struct {
		name               string
		cancelled, missing bool
	}{
		{name: "active"}, {name: "cancelled", cancelled: true}, {name: "missing", missing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancelCause(context.Background())
			defer cancel(nil)
			if tc.cancelled {
				cancel(errors.New("private-context-cause"))
			}
			if tc.missing {
				ctx = nil
			}
			calls := 0
			client, err := NewBirdClient(&NewBirdClientRequest{APIKey: birdTestKey, Transport: birdTestTransport(func(*http.Request) (*http.Response, error) {
				calls++
				return birdTestResponse(202, birdTestReceipt), nil
			})})
			require.NoError(t, err)
			provider := NewBirdEmailProvider(client)
			require.Equal(t, !tc.cancelled && !tc.missing, provider.IsHealthy(ctx))
			require.Zero(t, calls, "readiness must never make a network probe")
			result, err := provider.Send(ctx, birdTestEmail())
			if tc.cancelled || tc.missing {
				require.ErrorIs(t, err, ErrEmailProviderSendFailed)
				require.NotContains(t, err.Error(), "private-context-cause")
				require.False(t, result.Success)
				require.Zero(t, calls)
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, calls)
			}
		})
	}
}
