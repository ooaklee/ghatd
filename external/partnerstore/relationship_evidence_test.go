package partnerstore

import (
	"context"
	"testing"

	"github.com/ooaklee/ghatd/external/referral"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

type completeReadBoundaryStore struct {
	recordstore.Store
	calls int
	mode  string
}

func (s *completeReadBoundaryStore) Read(ctx context.Context, fn func(recordstore.Tx) error) error {
	s.calls++
	if s.mode == "nil_tx" {
		return fn(nil)
	}
	if s.mode == "canceled_read" {
		return context.Canceled
	}
	return nil // Deliberately violates Store's executed-callback contract.
}

// Audit disposition: named invalid context, absent dependencies and malformed
// callback contracts prove availability cannot be mistaken for a complete zero.
func TestCompleteEvidenceReadExecutionBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		want       error
	}{
		{"callback_never_executed", "no_callback", referral.ErrUnavailable},
		{"typed_transaction_absent", "nil_tx", referral.ErrUnavailable},
		{"cancel_during_native_read", "canceled_read", context.Canceled},
		{"nil_context_before_read", "nil_context", referral.ErrInvalid},
		{"canceled_context_before_read", "canceled", context.Canceled},
		{"nil_repository", "nil_repo", referral.ErrUnavailable},
		{"wrong_program_before_read", "program", referral.ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &completeReadBoundaryStore{mode: tc.mode}
			repo, err := NewReferralRepository(store)
			require.NoError(t, err)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			program := referral.ProgramID
			switch tc.mode {
			case "nil_context":
				ctx = nil
			case "canceled":
				cancel()
			case "nil_repo":
				repo = nil
			case "program":
				program = "other-program"
			}
			out, err := repo.ReadRelationshipEvidence(ctx, program, "partner")
			require.ErrorIs(t, err, tc.want)
			require.Nil(t, out)
			if tc.mode == "nil_context" || tc.mode == "canceled" || tc.mode == "program" || tc.mode == "nil_repo" {
				require.Zero(t, store.calls)
			}
		})
	}
}
