package policy

import (
	"context"
	"testing"
)

func TestGenerateStaticPoliciesIncludesSecurityAndCompliance(t *testing.T) {
	store := NewStore(
		"Example",
		"support@example.com",
		"https://example.com",
		"Example Ltd",
	)
	store.GenerateStaticPolicies()

	service := NewService(store)
	policy, err := service.GetPolicyByName(context.Background(), &GetPolicyByNameRequest{
		PolicyName: "security-and-compliance",
	})
	if err != nil {
		t.Fatalf("expected security and compliance policy to be found: %v", err)
	}

	if policy.Type != SecurityAndCompliancePolicy {
		t.Fatalf("expected type %q, got %q", SecurityAndCompliancePolicy, policy.Type)
	}

	if len(policy.TableOfContentsItems) == 0 {
		t.Fatal("expected table of contents items to be generated")
	}
}
