package accessproof

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestCompile validates startup configuration without creating anonymous rules.
func TestCompile(t *testing.T) {
	for _, tc := range []struct {
		// name identifies the configuration boundary independently of its contents.
		name string
		// rules and invalid define the submitted alternatives and expected result.
		rules   []Requirement
		invalid bool
	}{
		{"one category", []Requirement{{Kind: "member"}}, false},
		{"alternative categories", []Requirement{{Kind: "member"}, {Kind: "guest", Capabilities: []string{"read"}, ResourceKind: "document", RequireExpiry: true}}, false},
		{"no implicit anonymous", nil, true},
		{"empty category", []Requirement{{}}, true},
		{"wildcard category", []Requirement{{Kind: "*"}}, true},
		{"whitespace category", []Requirement{{Kind: "member "}}, true},
		{"oversize category", []Requirement{{Kind: strings.Repeat("a", 129)}}, true},
		{"too many branches", make([]Requirement, 33), true},
		{"empty assurance", []Requirement{{Kind: "member", Assurances: []string{""}}}, true},
		{"duplicate assurance", []Requirement{{Kind: "member", Assurances: []string{"verified", "verified"}}}, true},
		{"wildcard scope", []Requirement{{Kind: "guest", Capabilities: []string{"read.*"}}}, true},
		{"duplicate scope", []Requirement{{Kind: "guest", Capabilities: []string{"read", "read"}}}, true},
		{"empty scope", []Requirement{{Kind: "guest", Capabilities: []string{""}}}, true},
		{"too many scopes", []Requirement{{Kind: "guest", Capabilities: make([]string, 129)}}, true},
		{"control byte", []Requirement{{Kind: "guest", Capabilities: []string{"read\n"}}}, true},
		{"invalid resource kind", []Requirement{{Kind: "guest", ResourceKind: "?"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy, err := Compile(tc.rules...)
			if tc.invalid {
				require.ErrorIs(t, err, ErrInvalidPolicy)
				require.Empty(t, policy.alternatives)
			} else {
				require.NoError(t, err)
				require.Len(t, policy.alternatives, len(tc.rules))
			}
		})
	}
}

// TestAuthorize checks complete alternatives, exact binding, exclusive expiry,
// and malformed evidence. Members need not borrow a guest's scoped permissions.
func TestAuthorize(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	policy, err := Compile(
		Requirement{Kind: "member", Assurances: []string{"verified"}},
		Requirement{Kind: "guest", Capabilities: []string{"read", "comment"}, Assurances: []string{"invited"}, ResourceKind: "document", RequireExpiry: true},
	)
	require.NoError(t, err)
	for _, tc := range []struct {
		// name identifies a separate trust or grammar boundary.
		name string
		// member selects the other complete branch rather than merging facts.
		member bool
		// change mutates a fresh case-owned proof, target or clock.
		change func(*Evidence, *Resource, *time.Time)
		// allowed asserts only total admission, without exposing failure details.
		allowed bool
	}{
		{name: "bound guest", allowed: true},
		{name: "member without guest capabilities", member: true, allowed: true},
		{name: "missing member assurance", member: true, change: func(e *Evidence, _ *Resource, _ *time.Time) { e.Assurances = nil }},
		{name: "assurance does not inherit", member: true, change: func(e *Evidence, _ *Resource, _ *time.Time) { e.Assurances = []string{"admin"} }},
		{name: "empty subject", change: func(e *Evidence, _ *Resource, _ *time.Time) { e.SubjectID = "" }},
		{name: "invalid subject", change: func(e *Evidence, _ *Resource, _ *time.Time) { e.SubjectID = "subject\n" }},
		{name: "unknown category", change: func(e *Evidence, _ *Resource, _ *time.Time) { e.Kind = "api" }},
		{name: "one capability missing", change: func(e *Evidence, _ *Resource, _ *time.Time) { e.Capabilities = []string{"read"} }},
		{name: "extra capabilities allowed", allowed: true, change: func(e *Evidence, _ *Resource, _ *time.Time) { e.Capabilities = append(e.Capabilities, "edit") }},
		{name: "wildcards do not confer permission", change: func(e *Evidence, _ *Resource, _ *time.Time) { e.Capabilities = []string{"*"} }},
		{name: "no cross-branch fact merging", change: func(e *Evidence, _ *Resource, _ *time.Time) {
			e.Assurances = []string{"verified"}
			e.Capabilities = nil
		}},
		{name: "binding absent", change: func(e *Evidence, _ *Resource, _ *time.Time) { e.Binding = Resource{} }},
		{name: "binding half empty", change: func(e *Evidence, _ *Resource, _ *time.Time) { e.Binding.ID = "" }},
		{name: "wrong resource", change: func(e *Evidence, _ *Resource, _ *time.Time) { e.Binding.ID = "other" }},
		{name: "wrong namespace", change: func(e *Evidence, _ *Resource, _ *time.Time) { e.Binding.Kind = "invoice" }},
		{name: "target absent", change: func(_ *Evidence, r *Resource, _ *time.Time) { *r = Resource{} }},
		{name: "invalid target", change: func(_ *Evidence, r *Resource, _ *time.Time) { r.ID = " " }},
		{name: "expiry missing", change: func(e *Evidence, _ *Resource, _ *time.Time) { e.ExpiresAt = time.Time{} }},
		{name: "exact deadline denied", change: func(e *Evidence, _ *Resource, n *time.Time) { e.ExpiresAt = *n }},
		{name: "past deadline denied", change: func(e *Evidence, _ *Resource, n *time.Time) { e.ExpiresAt = n.Add(-time.Nanosecond) }},
		{name: "one nanosecond before deadline", allowed: true, change: func(e *Evidence, _ *Resource, n *time.Time) { e.ExpiresAt = n.Add(time.Nanosecond) }},
		{name: "optional exposed expiry still enforced", member: true, change: func(e *Evidence, _ *Resource, n *time.Time) { e.ExpiresAt = *n }},
		{name: "zero clock", change: func(_ *Evidence, _ *Resource, n *time.Time) { *n = time.Time{} }},
		{name: "duplicate capability", change: func(e *Evidence, _ *Resource, _ *time.Time) { e.Capabilities = append(e.Capabilities, "read") }},
		{name: "duplicate assurance", change: func(e *Evidence, _ *Resource, _ *time.Time) { e.Assurances = append(e.Assurances, "invited") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			clock := now
			target := Resource{Kind: "document", ID: "doc-1"}
			evidence := Evidence{Kind: "guest", SubjectID: "guest-1", Capabilities: []string{"read", "comment"}, Assurances: []string{"invited"}, Binding: target, ExpiresAt: now.Add(time.Minute)}
			if tc.member {
				evidence = Evidence{Kind: "member", SubjectID: "user-1", Assurances: []string{"verified"}}
			}
			if tc.change != nil {
				tc.change(&evidence, &target, &clock)
			}
			err := policy.Authorize(evidence, target, clock)
			if tc.allowed {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrDenied)
				require.Equal(t, ErrDenied.Error(), err.Error())
			}
			require.ErrorIs(t, (Policy{}).Authorize(evidence, target, clock), ErrDenied)
		})
	}
}

// TestPolicyCopiesConfiguration ensures later host edits cannot weaken a
// compiled policy. Each related slice/field case owns its mutable input.
func TestPolicyCopiesConfiguration(t *testing.T) {
	for _, field := range []string{"kind", "assurance", "capability", "binding", "expiry"} {
		t.Run(field, func(t *testing.T) {
			rules := []Requirement{{Kind: "guest", Assurances: []string{"invited"}, Capabilities: []string{"read"}, ResourceKind: "document", RequireExpiry: true}}
			policy, err := Compile(rules...)
			require.NoError(t, err)
			switch field {
			case "kind":
				rules[0].Kind = "member"
			case "assurance":
				rules[0].Assurances[0] = "unverified"
			case "capability":
				rules[0].Capabilities[0] = "edit"
			case "binding":
				rules[0].ResourceKind = ""
			case "expiry":
				rules[0].RequireExpiry = false
			}
			now := time.Now()
			target := Resource{Kind: "document", ID: "doc-1"}
			evidence := Evidence{Kind: "guest", SubjectID: "guest-1", Assurances: []string{"invited"}, Capabilities: []string{"read"}, Binding: target, ExpiresAt: now.Add(time.Minute)}
			require.NoError(t, policy.Authorize(evidence, target, now))
			evidence.ExpiresAt = time.Time{}
			require.ErrorIs(t, policy.Authorize(evidence, target, now), ErrDenied)
		})
	}
}
