package partneraccess

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"

	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/partnermanager"
)

// CohortPermission requires independently administered financial admission.
// It does not authorize publishing policy, changing grants or managing groups.
const CohortPermission = "partner.cohort.approved"

// cohortCandidateCapacity bounds host enforcement I/O before any grant read.
// Membership eligibility and the owning native reference probe remain in group.
const cohortCandidateCapacity = 10_000

// DirectGroups supplies complete current active direct memberships from the
// owning group service. Successful empty sets are nonnil and sorted/unique.
// It must not be implemented using the generic inherited access map.
type DirectGroups interface {
	// GetActiveDirectGroupIDs returns the complete current set of active direct
	// group memberships for the user from the owning group service.
	GetActiveDirectGroupIDs(context.Context, string) ([]string, error)
}

// CohortScope binds admission to an exact group identity without namespace or
// punctuation ambiguity. Pair it with CohortPermission on the selected customer's
// current grant through the existing privileged access-policy management API.
func CohortScope(groupID string) (string, error) {
	if !validID(groupID) {
		return "", partnermanager.ErrInvalid
	}
	digest := sha256.Sum256([]byte(groupID))
	return "partners.cohort." + hex.EncodeToString(digest[:]), nil
}

// Cohorts intersects owning membership with privileged customer admission.
// Ordinary group creation/joining confers no rate-bearing cohort approval.
// This trusted in-process boundary owns no grants, groups or financial state;
// the manager must independently verify the selected paying customer's identity.
type Cohorts struct {
	system string
	groups DirectGroups
	policy PolicyService
}

// NewCohorts validates the system identifier and borrowed ports, returning
// ErrUnavailable rather than constructing around missing dependencies.
func NewCohorts(system string, groups DirectGroups, policy PolicyService) (*Cohorts, error) {
	if !validID(system) || nilPort(groups) || nilPort(policy) {
		return nil, partnermanager.ErrUnavailable
	}
	return &Cohorts{system: system, groups: groups, policy: policy}, nil
}

// PartnerGroupIDs returns only approved current memberships. Complete evidence
// is validated before grant I/O; joined/alias denials and late failures discard
// everything. Membership is reread and the selected scopes are confirmed together
// in one owning grant read, preventing mixed per-group grant revisions from
// approving an incompatible set. This is not an atomic cross-domain snapshot or
// historical fact. Membership churn/final grant denial returns unavailable for
// caller backoff and a fresh complete resolve, never partial stale approval.
// There are at most capacity+1 policy reads and two owning membership reads.
func (c *Cohorts) PartnerGroupIDs(ctx context.Context, customerID string) ([]string, error) {
	if ctx == nil || !validID(customerID) {
		return nil, partnermanager.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c == nil || nilPort(c.groups) || nilPort(c.policy) {
		return nil, partnermanager.ErrUnavailable
	}
	ids, err := c.memberships(ctx, customerID)
	if err != nil {
		return nil, err
	}
	subject := accesspolicy.Subject{System: c.system, Kind: accesspolicy.UserSubject, ID: customerID}
	approved, scopes := make([]string, 0, len(ids)), make([]string, 0, len(ids))
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		scope, _ := CohortScope(id) // Complete membership validation precedes I/O.
		err := c.policy.Authorize(ctx, subject, []string{scope}, []string{CohortPermission})
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if soleDenial(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %w", partnermanager.ErrUnavailable, err)
		}
		approved, scopes = append(approved, id), append(scopes, scope)
	}
	current, err := c.memberships(ctx, customerID)
	if err != nil {
		return nil, err
	}
	if !slices.Equal(ids, current) {
		return nil, partnermanager.ErrUnavailable
	}
	if len(approved) != 0 {
		if err := c.policy.Authorize(ctx, subject, scopes, []string{CohortPermission}); err != nil {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("%w: %w", partnermanager.ErrUnavailable, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return approved, nil
}

// memberships reads current direct group IDs, enforcing a non-nil sorted unique
// result within the candidate capacity and returning ErrUnavailable otherwise;
// context errors take precedence and other failures become unavailable. The
// slice is cloned so a source reusing its backing storage cannot mutate an
// earlier read.
func (c *Cohorts) memberships(ctx context.Context, customerID string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ids, err := c.groups.GetActiveDirectGroupIDs(ctx, customerID)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %w", partnermanager.ErrUnavailable, err)
	}
	if ids == nil || len(ids) > cohortCandidateCapacity {
		return nil, partnermanager.ErrUnavailable
	}
	for i, id := range ids {
		if !validID(id) || (i > 0 && ids[i-1] >= id) {
			return nil, partnermanager.ErrUnavailable
		}
	}
	// Isolate the first read from a source reusing its backing slice on reread.
	return slices.Clone(ids), nil
}

var _ partnermanager.Groups = (*Cohorts)(nil)
