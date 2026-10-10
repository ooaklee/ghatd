package referral

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

// visitNonce is the signed payload inside a visit dedupe cookie, binding the
// nonce to one program, audience and link with issue/expiry times.
type visitNonce struct {
	ProgramID string    `json:"program_id"`
	Audience  string    `json:"audience"`
	LinkID    string    `json:"link_id"`
	Nonce     string    `json:"nonce"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// IssueVisit explicitly opts into a separate, bounded dedupe cookie. The host
// keeps it secure/HttpOnly and consent-gated; it grants no authentication or
// signup attribution. Raw nonce values are never retained in analytics storage.
func (s *EvidenceSigner) IssueVisit(link Link) (string, VisitIdentity, error) {
	if s.config.VisitWindow == 0 || nilReferralDependency(s.config.VisitIDs) {
		return "", VisitIdentity{}, ErrUnavailable
	}
	if link.ProgramID != s.config.ProgramID || !analyticsID(link.ID, 128) || link.RetiredAt != nil {
		return "", VisitIdentity{}, ErrDenied
	}
	nonce := s.config.VisitIDs.NewID()
	if !analyticsID(nonce, 128) {
		return "", VisitIdentity{}, ErrUnavailable
	}
	now := s.clock.Now().UTC()
	v := visitNonce{ProgramID: s.config.ProgramID, Audience: "partner-visit", LinkID: link.ID, Nonce: nonce, IssuedAt: now, ExpiresAt: now.Add(s.config.VisitWindow)}
	data, err := json.Marshal(v)
	if err != nil {
		return "", VisitIdentity{}, ErrInvalid
	}
	message := s.config.ActiveKeyID + "." + base64.RawURLEncoding.EncodeToString(data)
	mac := hmac.New(sha256.New, s.config.Keys[s.config.ActiveKeyID])
	_, _ = mac.Write([]byte(message))
	token := message + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return token, s.visitIdentity(s.config.ActiveKeyID, v), nil
}

// visitIdentity derives the link-scoped keyed digest retained for analytics
// from a verified nonce. It uses the signer's configured key and never exposes
// the raw nonce in the returned identity.
func (s *EvidenceSigner) visitIdentity(key string, v visitNonce) VisitIdentity {
	data, _ := json.Marshal([]string{"partner-measured-visit", v.ProgramID, v.LinkID, v.Nonce})
	mac := hmac.New(sha256.New, s.config.Keys[key])
	_, _ = mac.Write(data)
	return VisitIdentity{LinkID: v.LinkID, Digest: hex.EncodeToString(mac.Sum(nil)), ExpiresAt: v.ExpiresAt}
}

// VerifyVisit authenticates a same-link dedupe cookie at the injected current
// clock. It cannot be substituted for EvidenceSigner.Verify at signup time.
func (s *EvidenceSigner) VerifyVisit(token string, link Link) (VisitIdentity, error) {
	if s.config.VisitWindow == 0 {
		return VisitIdentity{}, ErrUnavailable
	}
	if len(token) > 2048 {
		return VisitIdentity{}, ErrDenied
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(s.config.Keys[parts[0]]) < 32 {
		return VisitIdentity{}, ErrDenied
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return VisitIdentity{}, ErrDenied
	}
	mac := hmac.New(sha256.New, s.config.Keys[parts[0]])
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return VisitIdentity{}, ErrDenied
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return VisitIdentity{}, ErrDenied
	}
	var v visitNonce
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(&v) != nil {
		return VisitIdentity{}, ErrDenied
	}
	var extra any
	if !errors.Is(d.Decode(&extra), io.EOF) {
		return VisitIdentity{}, ErrDenied
	}
	now := s.clock.Now().UTC()
	if v.ProgramID != s.config.ProgramID || v.Audience != "partner-visit" || v.LinkID != link.ID || link.ProgramID != v.ProgramID || link.RetiredAt != nil || !analyticsID(v.Nonce, 128) || v.IssuedAt.IsZero() || v.IssuedAt.After(now) || !v.ExpiresAt.After(now) || !v.ExpiresAt.After(v.IssuedAt) || v.ExpiresAt.Sub(v.IssuedAt) > s.config.VisitWindow {
		return VisitIdentity{}, ErrDenied
	}
	return s.visitIdentity(parts[0], v), nil
}
