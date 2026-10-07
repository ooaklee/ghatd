package referral

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"time"
)

func signupDigest(e Evidence) (string, error) {
	encoded, err := json.Marshal(e)
	if err != nil {
		return "", ErrInvalid
	}
	sum := sha256.Sum256(encoded)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// LookupSignup reconciles a durable original signup decision before attempting
// new admission. An authenticated immutable identity/evidence pair may replay
// after a later ownership correction, link retirement or acquisition pause.
// It never grants a new referral or bypasses the manager's current authority.
func (s *Service) LookupSignup(ctx context.Context, customer, signup string, evidence Evidence, createdAt time.Time) (Referral, error) {
	if err := s.ready(ctx); err != nil {
		return Referral{}, err
	}
	if ctx == nil || customer == "" || signup == "" || createdAt.IsZero() {
		return Referral{}, ErrInvalid
	}
	if evidence.ProgramID != ProgramID || evidence.Audience != "partner-signup" {
		return Referral{}, ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return Referral{}, err
	}
	digest, err := signupDigest(evidence)
	if err != nil {
		return Referral{}, err
	}
	history, err := s.repo.ListReferralHistory(ctx, ProgramID, customer)
	if err != nil {
		return Referral{}, err
	}
	var found Referral
	for _, r := range history {
		// Corrections retain the owning creation identity for audit, but are
		// separate prospective decisions rather than another signup acceptance.
		if r.SignupID != signup || r.SourceKind != "click" {
			continue
		}
		if r.ReferredCustomer != customer || r.ProgramID != ProgramID || r.EvidenceDigest != digest || !r.LockedAt.Equal(createdAt) {
			return Referral{}, ErrAlreadyReferred
		}
		if found.ID != "" && found.ID != r.ID {
			return Referral{}, ErrUnavailable
		}
		found = r
	}
	if found.ID == "" {
		return Referral{}, ErrNotFound
	}
	return found, nil
}
