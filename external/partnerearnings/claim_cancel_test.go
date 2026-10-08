package partnerearnings

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCustomerCancellationRejectsInvalidIntentBeforeWrites(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"empty_actor", ErrInvalid}, {"empty_partner", ErrInvalid}, {"empty_claim", ErrInvalid},
		{"empty_reason", ErrInvalid}, {"empty_key", ErrInvalid}, {"padded_key", ErrInvalid},
		{"control_in_reason", ErrInvalid}, {"oversized_reason", ErrInvalid},
		{"zero_revision", ErrInvalid}, {"negative_revision", ErrInvalid}, {"overflow_revision", ErrInvalid},
		{"nil_context", ErrUnavailable}, {"cancelled_context", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, repo, _ := newTestService(t)
			req := CancelClaimRequest{PartnerID: "partner", ActorID: "owner", ClaimID: "claim", ExpectedRevision: 1, Reason: "customer changed plans", IdempotencyKey: "original-key"}
			ctx := t.Context()
			switch tc.name {
			case "empty_actor":
				req.ActorID = ""
			case "empty_partner":
				req.PartnerID = ""
			case "empty_claim":
				req.ClaimID = ""
			case "empty_reason":
				req.Reason = ""
			case "empty_key":
				req.IdempotencyKey = ""
			case "padded_key":
				req.IdempotencyKey = " original-key "
			case "control_in_reason":
				req.Reason = "reason\x00extra"
			case "oversized_reason":
				req.Reason = strings.Repeat("a", maxReasonLength+1)
			case "zero_revision":
				req.ExpectedRevision = 0
			case "negative_revision":
				req.ExpectedRevision = -1
			case "overflow_revision":
				req.ExpectedRevision = math.MaxInt64
			case "nil_context":
				ctx = nil
			case "cancelled_context":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			out, err := s.CancelRequestedClaim(ctx, req)
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, out.ID)
			require.Empty(t, repo.claims)
			require.Empty(t, repo.entries)
			require.Empty(t, repo.receipts)
		})
	}
}
