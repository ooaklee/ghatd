package referral

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type relationshipRepo struct {
	Repository
	rows  []RelationshipSnapshot
	more  bool
	err   error
	calls int
}

func (r *relationshipRepo) ListRelationshipSnapshots(context.Context, string, string, int, string) ([]RelationshipSnapshot, bool, error) {
	r.calls++
	return r.rows, r.more, r.err
}

func relationshipEvidence() RelationshipSnapshot {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	first := Referral{ID: "original", ProgramID: ProgramID, PartnerID: "partner", ReferredCustomer: "private-customer", Revision: 1, LockedAt: at, PostedAt: at, TermsSnapshot: fixtureTerms()}
	second := first
	second.ID, second.PartnerID, second.Revision = "other-revision", "other-partner", 2
	second.CorrectionOf, second.PriorPartnerID = first.ID, first.PartnerID
	second.LockedAt, second.PostedAt = at.Add(time.Hour), at.Add(time.Hour)
	return RelationshipSnapshot{ID: RelationshipReferenceID(ProgramID, "partner", first.ReferredCustomer), ProgramID: ProgramID, PartnerID: "partner", ReferredCustomer: first.ReferredCustomer, FirstReferralID: first.ID, Head: second, History: []Referral{first, second}}
}

// Audit disposition: named owning-chain, lifetime-membership, pagination and
// failure boundaries. Raw repository evidence never becomes partial output.
func TestRelationshipReadBoundaries(t *testing.T) {
	cases := []struct {
		name, mode string
		want       error
	}{
		{name: "former_owner_retains_one_owned_period", mode: "former"},
		{name: "reacquired_owner_keeps_first_membership", mode: "reacquired"},
		{name: "next_page_uses_lifetime_reference", mode: "more"},
		{name: "missing_middle_revision", mode: "gap", want: ErrUnavailable},
		{name: "head_does_not_match_history", mode: "head", want: ErrUnavailable},
		{name: "first_reference_not_first_owned", mode: "reference", want: ErrUnavailable},
		{name: "relationship_identity_mismatch", mode: "identity", want: ErrUnavailable},
		{name: "wrong_partner_scope", mode: "scope", want: ErrUnavailable},
		{name: "duplicate_membership", mode: "duplicate", want: ErrUnavailable},
		{name: "cursor_does_not_advance", mode: "cursor", want: ErrUnavailable},
		{name: "invalid_first_chain_link", mode: "first", want: ErrUnavailable},
		{name: "joined_absence_and_outage_discards_rows", mode: "outage", want: ErrUnavailable},
		{name: "more_with_short_page", mode: "short", want: ErrUnavailable},
		{name: "invalid_limit", mode: "limit", want: ErrInvalid},
		{name: "invalid_cursor", mode: "badcursor", want: ErrInvalid},
		{name: "cancelled_context", mode: "cancelled", want: context.Canceled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := relationshipEvidence()
			r := &relationshipRepo{rows: []RelationshipSnapshot{row}}
			q := RelationshipQuery{Limit: 1}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch tc.mode {
			case "reacquired":
				third := row.History[0]
				third.ID, third.Revision = "reacquired", 3
				third.CorrectionOf, third.PriorPartnerID = row.Head.ID, row.Head.PartnerID
				third.LockedAt, third.PostedAt = row.Head.LockedAt.Add(time.Hour), row.Head.PostedAt.Add(time.Hour)
				r.rows[0].Head = third
				r.rows[0].History = append(row.History, third)
			case "more":
				r.more = true
			case "gap":
				r.rows[0].History = row.History[1:]
			case "head":
				r.rows[0].Head = row.History[0]
			case "reference":
				r.rows[0].FirstReferralID = row.Head.ID
			case "identity":
				r.rows[0].ID = RelationshipReferenceID(ProgramID, "other", row.ReferredCustomer)
			case "scope":
				r.rows[0].PartnerID = "other"
			case "duplicate":
				q.Limit = 2
				r.rows = append(r.rows, row)
			case "cursor":
				q.After = row.ID
			case "first":
				r.rows[0].History[0].CorrectionOf = "unknown"
			case "outage":
				r.err = errors.Join(ErrNotFound, ErrUnavailable)
			case "short":
				r.more, q.Limit = true, 2
			case "limit":
				q.Limit = 101
			case "badcursor":
				q.After = "customer-id"
			case "cancelled":
				cancel()
			}
			s, err := NewService(r, fakeClock{row.Head.LockedAt}, &seqIDs{}, time.Hour)
			require.NoError(t, err)
			out, err := s.ListRelationships(ctx, "partner", q)
			require.ErrorIs(t, err, tc.want)
			if tc.want != nil {
				require.Empty(t, out)
				if tc.want == ErrInvalid || tc.want == context.Canceled {
					require.Zero(t, r.calls)
				}
				return
			}
			require.Len(t, out.Items, 1)
			item := out.Items[0]
			require.Equal(t, row.ID, item.ID)
			require.Equal(t, row.History[0].LockedAt, item.FirstOwnedAt)
			require.Equal(t, tc.mode == "reacquired", item.Current)
			require.Equal(t, row.Head.LockedAt, *item.Periods[0].Until)
			if tc.mode == "reacquired" {
				require.Len(t, item.Periods, 2)
				require.Nil(t, item.Periods[1].Until)
				require.True(t, item.Periods[1].Corrected)
			} else {
				require.Len(t, item.Periods, 1)
			}
			require.Equal(t, r.more, out.HasMore)
			if r.more {
				require.Equal(t, row.ID, out.NextAfter)
			} else {
				require.Empty(t, out.NextAfter)
			}
		})
	}
}
