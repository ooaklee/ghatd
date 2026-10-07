package referral

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// reviewedCorrection constructs trusted owning input for lower-level tests;
// manager tests separately verify that a browser cannot supply these facts.
func reviewedCorrection(t *testing.T, s *Service, req CorrectionRequest) CorrectionRequest {
	t.Helper()
	state, err := s.GetAttributionSnapshot(context.Background(), req.ReferredCustomer)
	require.NoError(t, err)
	req.Mode = CorrectionProspective
	req.IdempotencyKey = "review-" + req.Partner.PartnerID + "-" + req.Reason
	req.ExpectedSnapshotFingerprint = state.Fingerprint
	req.PreviewFingerprint = state.Fingerprint
	req.SignupID = state.Head.SignupID
	if req.SignupID == "" {
		req.SignupID = "signup-fixture"
	}
	req.SignupCreatedAt = s.clock.Now()
	if len(state.History) > 0 {
		req.SignupCreatedAt = state.History[0].LockedAt
	}
	return req
}
