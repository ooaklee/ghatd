// Package policy implements a policy management service for web applications.
// It provides functionality for storing, retrieving, and managing application
// policies through a configurable policy store.
//
// The package supports both static and dynamic policy definitions and allows
// for flexible policy lookup by name.
package policy

import (
	"context"
	"strings"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.uber.org/zap"
)

// PolicyStore defines the interface for policy storage and retrieval.
// Implementations can provide in-memory, database-backed, or other storage mechanisms.
type PolicyStore interface {
	// GenerateStaticPolicies initializes the store with predefined policies.
	GenerateStaticPolicies()

	// GetPolicies returns all policies in the store.
	GetPolicies() []WebAppPolicy

	// AddPolicy adds a new policy to the store.
	AddPolicy(policy WebAppPolicy)
}

// Service manages policy business logic and provides access to policy data.
type Service struct {
	Store PolicyStore
}

// NewService creates a new policy service with the provided store.
//
// The store should be initialized with any required policies before
// creating the service.
func NewService(store PolicyStore) *Service {
	return &Service{
		Store: store,
	}
}

// GetPolicies retrieves all policies from the store.
func (s *Service) GetPolicies(ctx context.Context, r *GetPoliciesRequest) ([]WebAppPolicy, error) {
	return s.Store.GetPolicies(), nil
}

// GetPolicyByName retrieves a policy by its name.
//
// Policy names are normalized to lowercase with spaces replaced by hyphens
// for consistent matching. Returns an error if no matching policy is found.
func (s *Service) GetPolicyByName(ctx context.Context, r *GetPolicyByNameRequest) (*WebAppPolicy, error) {
	log := logger.Get(ctx).WithOptions(zap.AddStacktrace(zap.DPanicLevel))

	// Normalize requested policy name
	standardizedName := standardisePolicyName(r.PolicyName)

	for _, policy := range s.Store.GetPolicies() {
		policyName := standardisePolicyName(policy.Name)

		log.Debug("comparing-policy-names",
			zap.String("policy", policyName),
			zap.String("requested", standardizedName))

		if policyName == standardizedName {
			return &policy, nil
		}
	}

	return nil, ErrPolicyNotFound
}

// standardisePolicyName normalizes a policy name to lowercase with spaces
// replaced by hyphens for consistent matching.
func standardisePolicyName(name string) string {
	return strings.ReplaceAll(
		toolbox.StringStandardisedToLower(name),
		" ",
		"-")
}
