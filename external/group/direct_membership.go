package group

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	// ErrDirectMembershipUnavailable means current complete membership could
	// not be established. It must not become an empty group set or permission.
	ErrDirectMembershipUnavailable = errors.New("group/direct-membership-unavailable")
	// ErrDirectMembershipCapacity refuses a truncated current reference set.
	ErrDirectMembershipCapacity = errors.New("group/direct-membership-capacity")
)

// DirectMembershipCapacity bounds referenced groups before active-membership
// filtering. An extra reference cannot be silently dropped even if ineligible.
const DirectMembershipCapacity = 10000

// DirectMembershipRepository is an optional bounded source capability. The
// result includes an extra-row probe (at most limit+1), not a truncated page.
// It supplies current reference rows, not business eligibility or permissions.
// Empty successful results are nonnil; any late read failure discards all rows.
type DirectMembershipRepository interface {
	// GetGroupsByReferencedUserIDBounded returns current group rows referencing the
	// userID, probing at most limit+1 entries so the service can detect more
	// results, not a truncated page. The Repository implementation validates the
	// limit against DirectMembershipCapacity and returns nonnil empty slices on
	// success.
	GetGroupsByReferencedUserIDBounded(context.Context, string, int) ([]UniversalGroup, error)
}

// GetActiveDirectGroupIDs reads current active, nondeleted groups where the
// selected user is owner or an accepted direct USER member. Pending invitations,
// group-typed references and inherited descendant administration do not qualify.
// It registers no route and grants no authority, enrollment eligibility or rate
// override: hosts must independently require their privileged cohort approval.
// The single owning query is current evidence, not a historical membership fact
// or an atomic snapshot with another domain's permission/policy read.
func (s *Service) GetActiveDirectGroupIDs(ctx context.Context, userID string) ([]string, error) {
	if ctx == nil {
		return nil, ErrDirectMembershipUnavailable
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !directMembershipID(userID) {
		return nil, ErrInvalidUserIDProvided
	}
	if s == nil || directMembershipNil(s.GroupRepository) {
		return nil, ErrDirectMembershipUnavailable
	}
	repo, ok := s.GroupRepository.(DirectMembershipRepository)
	if !ok || directMembershipNil(repo) {
		return nil, ErrDirectMembershipUnavailable
	}
	groups, err := repo.GetGroupsByReferencedUserIDBounded(ctx, userID, DirectMembershipCapacity)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrDirectMembershipUnavailable, err)
	}
	if groups == nil {
		return nil, ErrDirectMembershipUnavailable
	}
	if len(groups) > DirectMembershipCapacity {
		return nil, ErrDirectMembershipCapacity
	}
	ids := make([]string, 0, len(groups))
	seen := make(map[string]bool, len(groups))
	for _, row := range groups {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !directMembershipID(row.ID) || seen[row.ID] {
			return nil, ErrDirectMembershipUnavailable
		}
		seen[row.ID] = true
		if row.Status != GroupStatusActive || (row.Metadata != nil && row.Metadata.DeletedAt != "") {
			continue
		}
		member := row.OwnerID == userID
		matchingReferences := 0
		for _, direct := range row.Members {
			if direct.ID == userID {
				matchingReferences++
				if matchingReferences > 1 {
					return nil, ErrDirectMembershipUnavailable
				}
			}
			if direct.ID == userID && direct.Type == MemberTypeUser && direct.InvitationState == "" && !isPendingInviteMember(direct) {
				member = true
			}
		}
		if member {
			ids = append(ids, row.ID)
		}
	}
	sort.Strings(ids)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

// directMembershipID reports whether a membership identifier is non-empty, at
// most 256 bytes, valid UTF-8 and free of whitespace or control characters.
func directMembershipID(value string) bool {
	return value != "" && len(value) <= 256 && utf8.ValidString(value) && strings.IndexFunc(value, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) < 0
}

// directMembershipNil reports whether an interface value is nil or wraps a nil
// channel, func, interface, map, pointer or slice.
func directMembershipNil(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}
