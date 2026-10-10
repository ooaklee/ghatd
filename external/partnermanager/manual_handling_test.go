package partnermanager

import (
	"context"
	"testing"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/stretchr/testify/require"
)

// This narrow port observes admission and argument forwarding only; real
// encrypted Mongo tests separately enforce claim transitions and settlements.
type manualDecisionPort struct {
	EarningsService
	request *partnerearnings.ClaimDecision
}

func (p *manualDecisionPort) DecideClaim(_ context.Context, req partnerearnings.ClaimDecision) (partnerearnings.Claim, error) {
	p.request = &req
	return partnerearnings.Claim{ID: req.ClaimID}, nil
}

func TestManualHandlingAdmissionPreservesRecoveryActions(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		enabled     bool
		revoked     bool
		revision    int64
		want        error
	}{
		{"new_handling_enabled", partnerearnings.ClaimProcessing, true, false, 1, nil},
		{"new_handling_paused", partnerearnings.ClaimProcessing, false, false, 1, ErrDenied},
		{"review_preserved_during_pause", partnerearnings.ClaimNeedsReview, false, false, 2, nil},
		{"confirmed_unsent_rejection_preserved", partnerearnings.ClaimRejected, false, false, 2, nil},
		{"confirmed_unsent_cancellation_preserved", partnerearnings.ClaimCancelled, false, false, 2, nil},
		{"revoked_processing_denied", partnerearnings.ClaimProcessing, true, true, 1, ErrDenied},
		{"revoked_review_denied_during_pause", partnerearnings.ClaimNeedsReview, false, true, 2, ErrDenied},
		{"missing_revision_invalid_even_during_pause", partnerearnings.ClaimProcessing, false, false, 0, ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, _, _, a, _, _ := managerFixture(t)
			p := &manualDecisionPort{}
			m.deps.Earnings = p
			m.deps.Controls.ManualRecording = tc.enabled
			a.deny = tc.revoked
			require.Equal(t, tc.enabled, m.ManualHandlingAdmitted())
			req := partnerearnings.ClaimDecision{ActorID: "operator", ClaimID: "selected-claim", NewState: tc.state, Reason: "checked external outcome", ExpectedRevision: tc.revision, ConfirmedUnsent: true}
			_, err := m.AdminDecideClaim(t.Context(), req)
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, []string{CapabilityProcessing}, a.calls)
			if tc.want != nil {
				require.Nil(t, p.request)
			} else {
				require.Equal(t, &req, p.request)
			}
		})
	}
}
