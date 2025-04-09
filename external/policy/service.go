package policy

import (
	"context"
	"errors"
	"strings"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/toolbox"
	"go.uber.org/zap"
)

// PolicyStore holds valid methods for valid store that holds policies
type PolicyStore interface {
	GenerateStaticPolicies()
	GetPolicies() []WebAppPolicy
	AddPolicy(policy WebAppPolicy)
}

// Service holds and manages policy business logic
type Service struct {
	Store PolicyStore
}

// NewService created policy service
func NewService(store PolicyStore) *Service {
	return &Service{
		Store: store,
	}
}

// GetPolicies returns a slice of WebAppPolicy instances representing the static policies.
func (s *Service) GetPolicies(ctx context.Context, r *GetPoliciesRequest) ([]WebAppPolicy, error) {
	return s.Store.GetPolicies(), nil
}

// GetPolicyByName retrieves a WebAppPolicy by its name. It iterates through the static policies
// and returns the first policy that matches the requested policy name. If no matching policy is
// found, it returns an error.
func (s *Service) GetPolicyByName(ctx context.Context, r *GetPolicyByNameRequest) (*WebAppPolicy, error) {

	logger := logger.AcquireFrom(ctx).WithOptions(
		zap.AddStacktrace(zap.DPanicLevel),
	)

	for _, policy := range s.Store.GetPolicies() {

		policyName := strings.ReplaceAll(
			toolbox.StringStandardisedToLower(policy.Name),
			" ",
			"-")

		logger.Info("policy-names-to-compare", zap.String("policy-name", policyName), zap.String("requested-policy-name", r.PolicyName))

		if policyName == r.PolicyName {
			return &policy, nil
		}
	}

	return nil, errors.New(ErrKeyPolicyNotFound)
}
