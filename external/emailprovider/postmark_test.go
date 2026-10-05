package emailprovider

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/observability"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

func TestPostmarkConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name    string
		req     *NewPostmarkClientRequest
		wantErr bool
	}{
		{"nil", nil, true}, {"missing token", &NewPostmarkClientRequest{}, true}, {"header injection", &NewPostmarkClientRequest{ServerToken: "test\nsecret"}, true}, {"transactional", &NewPostmarkClientRequest{ServerToken: "test-server-token"}, false}, {"broadcast", &NewPostmarkClientRequest{ServerToken: "test-server-token", MarketingStream: "broadcast"}, false}, {"same stream", &NewPostmarkClientRequest{ServerToken: "test-server-token", TransactionalStream: "same", MarketingStream: "same"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := NewPostmarkClient(tc.req)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.LessOrEqual(t, c.httpClient.Timeout, 30*time.Second)
			require.Nil(t, c.httpClient.Jar)
			require.ErrorIs(t, c.httpClient.CheckRedirect(nil, nil), http.ErrUseLastResponse)
		})
	}
}

func TestPostmarkInlineMapping(t *testing.T) {
	for _, tc := range []struct {
		name       string
		purpose    MailType
		marketing  string
		wantStream string
		html, text string
	}{
		{"legacy transactional", "", "", "outbound", "<p>Test</p>", "Test"}, {"explicit transactional", Transactional, "broadcast", "outbound", "<p>Test</p>", "Test"}, {"marketing broadcast", Marketing, "broadcast", "broadcast", "<p>Test</p>", "Test"}, {"plain text", Transactional, "", "outbound", "", "Test"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			email := birdTestEmail()
			email.MailType = tc.purpose
			email.HTMLBody = tc.html
			email.TextBody = tc.text
			original := *email
			client, err := NewPostmarkClient(&NewPostmarkClientRequest{ServerToken: "test-server-token", MarketingStream: tc.marketing, HTTPClient: &http.Client{Transport: birdTestTransport(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "https://api.postmarkapp.com/email", req.URL.String())
				require.Equal(t, "test-server-token", req.Header.Get("X-Postmark-Server-Token"))
				require.Nil(t, req.GetBody)
				require.Empty(t, req.Header.Get("Idempotency-Key"))
				body, err := io.ReadAll(req.Body)
				require.NoError(t, err)
				var msg map[string]any
				require.NoError(t, json.Unmarshal(body, &msg))
				require.Equal(t, email.From, msg["From"])
				require.Equal(t, email.To, msg["To"])
				require.Equal(t, email.ReplyTo, msg["ReplyTo"])
				require.Equal(t, email.Subject, msg["Subject"])
				require.Equal(t, tc.html, msg["HtmlBody"])
				require.Equal(t, tc.text, msg["TextBody"])
				require.Equal(t, tc.wantStream, msg["MessageStream"])
				require.Equal(t, false, msg["TrackOpens"])
				require.Equal(t, "None", msg["TrackLinks"])
				return birdTestResponse(200, `{"ErrorCode":0,"MessageID":"postmark-receipt"}`), nil
			})}})
			require.NoError(t, err)
			result, err := NewPostmarkEmailProvider(client).Send(context.Background(), email)
			require.NoError(t, err)
			require.Equal(t, Accepted, result.State)
			require.Equal(t, "postmark-receipt", result.MessageID)
			require.True(t, result.Success)
			require.Equal(t, 1, calls)
			require.Equal(t, original, *email)
		})
	}
}
func TestPostmarkOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		body         string
		transportErr error
		state        SendState
	}{
		{"validation rejection", 422, `{"Message":"private-token-canary"}`, nil, Failed},
		{"unauthorized", 401, "private-token-canary", nil, Failed},
		{"API rejection", 200, `{"ErrorCode":406,"Message":"private-token-canary"}`, nil, Failed},
		{"missing error code", 200, `{"MessageID":"receipt"}`, nil, Uncertain},
		{"missing receipt", 200, `{"ErrorCode":0}`, nil, Uncertain},
		{"malformed", 200, "private-token-canary", nil, Uncertain},
		{"extra JSON", 200, `{"ErrorCode":0,"MessageID":"receipt"}{}`, nil, Uncertain},
		{"oversized", 200, strings.Repeat("x", birdResponseLimit+1), nil, Uncertain},
		{"server error", 500, "private-token-canary", nil, Uncertain},
		{"redirect", 307, "", nil, Uncertain},
		{"uncertain timeout", 0, "", errors.New("private-token-canary"), Uncertain},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client, err := NewPostmarkClient(&NewPostmarkClientRequest{ServerToken: "test-server-token", HTTPClient: &http.Client{Transport: birdTestTransport(func(*http.Request) (*http.Response, error) {
				calls++
				if tc.transportErr != nil {
					return nil, tc.transportErr
				}
				return birdTestResponse(tc.status, tc.body), nil
			})}})
			require.NoError(t, err)
			result, err := NewPostmarkEmailProvider(client).Send(context.Background(), birdTestEmail())
			require.ErrorIs(t, err, ErrEmailProviderSendFailed)
			require.Equal(t, tc.state, result.State)
			require.NotContains(t, err.Error(), "private-token-canary")
			require.False(t, result.Success)
			require.Equal(t, 1, calls)
		})
	}
}
func TestPostmarkPreflight(t *testing.T) {
	for _, tc := range []struct {
		name   string
		email  *Email
		cancel bool
	}{
		{"nil", nil, false}, {"invalid recipient", &Email{From: "from@example.test", To: "bad", Subject: "Test", TextBody: "Test"}, false}, {"marketing not configured", &Email{From: "from@example.test", To: "to@example.test", Subject: "Test", TextBody: "Test", MailType: Marketing}, false}, {"cancelled", birdTestEmail(), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c, err := NewPostmarkClient(&NewPostmarkClientRequest{ServerToken: "test-server-token", HTTPClient: &http.Client{Transport: birdTestTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected") })}})
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			result, err := NewPostmarkEmailProvider(c).Send(ctx, tc.email)
			require.Error(t, err)
			require.Equal(t, Failed, result.State)
			require.Zero(t, calls)
		})
	}
}

// Global trace state is restored per named case; parallel execution would invalidate the assertions.
func TestPostmarkHostTraceAndPrivacy(t *testing.T) {
	for _, tc := range []struct {
		name     string
		rejected bool
	}{{"accepted", false}, {"rejected", true}} {
		t.Run(tc.name, func(t *testing.T) {
			tracer, recorder := newRecordingTraceSDK(t)
			previous := otel.GetTextMapPropagator()
			otel.SetTextMapPropagator(propagation.TraceContext{})
			t.Cleanup(func() { otel.SetTextMapPropagator(previous) })
			var propagated string
			client, err := NewPostmarkClient(&NewPostmarkClientRequest{ServerToken: "private-token-canary", HTTPClient: observability.NewHTTPClient(birdTestTransport(func(req *http.Request) (*http.Response, error) {
				propagated = req.Header.Get("traceparent")
				if tc.rejected {
					return birdTestResponse(422, `{"Message":"private-content-canary"}`), nil
				}
				return birdTestResponse(200, `{"ErrorCode":0,"MessageID":"private-receipt-canary"}`), nil
			}), time.Second)})
			require.NoError(t, err)
			ctx, parent := tracer.Tracer("test").Start(context.Background(), "send")
			email := birdTestEmail()
			email.Subject = "private-content-canary"
			_, err = NewPostmarkEmailProvider(client).Send(ctx, email)
			if tc.rejected {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NotEmpty(t, propagated)
			children := recorder.Ended()
			require.Len(t, children, 1)
			require.Equal(t, parent.SpanContext().SpanID(), children[0].Parent().SpanID())
			serialized, _ := json.Marshal(children[0].Attributes())
			for _, private := range []string{"private-token-canary", "private-content-canary", "private-receipt-canary", "recipient@example.test"} {
				require.NotContains(t, string(serialized), private)
			}
			parent.End()
		})
	}
}
