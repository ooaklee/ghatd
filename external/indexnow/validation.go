package indexnow

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func validateAPIKey(key string) error {
	if len(key) < minAPIKeyLength || len(key) > maxAPIKeyLength {
		return ErrInvalidAPIKey
	}
	for _, c := range []byte(key) {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
			return ErrInvalidAPIKey
		}
	}
	return nil
}

func nilDependency(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}

// parseHTTPURL requires a transport-safe absolute URL. Hostnames use DNS ASCII
// form (international names must be supplied in their punycode representation).
func parseHTTPURL(raw string) (*url.URL, error) {
	if raw == "" || strings.TrimSpace(raw) != raw || !utf8.ValidString(raw) || strings.Contains(raw, "\\") {
		return nil, ErrInvalidURL
	}
	for _, c := range raw {
		if c <= 0x20 || c == 0x7f {
			return nil, ErrInvalidURL
		}
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.User != nil || u.Fragment != "" || strings.Contains(raw, "#") || (u.Scheme != "http" && u.Scheme != "https") || !validHost(u) {
		return nil, ErrInvalidURL
	}
	return u, nil
}

func validHost(u *url.URL) bool {
	host := u.Hostname()
	if host == "" || strings.HasSuffix(u.Host, ":") {
		return false
	}
	if port := u.Port(); port != "" {
		n, e := strconv.Atoi(port)
		if e != nil || n < 1 || n > 65535 {
			return false
		}
	}
	if net.ParseIP(host) != nil {
		return true
	}
	if len(host) > 253 || strings.HasSuffix(host, ".") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func parseSiteURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrMissingSiteURL
	}
	u, err := parseHTTPURL(raw)
	if err != nil || u.RawQuery != "" || u.ForceQuery || (u.Path != "" && u.Path != "/") {
		return nil, ErrInvalidSiteURL
	}
	u.Path = ""
	u.RawPath = ""
	return u, nil
}

func parseEndpoint(raw string) (*url.URL, error) {
	u, err := parseHTTPURL(raw)
	if err != nil || u.RawQuery != "" || u.ForceQuery {
		return nil, ErrInvalidEndpoint
	}
	return u, nil
}

// resolveKeyLocation returns the exact HTTP route and ownership URL. Custom
// paths use unreserved ASCII segments so routing and directory scope agree.
func resolveKeyLocation(raw, key string, site *url.URL) (string, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		p := "/" + key + ".txt"
		return p, site.String() + p, nil
	}
	loc := *site
	if strings.HasPrefix(raw, "/") {
		loc.Path = raw
	} else {
		u, err := parseHTTPURL(raw)
		if err != nil || !sameAuthority(u, site) || u.RawQuery != "" || u.ForceQuery {
			return "", "", ErrInvalidKeyLocation
		}
		loc = *u
	}
	if err := validateKeyLocationPath(loc.Path); err != nil || loc.RawPath != "" {
		return "", "", ErrInvalidKeyLocation
	}
	return loc.Path, loc.String(), nil
}

func validateKeyLocationPath(p string) error {
	if !strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/") || strings.Contains(p, "//") {
		return ErrInvalidKeyLocation
	}
	for _, c := range p {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("/-._~", c)) {
			return ErrInvalidKeyLocation
		}
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "." || seg == ".." {
			return ErrInvalidKeyLocation
		}
	}
	return nil
}

func validateURLs(urls []string, site *url.URL) error {
	if len(urls) == 0 {
		return ErrNoURLs
	}
	if len(urls) > MaxURLsPerRequest {
		return fmt.Errorf("%w: maximum %d", ErrTooManyURLs, MaxURLsPerRequest)
	}
	for _, raw := range urls {
		u, err := parseHTTPURL(raw)
		if err != nil {
			return ErrInvalidURL
		}
		if !sameAuthority(u, site) {
			return ErrURLHostMismatch
		}
		if !safeURLPath(u) {
			return ErrInvalidURL
		}
	}
	return nil
}

// safeURLPath rejects ambiguous separators and traversal before scope checks.
// Normal percent-encoded path text is permitted, double-encoded aliases are not.
func safeURLPath(u *url.URL) bool {
	escaped := strings.ToLower(u.EscapedPath())
	if strings.Contains(escaped, "%2f") || strings.Contains(escaped, "%5c") || strings.Contains(escaped, "%25") || strings.Contains(u.Path, "//") {
		return false
	}
	for _, seg := range strings.Split(u.Path, "/") {
		if seg == "." || seg == ".." {
			return false
		}
	}
	for _, c := range u.Path {
		if c < 0x20 || c == 0x7f || c == '\\' {
			return false
		}
	}
	return true
}

// Default HTTP and HTTPS authorities cover one host. A nondefault port must
// agree explicitly, preventing an unrelated virtual service being submitted.
func sameAuthority(a, b *url.URL) bool {
	return strings.EqualFold(a.Hostname(), b.Hostname()) && nondefaultPort(a) == nondefaultPort(b)
}
func nondefaultPort(u *url.URL) string {
	p := u.Port()
	if p == "80" && u.Scheme == "http" || p == "443" && u.Scheme == "https" {
		return ""
	}
	return p
}

func refuseRedirect(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
func boundedClient(base *http.Client, timeout time.Duration) *http.Client {
	c := &http.Client{}
	if base != nil {
		*c = *base
	}
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	if c.Timeout > 0 && c.Timeout < timeout {
		timeout = c.Timeout
	}
	c.Timeout = timeout
	c.CheckRedirect = refuseRedirect
	return c
}

func classifyTransportError(err error) *Error {
	e := &Error{Outcome: OutcomeTransportError, Retryable: true, Cause: "network error"}
	if errors.Is(err, context.Canceled) {
		e.Cause = "context canceled"
	} else if errors.Is(err, context.DeadlineExceeded) {
		e.Cause = "deadline exceeded"
	}
	return e
}
func wrapContextError(err error) *Error { return classifyTransportError(err) }

func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.ParseUint(v, 10, 64); err == nil {
		if secs > uint64(math.MaxInt64/int64(time.Second)) {
			return time.Duration(math.MaxInt64)
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
func outcomeRetryable(outcome Outcome, err error) bool {
	if err == nil {
		return false
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Retryable
	}
	return outcome == OutcomeTransportError || outcome == OutcomeServerError
}
func scrubMessage(err error) string {
	if err == nil {
		return ""
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Error()
	}
	return "indexnow: submission failed"
}
func ensureContext(ctx context.Context) error {
	if ctx == nil {
		return ErrNilContext
	}
	return nil
}
