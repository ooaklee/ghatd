package accessproof

import (
	"errors"
	"slices"
	"strings"
	"time"
)

var (
	// ErrInvalidPolicy rejects incomplete or malformed startup configuration.
	ErrInvalidPolicy = errors.New("accessproof: invalid policy")
	// ErrDenied hides which identity, binding, capability, or expiry failed.
	// Neither error includes caller-controlled evidence or credentials.
	ErrDenied = errors.New("accessproof: admission denied")
)

// Resource identifies one exact host-owned resource, not a wildcard or prefix.
// A zero Resource represents no resource binding; it never satisfies a bound
// requirement. Hosts must distinguish the resource namespace using Kind.
type Resource struct {
	// Kind separates otherwise identical IDs in different resource namespaces.
	Kind string
	// ID is the exact validated resource identifier, not a bearer credential.
	ID string
}

// Evidence describes one principal authenticated by a trusted resolver for the
// current request. Never decode this structure from client JSON or unsigned
// token claims. It grants no authority by itself and must not be cached between
// requests. The caller owns the slices and must not mutate them during a call.
type Evidence struct {
	// Kind identifies a host-defined proof category, such as member or guest.
	// It is not a persisted administrative subject kind or a user account role.
	Kind string
	// SubjectID is the resolved principal ID; raw credential presence is not proof.
	SubjectID string
	// Assurances contains exact trusted facts. There is no implicit ranking,
	// inheritance, or equivalence between differently named assurances.
	Assurances []string
	// Capabilities are exact permissions established by this one proof. The
	// evaluator never unions capabilities across identities or proofs.
	Capabilities []string
	// Binding is the resource established by the proof's trusted source, not
	// copied from request parameters to manufacture a matching authority.
	Binding Resource
	// ExpiresAt is the source proof's deadline, when exposed by the resolver.
	// Zero is allowed only if the matching requirement does not require expiry;
	// even then the resolver must have checked current validity on this request.
	ExpiresAt time.Time
}

// Requirement is one alternative whose constraints must all be satisfied by a
// single Evidence. Different alternatives are OR; facts within one are AND.
type Requirement struct {
	// Kind is the exact accepted proof category; empty and wildcard are invalid.
	Kind string
	// Assurances requires every named fact on the same evidence.
	Assurances []string
	// Capabilities requires every named permission on the same evidence.
	Capabilities []string
	// ResourceKind, when set, requires the evidence binding to equal the target
	// resource in this namespace. Empty does not impose a resource check; hosts
	// still enforce ownership or participant relationships in their domain.
	ResourceKind string
	// RequireExpiry rejects evidence that omits its source deadline. All nonzero
	// deadlines are checked, whether or not this option is set.
	RequireExpiry bool
}

// Policy is an immutable compiled set of alternatives safe for concurrent use.
// Its zero value denies all requests. Construct it with Compile at startup;
// public routes must explicitly bypass admission rather than use an empty rule.
type Policy struct {
	// alternatives is private and deeply copied from startup configuration.
	alternatives []Requirement
}

// Compile validates and copies a nonempty set of alternatives. Names are exact,
// bounded visible ASCII identifiers with no whitespace or wildcard semantics.
// It never normalizes a misspelled permission into a different permission.
func Compile(alternatives ...Requirement) (Policy, error) {
	if len(alternatives) == 0 || len(alternatives) > 32 {
		return Policy{}, ErrInvalidPolicy
	}
	result := Policy{alternatives: make([]Requirement, len(alternatives))}
	for i, requirement := range alternatives {
		if !validName(requirement.Kind) || !validNames(requirement.Assurances) || !validNames(requirement.Capabilities) ||
			(requirement.ResourceKind != "" && !validName(requirement.ResourceKind)) {
			return Policy{}, ErrInvalidPolicy
		}
		requirement.Assurances = slices.Clone(requirement.Assurances)
		requirement.Capabilities = slices.Clone(requirement.Capabilities)
		result.alternatives[i] = requirement
	}
	return result, nil
}

// Authorize admits one freshly authenticated proof if a complete alternative
// matches. A zero clock, invalid evidence, expiry at or before now, or partial
// match fails closed. The method neither resolves credentials nor consumes usage
// and returns no indication of which private constraint failed. Callers must
// still recheck revocation, resource authority and replay rights transactionally.
func (p Policy) Authorize(evidence Evidence, target Resource, now time.Time) error {
	if now.IsZero() || !validName(evidence.Kind) || !validID(evidence.SubjectID) ||
		!validNames(evidence.Assurances) || !validNames(evidence.Capabilities) ||
		(!evidence.ExpiresAt.IsZero() && !now.Before(evidence.ExpiresAt)) ||
		(evidence.Binding != (Resource{}) && (!validName(evidence.Binding.Kind) || !validID(evidence.Binding.ID))) {
		return ErrDenied
	}
	for _, requirement := range p.alternatives {
		if evidence.Kind != requirement.Kind || (requirement.RequireExpiry && evidence.ExpiresAt.IsZero()) ||
			!containsAll(evidence.Assurances, requirement.Assurances) || !containsAll(evidence.Capabilities, requirement.Capabilities) {
			continue
		}
		if requirement.ResourceKind != "" && (target.Kind != requirement.ResourceKind || !validID(target.ID) || evidence.Binding != target) {
			continue
		}
		return nil
	}
	return ErrDenied
}

// containsAll applies exact AND semantics without role hierarchy or wildcards.
func containsAll(actual, required []string) bool {
	for _, value := range required {
		if !slices.Contains(actual, value) {
			return false
		}
	}
	return true
}

// validNames bounds per-proof work and rejects empty, wildcard or duplicate facts.
func validNames(values []string) bool {
	if len(values) > 128 {
		return false
	}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if !validName(value) || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

// validName checks a bounded exact policy identifier rather than interpreting it.
func validName(value string) bool {
	return len(value) <= 128 && validID(value) && !strings.ContainsAny(value, "*?")
}

// validID accepts bounded opaque visible-ASCII identifiers without logging them.
func validID(value string) bool {
	if len(value) == 0 || len(value) > 512 {
		return false
	}
	for _, b := range []byte(value) {
		if b < 33 || b > 126 {
			return false
		}
	}
	return true
}
