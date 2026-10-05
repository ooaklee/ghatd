package emailprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/ooaklee/ghatd/external/observability"
)

// NewPostmarkClientRequest describes a single server token and its reviewed streams.
// A stream name is not proof of its live type; the operator owns that configuration.
type NewPostmarkClientRequest struct {
	// ServerToken authenticates this account; never expose it in logs or inbox metadata.
	ServerToken string
	// TransactionalStream defaults to Postmark's outbound stream.
	TransactionalStream string
	// MarketingStream is an opt-in broadcast stream with unsubscribe handling.
	MarketingStream string
	// HTTPClient is privately copied; redirects, cookies and automatic replay are disabled.
	HTTPClient *http.Client
	// Transport optionally overrides the borrowed client transport for host composition.
	Transport http.RoundTripper
}

// WithHTTPClient returns a request copy borrowing the host instrumented transport.
func (r *NewPostmarkClientRequest) WithHTTPClient(client *http.Client) *NewPostmarkClientRequest {
	if r == nil {
		return nil
	}
	copy := *r
	copy.HTTPClient = client
	return &copy
}

// PostmarkClient borrows a transport and submits once; it never verifies delivery.
type PostmarkClient struct {
	token, transactional, marketing string
	httpClient                      *http.Client
}

// NewPostmarkClient validates local configuration without accessing credentials or streams remotely.
func NewPostmarkClient(r *NewPostmarkClientRequest) (*PostmarkClient, error) {
	if r == nil || !birdIdentifier(r.ServerToken, 512) {
		return nil, ErrEmailProviderUnavailable
	}
	transactional := r.TransactionalStream
	if transactional == "" {
		transactional = "outbound"
	}
	if !birdIdentifier(transactional, 100) || (r.MarketingStream != "" && (!birdIdentifier(r.MarketingStream, 100) || r.MarketingStream == transactional)) {
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
	return &PostmarkClient{token: r.ServerToken, transactional: transactional, marketing: r.MarketingStream, httpClient: client}, nil
}

// PostmarkEmailProvider supports transactional mail and, when configured, broadcast single messages.
// It deliberately exposes no audience/campaign, webhook or lookup capability.
type PostmarkEmailProvider struct{ client *PostmarkClient }

// NewPostmarkEmailProvider binds one immutable account and its configured streams.
func NewPostmarkEmailProvider(c *PostmarkClient) *PostmarkEmailProvider {
	return &PostmarkEmailProvider{client: c}
}

// Name identifies the vendor; configured account identity belongs to ProviderRegistration.ID.
func (p *PostmarkEmailProvider) Name() string { return "POSTMARK" }

// IsHealthy checks local readiness only and makes no remote account or delivery claim.
func (p *PostmarkEmailProvider) IsHealthy(ctx context.Context) bool {
	return p != nil && p.client != nil && ctx != nil && ctx.Err() == nil
}

// SupportedMailTypes includes marketing only when a broadcast stream is configured.
func (p *PostmarkEmailProvider) SupportedMailTypes() []MailType {
	types := []MailType{Transactional}
	if p != nil && p.client != nil && p.client.marketing != "" {
		types = append(types, Marketing)
	}
	return types
}

// MailTypePreference defaults to unranked eligible-provider rotation.
func (p *PostmarkEmailProvider) MailTypePreference() []MailType { return nil }

// WithMailTypePreference decorates this account with a copied routing preference.
func (p *PostmarkEmailProvider) WithMailTypePreference(types []MailType) *PreferredProvider {
	return WithMailTypePreference(p, types)
}

type postmarkMessage struct {
	From, To, ReplyTo, Subject, HtmlBody, TextBody, MessageStream string
	TrackOpens                                                    bool
	TrackLinks                                                    string
}
type postmarkReceipt struct {
	MessageID string
	ErrorCode *int
}

// Send preserves the documented inline fields and stream, sanitizing all failures.
// Network/invalid responses after submission are uncertain and must not be blindly retried.
func (p *PostmarkEmailProvider) Send(ctx context.Context, email *Email) (*SendResult, error) {
	result := &SendResult{Provider: "POSTMARK", State: Failed}
	fail := func(err error) (*SendResult, error) { result.Error = err; return result, err }
	if !p.IsHealthy(ctx) {
		return fail(ErrEmailProviderUnavailable)
	}
	if err := validateBirdEmail(email); err != nil {
		return fail(err)
	}
	purpose := email.MailType
	if purpose == "" {
		purpose = Transactional
	}
	stream := p.client.transactional
	if purpose == Marketing {
		stream = p.client.marketing
	}
	if !purpose.Valid() || stream == "" {
		return fail(ErrEmailProviderInvalidEmail)
	}
	message := postmarkMessage{From: email.From, To: email.To, ReplyTo: email.ReplyTo, Subject: email.Subject, HtmlBody: email.HTMLBody, TextBody: email.TextBody, MessageStream: stream, TrackLinks: "None"}
	body, err := json.Marshal(message)
	if err != nil || len(body) > birdRequestLimit {
		return fail(ErrEmailProviderInvalidEmail)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.postmarkapp.com/email", io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return fail(ErrEmailProviderSendFailed)
	}
	req.Header.Set("X-Postmark-Server-Token", p.client.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	result.State = Uncertain
	response, err := p.client.httpClient.Do(req)
	if err != nil {
		return fail(ErrEmailProviderSendFailed)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, birdResponseLimit+1))
	if err != nil || len(data) > birdResponseLimit {
		return fail(ErrEmailProviderSendFailed)
	}
	if response.StatusCode >= 400 && response.StatusCode < 500 {
		result.State = Failed
		return fail(ErrEmailProviderSendFailed)
	}
	if response.StatusCode != 200 {
		return fail(ErrEmailProviderSendFailed)
	}
	var receipt postmarkReceipt
	decoder := json.NewDecoder(bytes.NewReader(data))
	if decoder.Decode(&receipt) != nil {
		return fail(ErrEmailProviderSendFailed)
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return fail(ErrEmailProviderSendFailed)
	}
	if receipt.ErrorCode == nil {
		return fail(ErrEmailProviderSendFailed)
	}
	if *receipt.ErrorCode != 0 {
		result.State = Failed
		return fail(ErrEmailProviderSendFailed)
	}
	if !birdIdentifier(strings.TrimSpace(receipt.MessageID), 512) {
		return fail(ErrEmailProviderSendFailed)
	}
	result.MessageID = receipt.MessageID
	result.Success = true
	result.State = Accepted
	return result, nil
}
