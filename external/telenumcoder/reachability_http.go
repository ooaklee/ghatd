package telenumcoder

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"
)

// HTTPConfig is trusted host configuration, never derived from a phone request.
// The secondary system accepts POST {number,channel} and returns {state}.
// No message is sent by this adapter, and destination numbers are never logged.
type HTTPConfig struct {
	Endpoint, Token string
	Client          *http.Client
	Timeout         time.Duration
}
type HTTPReachability struct {
	endpoint, token string
	client          *http.Client
}

func NewHTTPReachability(config HTTPConfig) (*HTTPReachability, error) {
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" || endpoint.RawQuery != "" {
		return nil, ErrLookupUnavailable
	}
	if config.Timeout == 0 {
		config.Timeout = 3 * time.Second
	}
	if config.Timeout < 100*time.Millisecond || config.Timeout > 10*time.Second {
		return nil, ErrLookupUnavailable
	}
	client := &http.Client{}
	if config.Client != nil {
		*client = *config.Client
	}
	client.Timeout = config.Timeout
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &HTTPReachability{endpoint: config.Endpoint, token: config.Token, client: client}, nil
}
func (p *HTTPReachability) Check(ctx context.Context, number string) (State, error) {
	body, err := json.Marshal(struct {
		Number  string `json:"number"`
		Channel string `json:"channel"`
	}{number, "sms"})
	if err != nil {
		return Unavailable, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint, bytes.NewReader(body))
	if err != nil {
		return Unavailable, ErrLookupUnavailable
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if p.token != "" {
		request.Header.Set("Authorization", "Bearer "+p.token)
	}
	response, err := p.client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return Unavailable, ctx.Err()
		}
		return Unavailable, ErrLookupUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return Unavailable, ErrLookupUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(raw) > 4096 {
		return Unavailable, ErrLookupUnavailable
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var value struct {
		State State `json:"state"`
	}
	if err = decoder.Decode(&value); err != nil {
		return Unavailable, ErrLookupUnavailable
	}
	if err = decoder.Decode(&struct{}{}); err != io.EOF {
		return Unavailable, ErrLookupUnavailable
	}
	if value.State != Reachable && value.State != Unreachable && value.State != Unavailable {
		return Unavailable, ErrLookupUnavailable
	}
	return value.State, nil
}
