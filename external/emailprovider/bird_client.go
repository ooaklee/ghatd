package emailprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ooaklee/ghatd/external/observability"
)

const (
	birdTimeout       = 30 * time.Second
	birdRequestLimit  = 1 << 20
	birdResponseLimit = 64 << 10
)

// NewBirdClientRequest configures the regional v1 transactional sender. It does
// not configure delivery tracking, templates, broadcasts or inbound mail.
type NewBirdClientRequest struct {
	// APIKey is a workspace key with emails write permission. Only bk_us1_ and
	// bk_eu1_ regional keys are supported; syntax validation does not verify access.
	APIKey string
	// BaseURL optionally asserts the matching HTTPS regional origin. Empty
	// selects the origin from APIKey. Arbitrary endpoints are deliberately rejected.
	BaseURL string
	// HTTPClient supplies a privately copied policy. Positive timeouts shorter
	// than 30 seconds are retained; cookies and redirects are always disabled.
	HTTPClient *http.Client
	// Transport overrides HTTPClient.Transport. Supply the host's privacy-safe
	// instrumented transport here or through HTTPClient; it must not retry sends.
	Transport http.RoundTripper
}

// WithHTTPClient returns an independent configuration copy without mutating r.
func (r *NewBirdClientRequest) WithHTTPClient(client *http.Client) *NewBirdClientRequest {
	if r == nil {
		return nil
	}
	copy := *r
	copy.HTTPClient = client
	return &copy
}

// BirdClient submits one inline transactional message per call. Configuration
// is immutable after construction; callers own shared transports and must not
// mutate an Email while sending it. Concurrent sends keep contexts isolated.
type BirdClient struct {
	// endpoint is the validated regional sending URL, never a caller-supplied path.
	endpoint string
	// apiKey remains private and is used only for the outbound Authorization header.
	apiKey string
	// httpClient is a private policy copy; its transport pool is borrowed.
	httpClient *http.Client
}

// NewBirdClient validates configuration without contacting Bird. It copies the
// supplied/default client, rejects redirects and cookie jars, and bounds calls
// to at most 30 seconds. Explicit transports retain their instrumentation;
// otherwise GHATD's privacy-safe OpenTelemetry transport is installed.
func NewBirdClient(r *NewBirdClientRequest) (*BirdClient, error) {
	if r == nil {
		return nil, ErrEmailProviderUnavailable
	}
	origin := ""
	for _, region := range []string{"us1", "eu1"} {
		prefix := "bk_" + region + "_"
		if strings.HasPrefix(r.APIKey, prefix) && birdIdentifier(strings.TrimPrefix(r.APIKey, prefix), 512) {
			origin = "https://" + region + ".platform.bird.com"
		}
	}
	if origin == "" || (r.BaseURL != "" && strings.TrimSuffix(r.BaseURL, "/") != origin) {
		return nil, ErrEmailProviderUnavailable
	}
	client := newPrivateHTTPClient(r.HTTPClient)
	if client.Timeout <= 0 || client.Timeout > birdTimeout {
		client.Timeout = birdTimeout
	}
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if r.Transport != nil {
		client.Transport = r.Transport
	}
	if client.Transport == nil {
		client.Transport = observability.NewRoundTripper(nil)
	}
	return &BirdClient{endpoint: origin + "/v1/email/messages", apiKey: r.APIKey, httpClient: client}, nil
}

// birdMessage deliberately exposes only the inline, single-recipient send
// contract. Tracking is explicitly disabled; transactional classification must
// never be used as a way to bypass marketing consent or suppressions.
type birdMessage struct {
	// From is one validated sender mailbox, optionally with a display name.
	From string `json:"from"`
	// To contains exactly the shared contract's single recipient.
	To []string `json:"to"`
	// ReplyTo is omitted unless the caller supplied one reply destination.
	ReplyTo []string `json:"reply_to,omitempty"`
	// Subject carries the validated inline subject without template expansion.
	Subject string `json:"subject"`
	// HTML preserves the caller-rendered HTML body, when present.
	HTML string `json:"html,omitempty"`
	// Text preserves a plain-text body, including text-only messages.
	Text string `json:"text,omitempty"`
	// Category is fixed to transactional; marketing use is unsupported.
	Category string `json:"category"`
	// TrackClicks explicitly disables link rewriting for operational messages.
	TrackClicks bool `json:"track_clicks"`
	// TrackOpens explicitly disables recipient open tracking.
	TrackOpens bool `json:"track_opens"`
}

// SendContext returns Bird's message ID only for a validated 202 acceptance.
// Acceptance is not delivery. The adapter makes no retries and sends no
// Idempotency-Key: Email has no durable operation identity. A failed/uncertain
// response after dispatch may have sent mail; callers must not retry blindly.
// Provider diagnostics and message contents are never returned as errors.
func (c *BirdClient) SendContext(ctx context.Context, email *Email) (string, error) {
	if c == nil || c.httpClient == nil {
		return "", ErrEmailProviderUnavailable
	}
	if ctx == nil {
		return "", ErrEmailProviderSendFailed
	}
	if ctx.Err() != nil {
		return "", birdSendFailure(ctx)
	}
	if err := validateBirdEmail(email); err != nil {
		return "", err
	}
	message := birdMessage{From: email.From, To: []string{email.To}, Subject: email.Subject,
		HTML: email.HTMLBody, Text: email.TextBody, Category: "transactional"}
	if email.ReplyTo != "" {
		message.ReplyTo = []string{email.ReplyTo}
	}
	body, err := json.Marshal(message)
	if err != nil || len(body) > birdRequestLimit {
		return "", ErrEmailProviderInvalidEmail
	}
	// Wrapping the reader keeps GetBody nil, preventing transparent replay of
	// this non-idempotent POST by the standard HTTP transport.
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return "", ErrEmailProviderSendFailed
	}
	request.ContentLength = int64(len(body))
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return "", birdSendFailure(ctx)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusAccepted {
		return "", ErrEmailProviderSendFailed
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, birdResponseLimit+1))
	if err != nil || len(data) > birdResponseLimit {
		return "", birdSendFailure(ctx)
	}
	var receipt struct {
		ID            string `json:"id"`
		Status        string `json:"status"`
		AcceptedCount int    `json:"accepted_count"`
	}
	if json.Unmarshal(data, &receipt) != nil || receipt.Status != "accepted" || receipt.AcceptedCount != 1 ||
		!strings.HasPrefix(receipt.ID, "em_") || !birdIdentifier(strings.TrimPrefix(receipt.ID, "em_"), 128) {
		return "", ErrEmailProviderSendFailed
	}
	return receipt.ID, nil
}

// validateBirdEmail bounds allocation and rejects malformed headers before any
// dispatch. The shared contract supports one recipient and one optional reply-to.
func validateBirdEmail(email *Email) error {
	if email == nil {
		return ErrEmailProviderInvalidEmail
	}
	if err := validateEmail(email); err != nil {
		return err
	}
	remaining := birdRequestLimit
	for _, value := range []string{email.To, email.From, email.ReplyTo, email.Subject, email.HTMLBody, email.TextBody} {
		if len(value) > remaining || !utf8.ValidString(value) {
			return ErrEmailProviderInvalidEmail
		}
		remaining -= len(value)
	}
	for _, value := range []string{email.To, email.From, email.ReplyTo} {
		if value == "" {
			continue
		}
		if strings.ContainsAny(value, "\r\n\x00") {
			return ErrEmailProviderInvalidEmail
		}
		if _, err := mail.ParseAddress(value); err != nil {
			return ErrEmailProviderInvalidEmail
		}
	}
	if strings.TrimSpace(email.Subject) == "" || strings.ContainsAny(email.Subject, "\r\n\x00") {
		return ErrEmailProviderInvalidEmail
	}
	return nil
}

// birdIdentifier admits bounded ASCII key/receipt syntax, not credential validity.
func birdIdentifier(value string, limit int) bool {
	if value == "" || len(value) > limit {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}

// birdSendFailure preserves only standard cancellation sentinels, never a
// caller-supplied context cause or the transport/provider's private error text.
func birdSendFailure(ctx context.Context) error {
	if err := ctx.Err(); err == context.Canceled || err == context.DeadlineExceeded {
		return errors.Join(ErrEmailProviderSendFailed, err)
	}
	return ErrEmailProviderSendFailed
}
