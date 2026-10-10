package accessmanager

import (
	"context"

	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/apitoken"
)

// TokenCreationPolicy supplies live limits from a server-configured system.
// WithTokenCreation must fence grant changes, then commit the callback's owner
// inventory fence, exact counts and insert in the same transaction. The
// callback may retry. Implementations and token adapters must preserve its
// context/client; a mutex around only the policy read is not sufficient.
// The persistence adapter owns transient-abort retries (the built-in Mongo
// policy uses the driver's WithTransaction). Never restart issuance in the
// manager after an uncertain commit or exhausted transaction retry.
type TokenCreationPolicy interface {
	// TokenLimits provides current display limits, never admission authority.
	TokenLimits(context.Context, string) (accesspolicy.TokenLimits, error)
	// WithTokenCreation returns nil only after the callback's writes commit.
	WithTokenCreation(context.Context, string, func(context.Context, accesspolicy.TokenLimits) error) error
}

// tokenInventory is the transitional legacy exact-count capability. Policy
// admission requires fencedTokenInventory instead; neither may use lists.
type tokenInventory interface {
	// CountTokenInventory returns the exact token inventory for the identified
	// owner; this transitional legacy capability must not use lists and policy
	// admission requires the fenced variant.
	CountTokenInventory(context.Context, string) (apitoken.Inventory, error)
}

// fencedTokenInventory serializes the counted inventory independently of the
// policy's system. A custom adapter must preserve the supplied transaction.
type fencedTokenInventory interface {
	// CountTokenInventoryFenced returns the counted token inventory serialized
	// independently of the policy's system; custom adapters must preserve the
	// supplied transaction.
	CountTokenInventoryFenced(context.Context, string) (apitoken.Inventory, error)
}

// createPolicyToken admits inventory against a live owner grant, independent of
// roles and signed claims. The exported command requires verified session
// context; current owner state is checked inside each transaction attempt.
// Grants for using the issued credential are separate and are not copied from
// the owner. No secret is returned on a failed/uncertain transaction outcome.
func (s *Service) createPolicyToken(ctx context.Context, r *CreateUserAPITokenRequest) (*CreateUserAPITokenResponse, error) {
	// The exported entry point validates request/context and selects this path.
	inventory, ok := s.ApitokenService.(fencedTokenInventory)
	if !ok || nilAccessDependency(s.ApitokenService) || nilAccessDependency(s.UserService) || nilAccessDependency(s.tokenPolicy) {
		return nil, ErrTokenPolicyUnavailable
	}
	var response *CreateUserAPITokenResponse
	err := s.tokenPolicy.WithTokenCreation(ctx, r.UserID, func(tx context.Context, limits accesspolicy.TokenLimits) error {
		response = nil // An aborted attempt must never become a success response.
		if err := tokenCreationContext(ctx, tx); err != nil {
			return err
		}
		if err := checkTokenLifetime(r.Ttl, limits); err != nil {
			return err
		}
		user, err := s.tokenManagementOwner(tx, r.ActorID)
		if contextErr := tokenCreationContext(ctx, tx); contextErr != nil {
			return contextErr
		}
		if err != nil {
			return err
		}
		count, err := inventory.CountTokenInventoryFenced(tx, r.UserID)
		if contextErr := tokenCreationContext(ctx, tx); contextErr != nil {
			return contextErr
		}
		if err != nil {
			return err
		}
		if count.Permanent < 0 || count.Ephemeral < 0 {
			return ErrTokenPolicyUnavailable
		}
		if r.Ttl == 0 && count.Permanent >= limits.Permanent {
			return ErrPermanentAPITokenLimitReached
		}
		if r.Ttl > 0 && count.Ephemeral >= limits.Ephemeral {
			return ErrEphemeralAPITokenLimitReached
		}
		created, err := s.ApitokenService.CreateAPIToken(tx, &apitoken.CreateAPITokenRequest{UserID: user.ID, UserNanoId: user.NanoID, TokenTtl: r.Ttl, Description: r.Description})
		if contextErr := tokenCreationContext(ctx, tx); contextErr != nil {
			return contextErr
		}
		if err != nil {
			return err
		}
		response, err = createdTokenResponse(created, r.UserID)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if response == nil {
		return nil, ErrTokenPolicyUnavailable
	}
	return response, nil
}

// checkTokenLifetime validates adapter-supplied limits before any modulo or
// duration arithmetic. Zero inventory means disabled, never unlimited.
func checkTokenLifetime(ttl int64, limits accesspolicy.TokenLimits) error {
	if accesspolicy.ValidateTokenLimits(limits) != nil {
		return ErrTokenPolicyUnavailable
	}
	if ttl < 0 {
		return apitoken.ErrInvalidTokenTTL
	}
	if ttl == 0 {
		return nil
	}
	if limits.Ephemeral == 0 {
		return ErrEphemeralAPITokenLimitReached
	}
	if ttl < limits.MinimumTTL {
		return ErrCreateUserAPITokenRequestTtlTooShort
	}
	if ttl > limits.MaximumTTL {
		return ErrCreateUserAPITokenRequestTtlTooLong
	}
	if ttl%limits.TTLIncrement != 0 {
		return ErrCreateUserAPITokenRequestTtlOutsideAllowedIncrement
	}
	return nil
}

// policyTokenThreshold keeps the established response schema while sourcing
// values from live policy. Missing grants never display legacy role allowances.
func (s *Service) policyTokenThreshold(ctx context.Context, userID string) (*GetUserAPITokenThresholdResponse, error) {
	if nilAccessDependency(s.tokenPolicy) {
		return nil, ErrTokenPolicyUnavailable
	}
	limits, err := s.tokenPolicy.TokenLimits(ctx, userID)
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	if err != nil {
		return nil, err
	}
	if err := checkTokenLifetime(0, limits); err != nil {
		return nil, err
	}
	return &GetUserAPITokenThresholdResponse{PermanentUserTokenLimit: limits.Permanent, EphemeralUserTokenLimit: limits.Ephemeral, EphemeralMinimumAllowedTime: limits.MinimumTTL, EphemeralMaximumAllowedTime: limits.MaximumTTL, EphemeralMinimumIncrements: limits.TTLIncrement}, nil
}

// tokenCreationContext checks both caller and transaction cancellation so a
// custom adapter cannot discard the caller's cancellation by replacing context.
// This cannot undo a write or establish whether an uncertain commit occurred.
func tokenCreationContext(caller, transaction context.Context) error {
	if err := caller.Err(); err != nil {
		return err
	}
	if transaction == nil {
		return ErrTokenPolicyUnavailable
	}
	return transaction.Err()
}

// createdTokenResponse rejects inconsistent custom adapter results and copies
// mutable digest data before any enclosing transaction can publish the secret.
// The token domain remains responsible for generation, digest and TTL validity.
func createdTokenResponse(created *apitoken.CreateAPITokenResponse, owner string) (*CreateUserAPITokenResponse, error) {
	if created == nil || created.APIToken.ID == "" || created.APIToken.Value == "" || created.APIToken.CreatedByID != owner || created.APIToken.Status != apitoken.UserTokenStatusKeyActive {
		return nil, ErrTokenPolicyUnavailable
	}
	token := created.APIToken
	token.ValueSHA = append([]byte(nil), token.ValueSHA...)
	return &CreateUserAPITokenResponse{UserAPIToken: token}, nil
}
