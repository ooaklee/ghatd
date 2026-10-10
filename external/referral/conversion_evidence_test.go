package referral

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type conversionRepo struct {
	Repository
	snapshot ConversionSnapshot
	err      error
	calls    int
}

func (r *conversionRepo) ReadConversionSnapshot(context.Context, string, string, AnalyticsQuery) (ConversionSnapshot, error) {
	r.calls++
	return r.snapshot, r.err
}

func conversionFixture() (ConversionSnapshot, time.Time) {
	traffic, at := analyticsFixture()
	snapshot := ConversionSnapshot{Analytics: traffic, Bindings: map[string][]PaymentAttribution{}}
	for _, member := range traffic.Relationships {
		first := member.History[0]
		snapshot.Bindings[member.ReferredCustomer] = []PaymentAttribution{{ID: "binding-" + first.ID, ProgramID: ProgramID, PaymentID: "payment-" + first.ID, ReferredCustomer: member.ReferredCustomer, ReferralID: first.ID, PartnerID: first.PartnerID, EffectiveAt: first.LockedAt.Add(time.Hour), BoundAt: at.Add(2 * time.Hour), Terms: first.TermsSnapshot}}
	}
	return snapshot, at
}

// Audit disposition: named complete evidence, independent revisions, persisted
// traffic golden and failures use fresh mutable snapshots per case.
func TestConversionEvidenceOwnershipAndRevision(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		want       error
	}{
		{"complete_binding_and_traffic_share_one_read", "", nil},
		{"raw_cleanup_keeps_canonical_revision", "cleanup", nil},
		{"binding_changes_only_attribution_and_combined_revision", "binding", nil},
		{"day_changes_only_traffic_and_combined_revision", "day", nil},
		{"missing_binding_partition_is_not_empty", "missing", ErrUnavailable},
		{"nil_binding_partition_is_not_empty", "nil", ErrUnavailable},
		{"unrelated_binding_partition", "foreign", ErrUnavailable},
		{"combined_link_ownership_binding_capacity", "capacity", ErrCapacity},
		{"partial_day_raw_loss_is_not_zero", "granularity", ErrGranularity},
		{"future_recorded_binding_is_unavailable", "future", ErrUnavailable},
		{"missing_same_repository_capability", "legacy", ErrUnavailable},
		{"invalid_query_before_read", "query", ErrInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, at := conversionFixture()
			r := &conversionRepo{snapshot: fixture}
			s, err := NewService(r, fakeClock{at.Add(3 * time.Hour)}, &seqIDs{}, time.Hour)
			require.NoError(t, err)
			q := AnalyticsQuery{Limit: 1}
			baseline, err := s.GetConversionEvidence(context.Background(), "partner", q)
			require.NoError(t, err)
			require.Equal(t, "4ba3332581951665e571d2fbd5d2cb5fc8883fb7bdc55fa64461a9411b9aad29", baseline.Analytics.Revision, "literal captured from pre-extraction owning analytics")
			customer := fixture.Analytics.Relationships[0].ReferredCustomer
			switch tc.mode {
			case "cleanup":
				r.snapshot.Analytics.Clicks = nil
			case "binding":
				second := r.snapshot.Bindings[customer][0]
				second.ID, second.PaymentID = "new-binding", "new-payment"
				r.snapshot.Bindings[customer] = append(r.snapshot.Bindings[customer], second)
			case "day":
				r.snapshot.Analytics.Days[0].Revision++
				r.snapshot.Analytics.Days[0].Counts.Observations++
				r.snapshot.Analytics.Days[0].Counts.UnmeasuredObservations++
			case "missing":
				delete(r.snapshot.Bindings, customer)
			case "nil":
				r.snapshot.Bindings[customer] = nil
			case "foreign":
				r.snapshot.Bindings["other-customer"] = []PaymentAttribution{}
			case "capacity":
				r.snapshot.Bindings[customer] = make([]PaymentAttribution, RelationshipEvidenceCapacity)
			case "granularity":
				to := at.Add(time.Minute)
				q.To = &to
				r.snapshot.Analytics.Clicks = nil
			case "future":
				r.snapshot.Bindings[customer][0].BoundAt = at.Add(4 * time.Hour)
			case "legacy":
				s.repo = newFakeRepo()
			case "query":
				q.Limit = 101
			}
			out, err := s.GetConversionEvidence(context.Background(), "partner", q)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Zero(t, out)
				return
			}
			require.Len(t, out.Relationships.Items, 4)
			require.Len(t, out.Links, 2, "private link set is complete before public pagination")
			require.Len(t, out.Analytics.Links, 1)
			bytes, err := json.Marshal(out)
			require.NoError(t, err)
			require.JSONEq(t, "{}", string(bytes))
			if tc.mode == "binding" {
				require.Equal(t, baseline.Analytics.Revision, out.Analytics.Revision)
				require.NotEqual(t, baseline.Relationships.Revision, out.Relationships.Revision)
				require.NotEqual(t, baseline.Revision, out.Revision)
			} else if tc.mode == "day" {
				require.NotEqual(t, baseline.Analytics.Revision, out.Analytics.Revision)
				require.Equal(t, baseline.Relationships.Revision, out.Relationships.Revision)
				require.NotEqual(t, baseline.Revision, out.Revision)
			} else {
				require.Equal(t, baseline.Revision, out.Revision)
			}
		})
	}
}
