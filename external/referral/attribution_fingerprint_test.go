package referral

import (
	"context"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func persistedEvidenceFixture(mode string) (string, Referral, []Referral, []PaymentAttribution) {
	customer := "private-customer"
	if mode == "empty" {
		return customer, Referral{}, nil, nil
	}
	row := relationshipEvidence()
	if mode == "correction" {
		row.History[1].Correction = &CorrectionReceipt{RequestFingerprint: "original-request", PreviewFingerprint: "reviewed-preview", SnapshotFingerprint: "original-snapshot", PartnerCustomer: "private-owner", SignupID: "signup", SignupCreatedAt: row.History[0].LockedAt, ExpectedRevision: 1, ExpectedReferralID: row.History[0].ID, Mode: CorrectionProspective}
		row.Head = row.History[1]
	}
	bindings := []PaymentAttribution{{ID: "binding-original", ProgramID: ProgramID, ReferredCustomer: customer, ReferralID: row.History[0].ID, PartnerID: row.History[0].PartnerID, PaymentID: "payment-original", EffectiveAt: row.History[0].LockedAt.Add(time.Minute), BoundAt: row.Head.LockedAt, Terms: row.History[0].TermsSnapshot}}
	return customer, row.Head, row.History, bindings
}

// Audit disposition: literal persisted-contract goldens were executed against
// the pre-extraction implementation. Nil/empty normalization and private
// correction receipts must keep issued correction fingerprints byte-stable.
func TestPersistedAttributionFingerprints(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"empty", "d15ae1ecfe77feef1087b092bb0696658f5ec06e759910dd0837dc711f176d96"},
		{"history", "aa58f1d020c71a167a266c3562bd817a9974b891075e77f7fdba9591ae7c210b"},
		{"correction", "edcadff9bda916852bd66b8fbd160bac49ceb49e88414852e8f81a9d4c602e17"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			customer, head, history, bindings := persistedEvidenceFixture(tc.name)
			out, err := validatedAttributionSnapshot(customer, head, history, bindings)
			require.NoError(t, err)
			require.Equal(t, tc.want, out.Fingerprint)
			r := newFakeRepo()
			r.history[ProgramID+":"+customer] = history
			for _, b := range bindings {
				r.bindings[ProgramID+":"+b.PaymentID] = b
			}
			s, err := NewService(r, fakeClock{time.Date(2026, 10, 7, 15, 0, 0, 0, time.UTC)}, &seqIDs{}, time.Hour)
			require.NoError(t, err)
			got, err := s.GetAttributionSnapshot(context.Background(), customer)
			require.NoError(t, err)
			require.Equal(t, out, got)
			if tc.name == "empty" {
				again, err := validatedAttributionSnapshot(customer, head, []Referral{}, []PaymentAttribution{})
				require.NoError(t, err)
				require.Equal(t, tc.want, again.Fingerprint)
			}
		})
	}
}
