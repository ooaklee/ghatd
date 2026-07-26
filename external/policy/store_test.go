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

func TestRemovePolicyByType(t *testing.T) {
	store := &Store{
		Policies: []WebAppPolicy{
			{Name: "Terms", Type: TermsOfServicePolicy},
			{Name: "Privacy", Type: PrivacyPolicy},
			{Name: "Cookies", Type: CookiesPolicy},
		},
	}

	if removed := store.RemovePolicyByType(PrivacyPolicy); !removed {
		t.Fatal("expected privacy policy to be removed")
	}

	if len(store.Policies) != 2 {
		t.Fatalf("expected 2 policies, got %d", len(store.Policies))
	}
	if store.Policies[0].Type != TermsOfServicePolicy || store.Policies[1].Type != CookiesPolicy {
		t.Fatalf("unexpected policies after removal: %#v", store.Policies)
	}

	if removed := store.RemovePolicyByType(RefundPolicy); removed {
		t.Fatal("expected removing a missing policy to report false")
	}
}

func TestRemovePolicyByNameNormalizesName(t *testing.T) {
	store := &Store{
		Policies: []WebAppPolicy{
			{Name: "Security and Compliance", Type: SecurityAndCompliancePolicy},
			{Name: "Privacy Policy", Type: PrivacyPolicy},
		},
	}

	if removed := store.RemovePolicyByName("security-and-compliance"); !removed {
		t.Fatal("expected normalized policy name to match")
	}

	if len(store.Policies) != 1 || store.Policies[0].Type != PrivacyPolicy {
		t.Fatalf("unexpected policies after removal: %#v", store.Policies)
	}

	if removed := store.RemovePolicyByName("missing-policy"); removed {
		t.Fatal("expected removing a missing policy to report false")
	}
}

func TestRemovePolicyRemovesOnlyFirstMatch(t *testing.T) {
	typeStore := &Store{
		Policies: []WebAppPolicy{
			{Name: "Privacy Policy", Type: PrivacyPolicy},
			{Name: "Privacy Policy", Type: PrivacyPolicy},
		},
	}

	if removed := typeStore.RemovePolicyByType(PrivacyPolicy); !removed {
		t.Fatal("expected policy to be removed")
	}

	if len(typeStore.Policies) != 1 {
		t.Fatalf("expected only the first matching policy type to be removed, got %d policies", len(typeStore.Policies))
	}

	nameStore := &Store{
		Policies: []WebAppPolicy{
			{Name: "Privacy Policy", Type: PrivacyPolicy},
			{Name: "privacy-policy", Type: RefundPolicy},
		},
	}

	if removed := nameStore.RemovePolicyByName("privacy policy"); !removed {
		t.Fatal("expected policy to be removed")
	}

	if len(nameStore.Policies) != 1 || nameStore.Policies[0].Type != RefundPolicy {
		t.Fatalf("expected only the first matching policy name to be removed: %#v", nameStore.Policies)
	}
}

func TestRemovePolicyFromEmptyStore(t *testing.T) {
	store := &Store{}

	if store.RemovePolicyByType(PrivacyPolicy) {
		t.Fatal("expected type removal from an empty store to report false")
	}
	if store.RemovePolicyByName("Privacy Policy") {
		t.Fatal("expected name removal from an empty store to report false")
	}
}

func TestRemoveOnlyPolicy(t *testing.T) {
	store := &Store{
		Policies: []WebAppPolicy{
			{Name: "Privacy Policy", Type: PrivacyPolicy},
		},
	}

	if removed := store.RemovePolicyByType(PrivacyPolicy); !removed {
		t.Fatal("expected policy to be removed")
	}
	if len(store.Policies) != 0 {
		t.Fatalf("expected store to be empty, got %d policies", len(store.Policies))
	}
}
