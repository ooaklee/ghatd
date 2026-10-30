package indexnow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/toolbox"
)

// DefaultEndpoint is the shared IndexNow endpoint used when Config.Endpoint is
// empty.
const DefaultEndpoint = "https://api.indexnow.org/indexnow"

// MaxURLsPerRequest is the protocol limit of URLs per POST submission.
const MaxURLsPerRequest = 10000

// DefaultTimeout bounds each outbound request; supplied HTTP clients are
// shallow-copied and forced to this bound (or Config.Timeout) regardless.
const DefaultTimeout = 15 * time.Second

const (
	minAPIKeyLength = 8
	maxAPIKeyLength = 128
)

// Config carries the optional wiring for a Service.
type Config struct {
	// SiteURL is the required canonical origin of the site being submitted. It
	// must be an absolute http(s) URL with a host; every submitted URL must
	// share its hostname (host or host:port authority).
	SiteURL string

	// Endpoint overrides the IndexNow API endpoint; empty uses DefaultEndpoint.
	// It must be an absolute http(s) URL and is validated at construction.
	Endpoint string

	// KeyLocation overrides where the key file is hosted: either an absolute
	// path within SiteURL (for example "/my-indexnow-key.txt") or an absolute
	// same-host http(s) URL. Empty defaults to "/<api key>.txt". Encoded or
	// traversing segments are rejected.
	KeyLocation string

	// HTTPClient overrides transport and cookie policy. The service never
	// mutates the supplied client: it shallow-copies it, preserving Transport
	// and Jar, and forces a bounded timeout plus a never-follow redirect
	// policy on the copy so the API key cannot leak via redirects. Nil uses a
	// default client with the same guarantees.
	HTTPClient *http.Client

	// Repository persists outbound attempt records. Nil disables persistence;
	// a typed-nil implementation is rejected as invalid wiring.
	Repository AttemptRepository

	// Timeout bounds each outbound request on the derived client. Zero uses
	// DefaultTimeout.
	Timeout time.Duration
}

// Service submits URLs to IndexNow and records attempt history. It is safe for
// concurrent use. Errors it produces and attempts it persists never contain the
// API key, request bodies, or raw transport diagnostics.
type Service struct {
	apiKey         string
	siteURL        *url.URL
	endpoint       string
	keyLocation    string
	keyLocationURL string
	defaultedKey   bool
	httpClient     *http.Client
	repository     AttemptRepository
}

// NewService validates the explicit API key and configuration and returns a
// ready Service. The API key must be 8-128 ASCII letters, digits, or hyphens;
// cfg.SiteURL is required and must be an absolute http(s) URL, and every
// submitted URL must share its hostname. Repository and HTTPClient are
// optional.
func NewService(apiKey string, cfg Config) (*Service, error) {
	if err := validateAPIKey(apiKey); err != nil {
		return nil, err
	}
	if cfg.Timeout < 0 || cfg.Repository != nil && nilDependency(cfg.Repository) ||
		cfg.HTTPClient != nil && (cfg.HTTPClient.Transport != nil && nilDependency(cfg.HTTPClient.Transport) || cfg.HTTPClient.Jar != nil && nilDependency(cfg.HTTPClient.Jar)) {
		return nil, ErrInvalidConfiguration
	}

	siteURL, err := parseSiteURL(cfg.SiteURL)
	if err != nil {
		return nil, err
	}

	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	endpointURL, err := parseEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	endpoint = endpointURL.String()

	keyLocation, keyLocationURL, err := resolveKeyLocation(cfg.KeyLocation, apiKey, siteURL)
	if err != nil {
		return nil, err
	}

	return &Service{
		apiKey:         apiKey,
		siteURL:        siteURL,
		endpoint:       endpoint,
		keyLocation:    keyLocation,
		keyLocationURL: keyLocationURL,
		defaultedKey:   strings.TrimSpace(cfg.KeyLocation) == "",
		httpClient:     boundedClient(cfg.HTTPClient, cfg.Timeout),
		repository:     cfg.Repository,
	}, nil
}

// SubmitURLsRequest is a batch submission request.
type SubmitURLsRequest struct {
	// URLs are absolute http(s) URLs on the configured site host, at most
	// MaxURLsPerRequest entries. The whole batch is validated before any
	// external side effect.
	URLs []string
}

// SubmitURLsResponse reports the outcome of a submission attempt.
type SubmitURLsResponse struct {
	// Outcome classifies the protocol result.
	Outcome Outcome

	// StatusCode is the HTTP status received, or 0 on transport failure.
	StatusCode int

	// Success is true for HTTP 200 (acknowledged) and HTTP 202 (pending),
	// which are distinct protocol results. It never implies the URLs were
	// indexed.
	Success bool

	// AttemptID identifies the recorded attempt; empty when persistence is
	// disabled or the attempt failed before recording.
	AttemptID string

	// Retryable reports whether the same request may be retried later. The
	// service never retries automatically.
	Retryable bool

	// RetryAfter carries the server-requested delay when throttled, parsed from
	// the Retry-After header; zero when absent.
	RetryAfter time.Duration
}

// SubmitURL submits a single URL via the protocol's GET form, with the URL
// escaped as a query parameter. It is equivalent to SubmitURLs with one URL.
func (s *Service) SubmitURL(ctx context.Context, rawURL string) (*SubmitURLsResponse, error) {
	if s == nil || s.siteURL == nil || s.httpClient == nil {
		return nil, ErrNilService
	}
	if err := ensureContext(ctx); err != nil {
		return nil, err
	}
	return s.submit(ctx, []string{rawURL}, http.MethodGet)
}

// SubmitURLs submits a batch of URLs via the protocol's POST form.
func (s *Service) SubmitURLs(ctx context.Context, req *SubmitURLsRequest) (*SubmitURLsResponse, error) {
	if s == nil || s.siteURL == nil || s.httpClient == nil {
		return nil, ErrNilService
	}
	if err := ensureContext(ctx); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, ErrNilRequest
	}
	return s.submit(ctx, req.URLs, http.MethodPost)
}

// submit validates, records, sends, and completes one attempt.
func (s *Service) submit(ctx context.Context, urls []string, method string) (*SubmitURLsResponse, error) {
	urls = append([]string(nil), urls...)
	if err := validateURLs(urls, s.siteURL); err != nil {
		return nil, err
	}
	// A key below the root only establishes ownership of its directory. Check
	// decoded, unambiguous paths after full-batch validation and before auditing.
	directory := s.keyLocation[:strings.LastIndex(s.keyLocation, "/")+1]
	if directory != "/" {
		for _, raw := range urls {
			u, _ := url.Parse(raw) // validateURLs already parsed every URL.
			if !strings.HasPrefix(u.Path, directory) {
				return nil, ErrInvalidKeyLocation
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, wrapContextError(err)
	}

	attempt := &Attempt{
		ID:        toolbox.GenerateUuidV4(),
		URLs:      urls,
		Method:    method,
		StartedAt: time.Now().UTC(),
		Outcome:   OutcomeInFlight,
	}

	if s.repository != nil {
		if err := s.repository.RecordAttempt(ctx, cloneSubmissionAttempt(attempt)); err != nil {
			return nil, &AuditError{}
		}
	}

	outcome, statusCode, retryAfter, sendErr := s.send(ctx, urls, method)

	attempt.FinishedAt = time.Now().UTC()
	attempt.StatusCode = statusCode
	attempt.Outcome = outcome
	attempt.Success = sendErr == nil
	attempt.Retryable = outcomeRetryable(outcome, sendErr)
	attempt.RetryAfter = retryAfter
	if sendErr != nil {
		attempt.FailureMessage = scrubMessage(sendErr)
	}

	var auditErr error
	if s.repository != nil {
		if ferr := s.repository.FinishAttempt(ctx, cloneSubmissionAttempt(attempt)); ferr != nil {
			// The submission result above remains authoritative; this error
			// only reports failed audit persistence.
			auditErr = &AuditError{Dispatched: true}
		}
	}

	resp := &SubmitURLsResponse{
		Outcome:    outcome,
		StatusCode: statusCode,
		Success:    attempt.Success,
		Retryable:  attempt.Retryable,
		RetryAfter: retryAfter,
	}
	if s.repository != nil {
		resp.AttemptID = attempt.ID
	}

	if sendErr != nil {
		return resp, errors.Join(sendErr, auditErr)
	}
	if auditErr != nil {
		return resp, auditErr
	}
	return resp, nil
}

// cloneSubmissionAttempt keeps repository argument ownership separate from the
// request used for dispatch and the authoritative result used for its receipt.
func cloneSubmissionAttempt(a *Attempt) *Attempt {
	c := *a
	c.URLs = append([]string(nil), a.URLs...)
	return &c
}

// send performs the outbound request under a bounded per-request timeout and
// classifies the outcome.
func (s *Service) send(ctx context.Context, urls []string, method string) (Outcome, int, time.Duration, error) {
	timeout := s.httpClient.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var (
		req *http.Request
		err error
	)
	if method == http.MethodGet {
		req, err = s.buildGetRequest(callCtx, urls[0])
	} else {
		req, err = s.buildPostRequest(callCtx, urls)
	}
	if err != nil {
		return OutcomeTransportError, 0, 0, classifyTransportError(err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return OutcomeTransportError, 0, 0, classifyTransportError(err)
	}
	defer drainAndClose(resp)

	outcome, retryable := asOutcome(resp.StatusCode)
	retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return outcome, resp.StatusCode, retryAfter, &Error{
			Outcome:    outcome,
			StatusCode: resp.StatusCode,
			Retryable:  retryable,
			RetryAfter: retryAfter,
		}
	}
	return outcome, resp.StatusCode, retryAfter, nil
}

// buildGetRequest builds the single-URL GET form with query escaping. A custom
// key location travels as its absolute site URL in the keyLocation query
// parameter per protocol.
func (s *Service) buildGetRequest(ctx context.Context, rawURL string) (*http.Request, error) {
	q := url.Values{}
	q.Set("url", rawURL)
	q.Set("key", s.apiKey)
	if !s.defaultedKey {
		q.Set("keyLocation", s.keyLocationURL)
	}
	return http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint+"?"+q.Encode(), nil)
}

// buildPostRequest builds the JSON batch POST form. A custom key location
// travels in the keyLocation JSON field as its absolute site URL per protocol.
func (s *Service) buildPostRequest(ctx context.Context, urls []string) (*http.Request, error) {
	payload := map[string]any{
		"host":    s.siteURL.Host,
		"key":     s.apiKey,
		"urlList": urls,
	}
	if !s.defaultedKey {
		payload["keyLocation"] = s.keyLocationURL
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	return req, nil
}

// drainAndClose bounds response-body retention and releases the connection.
func drainAndClose(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
}
