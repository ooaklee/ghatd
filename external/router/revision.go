package router

import (
	"net/http"
	"strings"
)

// StrongETagPolicy validates one non-empty opaque revision, not a resource's
// current version. Domains must still compare revisions transactionally. Policy
// is fixed by the server; no option may be selected from request input.
type StrongETagPolicy struct {
	// MaxBytes bounds the normalized, quoted tag. Zero defaults to 256; values
	// below three are configuration errors. The quotes count toward the limit.
	MaxBytes int
	// TrimSpace permits surrounding whitespace before validation. False keeps
	// stricter transport contracts that require the exact quoted header value.
	TrimSpace bool
	// ASCIIOnly rejects obs-text bytes in addition to HTTP control characters.
	// False accepts HTTP's opaque obs-text range without interpreting its text.
	ASCIIOnly bool
}

// Validate accepts a single quoted non-empty strong tag, rejecting weak tags,
// wildcard/list forms, embedded quotes, spaces, and control characters. It never
// returns the supplied value in an error or logs revision material.
func (p StrongETagPolicy) Validate(value string) error {
	maximum := p.MaxBytes
	if maximum == 0 {
		maximum = 256
	}
	if maximum < 3 {
		return ErrRouteConfiguration
	}
	if p.TrimSpace {
		value = strings.TrimSpace(value)
	}
	if len(value) < 3 || len(value) > maximum || value[0] != '"' || value[len(value)-1] != '"' {
		return ErrRouteInvalidRevision
	}
	for _, b := range []byte(value[1 : len(value)-1]) {
		if b < 0x21 || b == '"' || b == 0x7f || (p.ASCIIOnly && b > 0x7e) {
			return ErrRouteInvalidRevision
		}
	}
	return nil
}

// IfMatch reads exactly one If-Match header and returns its validated value.
// Missing optional headers return an empty value; required absence returns
// ErrRoutePrecondition. Duplicate, empty, or malformed headers never fall back
// to absence. Trimming affects the returned value only when explicitly enabled.
func (p StrongETagPolicy) IfMatch(r *http.Request, required bool) (string, error) {
	if p.MaxBytes != 0 && p.MaxBytes < 3 {
		return "", ErrRouteConfiguration
	}
	if r == nil {
		return "", ErrRouteInvalidRevision
	}
	values := r.Header.Values("If-Match")
	if len(values) == 0 {
		if required {
			return "", ErrRoutePrecondition
		}
		return "", nil
	}
	if len(values) != 1 {
		return "", ErrRouteInvalidRevision
	}
	if err := p.Validate(values[0]); err != nil {
		return "", err
	}
	value := values[0]
	if p.TrimSpace {
		value = strings.TrimSpace(value)
	}
	return value, nil
}
