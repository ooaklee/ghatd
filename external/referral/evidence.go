package referral

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

// EvidenceConfig supplies host-owned versioned keys and the approved attribution
// window. Keys are server-only; a public link never grants authentication.
type EvidenceConfig struct {
	ProgramID   string
	ActiveKeyID string
	Keys        map[string][]byte
	Window      time.Duration
	// VisitWindow and VisitIDs explicitly opt into bounded visit measurement.
	// They do not change signup attribution validity or financial admission.
	VisitWindow time.Duration
	VisitIDs    IDGenerator
}

// Evidence records the signed signup attribution context. The caller must
// additionally recheck the owning link and partner and verify a new account.
type Evidence struct {
	ProgramID       string    `json:"program_id"`
	Audience        string    `json:"audience"`
	Code            string    `json:"code"`
	LinkID          string    `json:"link_id"`
	IssuedAt        time.Time `json:"issued_at"`
	ExpiresAt       time.Time `json:"expires_at"`
	MeasuredClickID string    `json:"measured_click_id,omitempty"`
	// MeasuredOccurredAt freezes the owning first observation's time. Reports
	// can retain its cohort after raw analytics expire, without another lookup.
	MeasuredOccurredAt *time.Time `json:"measured_occurred_at,omitempty"`
}

// EvidenceSigner owns the versioned wire signature, with no persistence or
// transport I/O. Hosts keep old verification keys during their token window.
type EvidenceSigner struct {
	config EvidenceConfig
	clock  Clock
}

// NewEvidenceSigner validates and copies the configured key ring. Missing keys
// fail closed; a process-local random fallback would invalidate tokens on restart.
func NewEvidenceSigner(config EvidenceConfig, clock Clock) (*EvidenceSigner, error) {
	if config.ProgramID == "" || len(config.ProgramID) > 128 || len(config.ActiveKeyID) > 64 || config.ActiveKeyID == "" || strings.Contains(config.ActiveKeyID, ".") || config.Window <= 0 || config.Window > 180*24*time.Hour || nilReferralDependency(clock) {
		return nil, ErrInvalid
	}
	if config.VisitWindow < 0 || config.VisitWindow > 24*time.Hour || (config.VisitWindow > 0 && (config.VisitWindow < time.Second || nilReferralDependency(config.VisitIDs))) {
		return nil, ErrInvalid
	}
	keys := make(map[string][]byte, len(config.Keys))
	for id, key := range config.Keys {
		if id == "" || len(id) > 64 || strings.Contains(id, ".") || len(key) < 32 {
			return nil, ErrInvalid
		}
		keys[id] = append([]byte(nil), key...)
	}
	if len(keys[config.ActiveKeyID]) < 32 {
		return nil, ErrUnavailable
	}
	config.Keys = keys
	return &EvidenceSigner{config: config, clock: clock}, nil
}

// Issue signs a consented visit to an active link. The host owns consent and
// abuse admission, then stores the result in a bounded secure signup cookie.
func (s *EvidenceSigner) Issue(link Link) (string, Evidence, error) {
	return s.issue(link, "", nil)
}

// IssueMeasured signs only an eligible observation returned by the owning
// service for this link. The caller must not substitute browser click fields.
func (s *EvidenceSigner) IssueMeasured(link Link, click Click) (string, Evidence, error) {
	if click.LinkID != link.ID || click.Code != link.Code || click.Classification != VisitEligible || click.ID != click.MeasuredClickID || !analyticsID(click.ID, 256) || click.OccurredAt.IsZero() || click.OccurredAt.Before(link.CreatedAt) || click.OccurredAt.After(s.clock.Now()) || !validCorrectionFingerprint(click.VisitDigest) {
		return "", Evidence{}, ErrDenied
	}
	at := click.OccurredAt.UTC()
	return s.issue(link, click.ID, &at)
}

func (s *EvidenceSigner) issue(link Link, measured string, occurred *time.Time) (string, Evidence, error) {
	if link.ProgramID != s.config.ProgramID || link.ID == "" || len(link.ID) > 128 || link.Code == "" || len(link.Code) > 128 || link.RetiredAt != nil {
		return "", Evidence{}, ErrDenied
	}
	now := s.clock.Now().UTC()
	if now.IsZero() || !validMeasuredOrigin(measured, occurred, now) {
		return "", Evidence{}, ErrDenied
	}
	evidence := Evidence{ProgramID: s.config.ProgramID, Audience: "partner-signup", Code: link.Code, LinkID: link.ID, IssuedAt: now, ExpiresAt: now.Add(s.config.Window), MeasuredClickID: measured, MeasuredOccurredAt: occurred}
	payload, err := json.Marshal(evidence)
	if err != nil {
		return "", Evidence{}, err
	}
	message := s.config.ActiveKeyID + "." + base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, s.config.Keys[s.config.ActiveKeyID])
	_, _ = mac.Write([]byte(message))
	return message + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), evidence, nil
}

// Verify authenticates evidence at the trusted account-creation timestamp, so
// delayed durable processing neither extends the signup window nor loses an
// eligible signup. Never accept this timestamp from the browser as authority.
func (s *EvidenceSigner) Verify(token string, accountCreatedAt time.Time) (Evidence, error) {
	if len(token) > 2048 || accountCreatedAt.IsZero() {
		return Evidence{}, ErrDenied
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 || len(s.config.Keys[parts[0]]) < 32 {
		return Evidence{}, ErrDenied
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Evidence{}, ErrDenied
	}
	mac := hmac.New(sha256.New, s.config.Keys[parts[0]])
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return Evidence{}, ErrDenied
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Evidence{}, ErrDenied
	}
	var evidence Evidence
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&evidence); err != nil {
		return Evidence{}, ErrDenied
	}
	var extra any
	if !errors.Is(decoder.Decode(&extra), io.EOF) {
		return Evidence{}, ErrDenied
	}
	if evidence.ProgramID != s.config.ProgramID || evidence.Audience != "partner-signup" || evidence.Code == "" || evidence.LinkID == "" || evidence.IssuedAt.IsZero() || !validMeasuredOrigin(evidence.MeasuredClickID, evidence.MeasuredOccurredAt, evidence.IssuedAt) || evidence.IssuedAt.After(accountCreatedAt) || !evidence.ExpiresAt.After(accountCreatedAt) || !evidence.ExpiresAt.After(evidence.IssuedAt) || evidence.ExpiresAt.Sub(evidence.IssuedAt) > s.config.Window {
		return Evidence{}, ErrDenied
	}
	return evidence, nil
}

// VerifyCurrent authenticates existing signup evidence at the owning current
// clock for visit reuse; Verify instead uses trusted account-creation time.
func (s *EvidenceSigner) VerifyCurrent(token string) (Evidence, error) {
	return s.Verify(token, s.clock.Now().UTC())
}

// VisitMeasurementEnabled reports explicit construction-time opt-in. It is
// independent of signup attribution and does not certify reporting coverage.
func (s *EvidenceSigner) VisitMeasurementEnabled() bool { return s.config.VisitWindow > 0 }

func validMeasuredOrigin(id string, at *time.Time, issued time.Time) bool {
	if id == "" {
		return at == nil
	}
	return analyticsID(id, 256) && at != nil && !at.IsZero() && !at.After(issued)
}
