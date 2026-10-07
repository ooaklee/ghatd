package group

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Only the bounded owning source is faked here. The actual service performs
// membership projection; native query/extra-row behavior has separate tests.
type directMembershipSource struct {
	GroupRepository
	rows   []UniversalGroup
	err    error
	cancel context.CancelFunc
	calls  int
}

func (s *directMembershipSource) GetGroupsByReferencedUserIDBounded(_ context.Context, id string, limit int) ([]UniversalGroup, error) {
	s.calls++
	if id != "user-a" || limit != DirectMembershipCapacity {
		return nil, ErrInvalidQueryParam
	}
	if s.cancel != nil {
		s.cancel()
	}
	return s.rows, s.err
}

func TestActiveDirectMembershipProjection(t *testing.T) {
	type testCase struct {
		name, state string
		want        []string
		wantErr     error
	}
	for _, tc := range []testCase{
		{name: "accepted_direct_user", want: []string{"group-a"}},
		{name: "direct_owner", state: "owner", want: []string{"group-a"}},
		{name: "pending_invitation_is_not_membership", state: "invited", want: []string{}},
		{name: "unknown_invitation_state_is_not_accepted", state: "unknown-invitation", want: []string{}},
		{name: "whitespace_invitation_state_is_not_accepted", state: "whitespace-invitation", want: []string{}},
		{name: "legacy_invitation_time_is_not_membership", state: "invited-at", want: []string{}},
		{name: "group_typed_reference_is_not_user", state: "group-reference", want: []string{}},
		{name: "inherited_descendant_admin_is_not_direct", state: "descendant", want: []string{}},
		{name: "inactive_group", state: "inactive", want: []string{}},
		{name: "archived_group", state: "archived", want: []string{}},
		{name: "suspended_group", state: "suspended", want: []string{}},
		{name: "provisioned_group", state: "provisioned", want: []string{}},
		{name: "deleted_group", state: "deleted", want: []string{}},
		{name: "unrelated_current_member", state: "other-user", want: []string{}},
		{name: "empty_owning_reference_set", state: "empty", want: []string{}},
		{name: "nil_source_is_not_empty", state: "nil-rows", wantErr: ErrDirectMembershipUnavailable},
		{name: "malformed_group_identity", state: "malformed-id", wantErr: ErrDirectMembershipUnavailable},
		{name: "duplicate_group_identity", state: "duplicate", wantErr: ErrDirectMembershipUnavailable},
		{name: "contradictory_duplicate_member_reference", state: "duplicate-member", wantErr: ErrDirectMembershipUnavailable},
		{name: "owning_outage_discards_earlier_rows", state: "outage", wantErr: ErrDirectMembershipUnavailable},
		{name: "joined_absence_and_outage_is_not_empty", state: "joined", wantErr: ErrDirectMembershipUnavailable},
		{name: "cancelled_after_read_discards_all", state: "late-cancel", wantErr: context.Canceled},
		{name: "stable_sorted_output", state: "sorted", want: []string{"group-a", "group-z"}},
		{name: "exact_capacity_is_complete", state: "exact-capacity", want: []string{}},
		{name: "extra_inactive_reference_is_not_truncated", state: "over-capacity", wantErr: ErrDirectMembershipCapacity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outage := errors.New("membership-read-outage")
			source := &directMembershipSource{rows: []UniversalGroup{{ID: "group-a", Status: GroupStatusActive, Members: []Member{{ID: "user-a", Type: MemberTypeUser, Role: MemberRoleMember}}}}}
			ctx := t.Context()
			switch tc.state {
			case "owner":
				source.rows[0].OwnerID, source.rows[0].Members = "user-a", nil
			case "invited":
				source.rows[0].Members[0].InvitationState = MemberInvitationStateInvited
			case "unknown-invitation":
				source.rows[0].Members[0].InvitationState = "UNKNOWN"
			case "whitespace-invitation":
				source.rows[0].Members[0].InvitationState = " \t"
			case "invited-at":
				source.rows[0].Members[0].InvitedAt = "2026-10-07T12:00:00Z"
			case "group-reference":
				source.rows[0].Members[0].Type = MemberTypeGroup
			case "descendant":
				source.rows[0].Members = nil
				source.rows[0].Lineage = []string{"administered-ancestor"}
			case "inactive":
				source.rows[0].Status = GroupStatusInactive
			case "archived":
				source.rows[0].Status = GroupStatusArchived
			case "suspended":
				source.rows[0].Status = GroupStatusSuspended
			case "provisioned":
				source.rows[0].Status = GroupStatusProvisioned
			case "deleted":
				source.rows[0].Metadata = &GroupMetadata{DeletedAt: "2026-10-07T12:00:00Z"}
			case "other-user":
				source.rows[0].Members[0].ID = "user-b"
			case "empty":
				source.rows = []UniversalGroup{}
			case "nil-rows":
				source.rows = nil
			case "malformed-id":
				source.rows[0].ID = "malformed id"
			case "duplicate":
				source.rows = append(source.rows, source.rows[0])
			case "duplicate-member":
				source.rows[0].Members = append(source.rows[0].Members, Member{ID: "user-a", Type: MemberTypeUser, InvitationState: MemberInvitationStateInvited})
			case "outage":
				source.err = outage
			case "joined":
				source.err = errors.Join(ErrResourceNotFound, outage)
			case "late-cancel":
				ctx, source.cancel = context.WithCancel(ctx)
				t.Cleanup(source.cancel)
			case "sorted":
				row := source.rows[0]
				row.ID = "group-z"
				source.rows = append([]UniversalGroup{row}, source.rows...)
			case "exact-capacity", "over-capacity":
				count := DirectMembershipCapacity
				if tc.state == "over-capacity" {
					count++
				}
				source.rows = make([]UniversalGroup, count)
				for i := range source.rows {
					source.rows[i] = UniversalGroup{ID: fmt.Sprintf("inactive-%05d", i), Status: GroupStatusInactive}
				}
			}
			service := &Service{GroupRepository: source}
			ids, err := service.GetActiveDirectGroupIDs(ctx, "user-a")
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				require.Nil(t, ids, "no partial evidence on any failure")
				if tc.state == "outage" || tc.state == "joined" {
					require.ErrorIs(t, err, outage, "retain the underlying operational cause")
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, ids)
			}
			require.Equal(t, 1, source.calls)
		})
	}
}

func TestActiveDirectMembershipAdmission(t *testing.T) {
	type testCase struct{ name, state string }
	for _, tc := range []testCase{
		{name: "nil_context", state: "nil-context"},
		{name: "cancelled_context", state: "cancelled"},
		{name: "empty_user", state: "empty"},
		{name: "user_with_whitespace", state: "whitespace"},
		{name: "user_with_control", state: "control"},
		{name: "invalid_utf8_user", state: "utf8"},
		{name: "oversized_user", state: "oversized"},
		{name: "nil_service", state: "nil-service"},
		{name: "zero_value_service", state: "zero-service"},
		{name: "typed_nil_source", state: "nil-source"},
		{name: "missing_optional_source_capability", state: "missing-capability"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := &directMembershipSource{}
			service := &Service{GroupRepository: source}
			ctx, id, want := t.Context(), "user-a", ErrDirectMembershipUnavailable
			switch tc.state {
			case "nil-context":
				ctx = nil
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				want = context.Canceled
			case "empty", "whitespace", "control", "utf8", "oversized":
				id = map[string]string{"empty": "", "whitespace": "user a", "control": "user\x00a", "utf8": "user\xff", "oversized": strings.Repeat("u", 257)}[tc.state]
				want = ErrInvalidUserIDProvided
			case "nil-service":
				service = nil
			case "zero-service":
				service = &Service{}
			case "nil-source":
				var missing *directMembershipSource
				service.GroupRepository = missing
			case "missing-capability":
				// The original optional capability remains absent: no method
				// promoted from a nil embedded DirectMembershipRepository.
				service.GroupRepository = &struct{ GroupRepository }{}
			}
			ids, err := service.GetActiveDirectGroupIDs(ctx, id)
			require.ErrorIs(t, err, want)
			require.Nil(t, ids)
			require.Zero(t, source.calls)
		})
	}
}
