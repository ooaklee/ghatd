package teleprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// maxResponseBytes bounds each provider response to one MiB before JSON decoding.
const maxResponseBytes int64 = 1 << 20

// openWA contains all provider DTO, URL, credential and transport concerns.
// The client is copied to enforce redirect isolation without mutating its owner.
type openWA struct {
	// config retains server-side credentials, the explicit session and engine choice.
	config OpenWAConfig
	// client is a private copy that shares the supplied transport but refuses redirects.
	client *http.Client
	// base is the validated API URL copied before constructing each request.
	base *url.URL
	// timeout bounds the operation, including read retries and their delays.
	timeout time.Duration
	// readRetries bounds additional GET attempts; mutations are never replayed.
	readRetries int
}

// newOpenWA validates endpoint, credentials, explicit session, engine and read limits.
// It copies the HTTP client and disables redirects without making provider calls.
func newOpenWA(config Config) (*openWA, error) {
	c := config.OpenWA
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Host == "" || u.Hostname() == "" || u.RawPath != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || strings.ContainsAny(c.Endpoint, "\r\n\\") {
		return nil, invalid(CodeInvalidRequest)
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return nil, invalid(CodeInvalidRequest)
		}
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || (u.Hostname() != "localhost" && (ip == nil || !ip.IsLoopback())) {
			return nil, invalid(CodeInvalidRequest)
		}
	}
	if c.APIKey == "" || strings.TrimSpace(c.APIKey) != c.APIKey || strings.ContainsAny(c.APIKey, "\r\n") {
		return nil, invalid(CodeInvalidRequest)
	}
	id, err := uuid.Parse(c.SessionID)
	if err != nil || id == uuid.Nil || id.String() != c.SessionID {
		return nil, invalid(CodeInvalidRequest)
	}
	if c.Engine != "" && c.Engine != "baileys" && c.Engine != "whatsapp-web.js" {
		return nil, invalid(CodeInvalidRequest)
	}
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	if timeout < time.Millisecond || timeout > time.Minute {
		return nil, invalid(CodeInvalidRequest)
	}
	if config.ReadRetryLimit < 0 || config.ReadRetryLimit > 2 {
		return nil, invalid(CodeInvalidRequest)
	}
	client := http.Client{Transport: http.DefaultTransport}
	if config.HTTPClient != nil {
		client = *config.HTTPClient
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return &openWA{config: c, client: &client, base: u, timeout: timeout, readRetries: config.ReadRetryLimit}, nil
}

// Capabilities describes the configured OpenWA engine; an unspecified engine leaves
// group creation unknown. This report does not probe live session readiness.
func (p *openWA) Capabilities() Capabilities {
	return Capabilities{CheckNumber: true, DirectMessages: true, GroupMessages: true, JoinGroup: true, Replies: true, CreateGroup: p.config.Engine == "baileys", GroupCreationKnown: p.config.Engine != ""}
}

// path scopes an endpoint suffix to the explicitly configured session under the API base.
func (p *openWA) path(suffix string) string {
	return p.base.Path + "/sessions/" + p.config.SessionID + suffix
}

// retryAfter parses seconds or an HTTP date into a future retry delay; invalid or
// expired guidance returns zero.
func retryAfter(value string) time.Duration {
	if n, err := strconv.ParseInt(value, 10, 32); err == nil && n >= 0 {
		return time.Duration(n) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil {
		if d := time.Until(at); d > 0 {
			return d
		}
	}
	return 0
}

// call bounds the entire read/retry budget by the configured deadline and caller cancellation.
// Mutations are never automatically retried, including on redirects.
func (p *openWA) call(ctx context.Context, method, path string, query url.Values, body any, want int, out any) error {
	if ctx == nil {
		return invalid(CodeInvalidRequest)
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	for attempt := 0; ; attempt++ {
		err := p.callOnce(ctx, method, path, query, body, want, out)
		var failure *Error
		if err == nil || method != "GET" || attempt >= p.readRetries || !errors.As(err, &failure) || !failure.retryable || ctx.Err() != nil {
			return err
		}
		delay := time.Duration(100*(1<<attempt)) * time.Millisecond
		if failure.RetryAfter > delay {
			delay = failure.RetryAfter
		}
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= delay {
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return &Error{Code: CodeUnavailable, cause: ctx.Err()}
		case <-timer.C:
		}
	}
}

// callOnce performs one credential-bearing request, bounds and decodes a single
// JSON response, and translates failures without exposing provider diagnostics.
// Transport failures or malformed mutation receipts are classified as uncertain.
func (p *openWA) callOnce(ctx context.Context, method, path string, query url.Values, body any, want int, out any) error {
	if ctx == nil {
		return invalid(CodeInvalidRequest)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	u := *p.base
	u.Path = path
	u.RawQuery = query.Encode()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return invalid(CodeInvalidRequest)
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return invalid(CodeInvalidRequest)
	}
	request.Header.Set("X-API-Key", p.config.APIKey)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	mutation := method != "GET"
	response, err := p.client.Do(request)
	if err != nil {
		code := CodeUnavailable
		if mutation {
			code = CodeUncertain
		}
		return &Error{Code: code, Uncertain: mutation, cause: err, retryable: !mutation}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != want {
		e := &Error{Code: CodeUnavailable, status: response.StatusCode, RetryAfter: retryAfter(response.Header.Get("Retry-After"))}
		e.retryable = response.StatusCode == 429 || response.StatusCode == 502 || response.StatusCode == 503 || response.StatusCode == 504
		switch response.StatusCode {
		case 409:
			e.Code = CodeSessionNotReady
		case 429:
			e.Code = CodeRateLimited
		case 501:
			e.Code = CodeUnsupported
		case 401:
			e.Code = CodeAuthFailed
		case 403:
			e.Code = CodePermissionDenied
		case 400, 404, 422:
			if mutation {
				e.Code = CodeRejected
			}
		default:
			if mutation {
				e.Code = CodeUncertain
				e.Uncertain = true
			}
		}
		return e
	}
	encoded, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	// responseError preserves uncertain mutation outcomes when decoding cannot confirm success.
	responseError := func() error {
		if mutation {
			return &Error{Code: CodeUncertain, Uncertain: true}
		}
		return invalid(CodeInvalidResponse)
	}
	if err != nil || int64(len(encoded)) > maxResponseBytes {
		return responseError()
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	if decoder.Decode(out) != nil {
		return responseError()
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return responseError()
	}
	return nil
}

// CheckNumber removes only the leading + for OpenWA and requires a matching number,
// an explicit exists boolean and a canonical ID or null. Missing evidence is unknown,
// never an authoritative negative.
func (p *openWA) CheckNumber(ctx context.Context, number string) (Registration, error) {
	// body preserves missing fields separately from authoritative false/null evidence.
	var body struct {
		// Number is the echoed international digits without the leading +.
		Number string `json:"number"`
		// Exists must be present; a missing boolean cannot establish non-registration.
		Exists *bool `json:"exists"`
		// ID is a canonical contact ID for true, or explicit JSON null for false.
		ID json.RawMessage `json:"whatsappId"`
	}
	digits := strings.TrimPrefix(number, "+")
	if err := p.call(ctx, "GET", p.path("/contacts/check/"+digits), nil, nil, 200, &body); err != nil {
		return Registration{}, err
	}
	if body.Number != digits || body.Exists == nil || len(body.ID) == 0 {
		return Registration{}, invalid(CodeInvalidResponse)
	}
	r := Registration{Number: number, CheckedAt: time.Now().UTC()}
	if *body.Exists {
		if json.Unmarshal(body.ID, &r.CanonicalID) != nil || !contactID.MatchString(r.CanonicalID) {
			return Registration{}, invalid(CodeInvalidResponse)
		}
		r.Status = "registered"
	} else {
		if string(body.ID) != "null" {
			return Registration{}, invalid(CodeInvalidResponse)
		}
		r.Status = "not_registered"
		r.Code = CodeNotRegistered
	}
	return r, nil
}

// SendText selects the plain-text or quoted-reply endpoint and requires a 201
// acceptance receipt. It makes one mutation attempt and does not assert delivery.
func (p *openWA) SendText(ctx context.Context, req MessageRequest) (MessageReceipt, error) {
	body := map[string]string{"chatId": req.ChatID, "text": req.Text}
	path := "/messages/send-text"
	if req.QuotedMessageID != "" {
		body["quotedMessageId"] = req.QuotedMessageID
		path = "/messages/reply"
	}
	var response struct {
		// ID is the provider's opaque accepted-message identity.
		ID string `json:"messageId"`
		// Timestamp is provider acceptance time in Unix seconds and must be positive.
		Timestamp int64 `json:"timestamp"`
	}
	if err := p.call(ctx, "POST", p.path(path), nil, body, 201, &response); err != nil {
		return MessageReceipt{}, err
	}
	if !opaque(response.ID) || response.Timestamp <= 0 {
		return MessageReceipt{}, &Error{Code: CodeUncertain, Uncertain: true}
	}
	return MessageReceipt{State: "accepted", MessageID: response.ID, AcceptedAt: time.Unix(response.Timestamp, 0).UTC()}, nil
}

// CreateGroup submits resolved participant IDs to OpenWA once and maps its receipt.
// The owning service checks engine capability and validates the returned group ID.
func (p *openWA) CreateGroup(ctx context.Context, req GroupRequest) (GroupReceipt, error) {
	var response struct {
		// ID is the created group identity checked by the owning service.
		ID string `json:"id"`
		// Name must accompany a valid creation receipt.
		Name string `json:"name"`
	}
	body := map[string]any{"name": req.Name, "participants": req.Participants}
	if err := p.call(ctx, "POST", p.path("/groups"), nil, body, 201, &response); err != nil {
		return GroupReceipt{}, err
	}
	return GroupReceipt{GroupID: response.ID, Name: response.Name}, nil
}

// JoinGroup submits an invite code once, maps explicit invite rejection and requires
// a true success flag. The service separately validates the returned group ID.
func (p *openWA) JoinGroup(ctx context.Context, invite string) (GroupReceipt, error) {
	var response struct {
		// Success must be explicitly true to confirm invite acceptance.
		Success *bool `json:"success"`
		// GroupID is the joined group identity checked by the owning service.
		GroupID string `json:"groupId"`
	}
	if err := p.call(ctx, "POST", p.path("/groups/join"), nil, map[string]string{"inviteCode": invite}, 200, &response); err != nil {
		var failure *Error
		if errors.As(err, &failure) && (failure.status == 400 || failure.status == 404) {
			return GroupReceipt{}, invalid(CodeInvalidInvite)
		}
		return GroupReceipt{}, err
	}
	if response.Success == nil || !*response.Success {
		return GroupReceipt{}, &Error{Code: CodeUncertain, Uncertain: true}
	}
	return GroupReceipt{GroupID: response.GroupID}, nil
}

// ListMessages reads bounded stored rows for one session/chat without expanding media.
// The after cursor walks towards older rows; rejected cursors report a visible gap.
// Session identity and timestamps are checked before the service validates and filters rows.
func (p *openWA) ListMessages(ctx context.Context, req MessageQuery) (MessagePage, error) {
	var response struct {
		// Messages distinguishes an empty stored page from missing/null response data.
		Messages *[]struct {
			// ID is the storage row ID used by older-page cursors.
			ID string `json:"id"`
			// MessageID is the distinct engine-specific WhatsApp message ID.
			MessageID string `json:"waMessageId"`
			// SessionID must match the explicitly configured session.
			SessionID string `json:"sessionId"`
			// ChatID is checked against the requested conversation by the service.
			ChatID string `json:"chatId"`
			// From preserves the provider sender identity, including group senders.
			From string `json:"from"`
			// Author preserves an optional individual group author identity.
			Author string `json:"author"`
			// Direction is validated as incoming/outgoing by the service.
			Direction string `json:"direction"`
			// Body contains the stored message body without inline media expansion.
			Body string `json:"body"`
			// Type preserves the provider message-kind label.
			Type string `json:"type"`
			// Status preserves provider acknowledgement/status evidence without a delivery claim.
			Status string `json:"status"`
			// Timestamp contains a positive message time in Unix seconds.
			Timestamp int64 `json:"timestamp"`
			// Metadata carries quote context without interpreting engine-specific identities.
			Metadata struct {
				// QuotedMessage contains the optional quoted engine message identity.
				QuotedMessage struct {
					// ID is absent when quote identity is unknown.
					ID string `json:"id"`
				} `json:"quotedMessage"`
			} `json:"metadata"`
		} `json:"messages"`
		// Total must be present and at least the number of returned stored rows.
		Total *int `json:"total"`
	}
	query := url.Values{"chatId": {req.ChatID}, "limit": {strconv.Itoa(req.Limit)}, "inlineMedia": {"false"}}
	if req.After != "" {
		query.Set("after", req.After)
	}
	if err := p.call(ctx, "GET", p.path("/messages"), query, nil, 200, &response); err != nil {
		var failure *Error
		if req.After != "" && errors.As(err, &failure) && failure.status == 400 {
			return MessagePage{}, invalid(CodeCursorUnavailable)
		}
		return MessagePage{}, err
	}
	if response.Messages == nil || response.Total == nil || *response.Total < len(*response.Messages) {
		return MessagePage{}, invalid(CodeInvalidResponse)
	}
	page := MessagePage{Messages: []Message{}}
	for _, m := range *response.Messages {
		if m.SessionID != p.config.SessionID || m.Timestamp <= 0 {
			return MessagePage{}, invalid(CodeInvalidResponse)
		}
		// Query identity is checked again in the service. Do not reinterpret @lid.
		page.Messages = append(page.Messages, Message{RowID: m.ID, MessageID: m.MessageID, SessionID: m.SessionID, ChatID: m.ChatID, Sender: m.From, Author: m.Author, Direction: m.Direction, Text: m.Body, Type: m.Type, Status: m.Status, Timestamp: time.Unix(m.Timestamp, 0).UTC(), QuotedMessageID: m.Metadata.QuotedMessage.ID})
	}
	return page, nil
}
