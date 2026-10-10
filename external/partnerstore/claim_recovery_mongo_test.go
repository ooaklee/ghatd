package partnerstore

import (
	"testing"

	"github.com/ooaklee/ghatd/external/partnerearnings"
	"github.com/stretchr/testify/require"
)

// Audit disposition: named actual-replica-set recovery cases prove original
// claim/receipt consistency after lost acknowledgement, with actor isolation.
func TestMongoClaimRequestReceiptRecovery(t *testing.T) {
	cases := []struct {
		name                                                                   string
		uncertain, wrongActor, wrongPartner, wrongKey, changedReason, rejected bool
		want                                                                   error
	}{
		{name: "original_frozen_destination_and_claim"},
		{name: "lost_creation_acknowledgement", uncertain: true},
		{name: "another_actor_has_no_receipt", wrongActor: true, want: partnerearnings.ErrNotFound},
		{name: "another_partner_has_no_receipt", wrongPartner: true, want: partnerearnings.ErrNotFound},
		{name: "another_key_has_no_receipt", wrongKey: true, want: partnerearnings.ErrNotFound},
		{name: "changed_request_reason_conflicts", changedReason: true},
		{name: "claim_decision_preserves_original_request_reason", rejected: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, store, _, clock, ctx := mongoEarnings(t)
			accrue(t, svc, ctx, clock, "payment", 10000)
			req := claimRequest(1000, "claim-key")
			req.Reason = "owner requested assistance"
			creator := svc
			if tc.uncertain {
				repo, err := NewEarningsRepository(injectedStore{Store: store, uncertain: true}, svc.Config())
				require.NoError(t, err)
				creator, err = partnerearnings.NewService(repo, clock, randomIDs{}, svc.Config())
				require.NoError(t, err)
			}
			created, err := creator.RequestClaim(ctx, req)
			if tc.uncertain {
				require.ErrorIs(t, err, partnerearnings.ErrUncertain)
			} else {
				require.NoError(t, err)
			}
			if tc.changedReason {
				changed := req
				changed.Reason = "different request"
				_, err := svc.RequestClaim(ctx, changed)
				require.ErrorIs(t, err, partnerearnings.ErrConflict)
			}
			if tc.rejected {
				created, err = svc.DecideClaim(ctx, partnerearnings.ClaimDecision{ClaimID: created.ID, NewState: partnerearnings.ClaimRejected, ActorID: "operator", Reason: "processing decision", ExpectedRevision: created.Revision})
				require.NoError(t, err)
			}
			actor, partner, key := req.ActorID, req.PartnerID, req.IdempotencyKey
			if tc.wrongActor {
				actor = "another-operator"
			}
			if tc.wrongPartner {
				partner = "another-partner"
			}
			if tc.wrongKey {
				key = "another-key"
			}
			before, err := svc.ListJournal(ctx, req.PartnerID)
			require.NoError(t, err)
			recovered, err := svc.FindClaimRequest(ctx, actor, partner, key)
			require.ErrorIs(t, err, tc.want)
			if tc.want == nil {
				require.NotEmpty(t, recovered.ID)
				require.Equal(t, req.DestinationID, recovered.DestinationID)
				require.Equal(t, req.DestinationSnapshot, recovered.DestinationSnapshot)
				require.Equal(t, req.Reason, recovered.RequestedReason)
				if !tc.uncertain {
					require.Equal(t, created, recovered)
				}
				again, err := svc.FindClaimRequest(ctx, actor, partner, key)
				require.NoError(t, err)
				require.Equal(t, recovered, again)
			}
			after, err := svc.ListJournal(ctx, req.PartnerID)
			require.NoError(t, err)
			require.Equal(t, before, after)
		})
	}
}
