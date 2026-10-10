package referral

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type evidenceRepo struct {
	Repository
	rows  []RelationshipEvidenceRow
	err   error
	calls int
}

func (r *evidenceRepo) ReadRelationshipEvidence(context.Context, string, string) ([]RelationshipEvidenceRow, error) {
	r.calls++
	return r.rows, r.err
}

// Audit disposition: named isolated provenance, privacy, capacity and current
// ownership cases; no fake global set is assembled from relationship pages.
func TestCompleteRelationshipEvidenceBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, mode string
		want       error
	}{
		{"former_owner_keeps_frozen_original_binding", "", nil},
		{"empty_complete_set", "empty", nil},
		{"nil_is_not_complete_empty", "nil", ErrUnavailable},
		{"duplicate_membership", "duplicate", ErrUnavailable},
		{"foreign_membership", "foreign", ErrUnavailable},
		{"wrong_first_owned_reference", "first", ErrUnavailable},
		{"history_gap", "history", ErrUnavailable},
		{"binding_owner_does_not_match_history", "owner", ErrUnavailable},
		{"binding_terms_differ", "terms", ErrUnavailable},
		{"binding_precedes_frozen_owner", "before", ErrUnavailable},
		{"duplicate_economic_binding", "binding_duplicate", ErrUnavailable},
		{"duplicate_opaque_binding_identity", "binding_id_duplicate", ErrUnavailable},
		{"future_effective_binding_remains_frozen", "future_effective", nil},
		{"future_recorded_binding_cannot_be_certified", "future_bound", ErrUnavailable},
		{"independent_combined_evidence_capacity", "capacity", ErrCapacity},
		{"joined_absence_and_outage_discards_all", "outage", ErrUnavailable},
		{"invalid_partner_before_repository", "partner", ErrInvalid},
		{"canceled_before_repository", "cancel", context.Canceled},
		{"missing_same_repository_capability", "legacy", ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := relationshipEvidence()
			binding := PaymentAttribution{ID: "binding", ProgramID: ProgramID, PaymentID: "payment", ReferredCustomer: row.ReferredCustomer, ReferralID: row.History[0].ID, PartnerID: "partner", EffectiveAt: row.History[0].LockedAt.Add(time.Minute), BoundAt: row.Head.LockedAt, Terms: fixtureTerms()}
			r := &evidenceRepo{rows: []RelationshipEvidenceRow{{row, []PaymentAttribution{binding}}}}
			at := row.Head.LockedAt.Add(time.Hour)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			partner := "partner"
			switch tc.mode {
			case "empty":
				r.rows = []RelationshipEvidenceRow{}
			case "nil":
				r.rows = nil
			case "duplicate":
				r.rows = append(r.rows, r.rows[0])
			case "foreign":
				r.rows[0].Relationship.PartnerID = "other"
			case "first":
				r.rows[0].Relationship.FirstReferralID = row.Head.ID
			case "history":
				r.rows[0].Relationship.History = row.History[1:]
			case "owner":
				r.rows[0].Bindings[0].PartnerID = "other"
			case "terms":
				r.rows[0].Bindings[0].Terms.RateBasisPoints++
			case "before":
				r.rows[0].Bindings[0].EffectiveAt = row.History[0].LockedAt.Add(-time.Nanosecond)
			case "binding_duplicate":
				r.rows[0].Bindings = append(r.rows[0].Bindings, binding)
			case "binding_id_duplicate":
				second := binding
				second.PaymentID = "different-economic-payment"
				r.rows[0].Bindings = append(r.rows[0].Bindings, second)
			case "future_effective":
				r.rows[0].Bindings[0].EffectiveAt = at.Add(time.Hour)
			case "future_bound":
				r.rows[0].Bindings[0].BoundAt = at.Add(time.Nanosecond)
			case "capacity":
				r.rows[0].Bindings = make([]PaymentAttribution, RelationshipEvidenceCapacity)
			case "outage":
				r.err = errors.Join(ErrNotFound, ErrUnavailable)
			case "partner":
				partner = " partner"
			case "cancel":
				cancel()
			}
			s, err := NewService(r, fakeClock{at}, &seqIDs{}, time.Hour)
			require.NoError(t, err)
			if tc.mode == "legacy" {
				s.repo = newFakeRepo()
			}
			out, err := s.GetRelationshipEvidence(ctx, partner)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Zero(t, out)
				if tc.want == ErrInvalid || tc.want == context.Canceled {
					require.Zero(t, r.calls)
				}
				return
			}
			require.NotNil(t, out.Items)
			require.NotEmpty(t, out.Revision)
			require.Equal(t, at, out.AsOf)
			if tc.mode != "empty" {
				require.Len(t, out.Items, 1)
				require.False(t, out.Items[0].Relationship.Current)
				require.Len(t, out.Items[0].Relationship.Periods, 1)
				require.Len(t, out.Items[0].Attribution.Bindings, 1)
			}
			bytes, err := json.Marshal(out)
			require.NoError(t, err)
			require.JSONEq(t, "{}", string(bytes), "private owning evidence cannot serialize into customer transport")
			confirm, err := s.GetRelationshipEvidence(ctx, partner)
			require.NoError(t, err)
			require.Equal(t, out.Revision, confirm.Revision)
		})
	}
}
