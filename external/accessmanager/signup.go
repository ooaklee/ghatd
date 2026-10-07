package accessmanager

import "net/http"

// WithSignupEvidenceCookie enables browser signup capture during host startup.
// The host owns issuing a signed, consented, HttpOnly/SameSite cookie; this
// service only transports bounded evidence to the owning identity insertion.
// An empty or invalid name is rejected, and no cookie is enabled by default.
func (s *Service) WithSignupEvidenceCookie(name string) (*Service, error) {
	if s == nil || len(name) == 0 || len(name) > 128 || (&http.Cookie{Name: name, Value: "probe"}).Valid() != nil {
		return nil, ErrBadRequest
	}
	s.signupEvidenceCookie = name
	return s, nil
}

// readSignupEvidence declines malformed or ambiguous cookies without breaking
// account creation. Invalid signed evidence never grants attribution: the
// owning consumer independently verifies the immutable creation context.
func (s *Service) readSignupEvidence(cookies []*http.Cookie) string {
	if s == nil || s.signupEvidenceCookie == "" {
		return ""
	}
	var value string
	found := false
	for _, c := range cookies {
		if c == nil || c.Name != s.signupEvidenceCookie {
			continue
		}
		if found || len(c.Value) == 0 || len(c.Value) > 2048 {
			return ""
		}
		found = true
		value = c.Value
	}
	return value
}
