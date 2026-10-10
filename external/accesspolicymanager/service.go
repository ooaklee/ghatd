// Package accesspolicymanager coordinates explicit user token-policy review and
// provisioning over the accesspolicy domain. It owns no database connection and
// never derives allowances or API-credential permissions from roles or claims.
package accesspolicymanager

import (
	"context"
	"errors"
	"reflect"
	"unicode"
	"unicode/utf8"

	"github.com/ooaklee/ghatd/external/accesspolicy"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
)

var (
	// ErrInvalidRequest rejects malformed target IDs or incomplete limit requests.
	ErrInvalidRequest = errors.New("accesspolicymanager/invalid-request")
	// ErrUserNotFound identifies a missing stored target, never the requesting actor.
	ErrUserNotFound = errors.New("accesspolicymanager/user-not-found")
	// ErrPreconditionRequired requires an explicit reviewed revision before mutation.
	ErrPreconditionRequired = errors.New("accesspolicymanager/revision-required")
)

// UserReader resolves explicitly selected stored users, preserving dependency
// errors. A repository port avoids treating a failed database read as absence.
type UserReader interface {
	// GetUserByID resolves the explicitly selected stored user, preserving
	// dependency errors rather than treating a failed read as absence.
	GetUserByID(context.Context, string) (*userv2.UniversalUser, error)
}

// InventoryPreparer idempotently prepares the owner's inventory fence outside
// grant transactions. It creates neither a grant nor a credential. Hosts must
// initialize it and share the policy/token repository's managed client/database.
type InventoryPreparer interface {
	// PrepareTokenInventory idempotently prepares the owner's inventory fence
	// outside grant transactions; it creates neither a grant nor a credential.
	PrepareTokenInventory(context.Context, string) error
}

// Config supplies lower-domain ports and one trusted resource-system namespace.
type Config struct {
	// System cannot be selected by the HTTP request or an unverified claim.
	System string
	// Store is the same initialized policy store used by token enforcement.
	Store accesspolicy.Store
	// Authorize rechecks live session and current policy-management authority.
	Authorize accesspolicy.ManagementAuthorizer
	// Users resolves exact immutable IDs before any target policy is created.
	Users UserReader
	// Inventory prepares only the explicit target, never an account-wide scan.
	Inventory InventoryPreparer
}

// Service keeps management orchestration separate from token/grant persistence.
type Service struct {
	// system is immutable trusted configuration, copied during construction.
	system string
	// policy owns review snapshots, live authorization and audited revision CAS.
	policy *accesspolicy.Service
	// authorize guards target lookup and inventory preparation as well as writes.
	authorize accesspolicy.ManagementAuthorizer
	// users supplies durable target identity without lower-domain HTTP coupling.
	users UserReader
	// inventory prepares the independent owner-wide admission fence.
	inventory InventoryPreparer
}

// NewService validates wiring without network calls, collection setup or grants.
func NewService(config Config) (*Service, error) {
	if !identifier(config.System) || config.Authorize == nil || config.Users == nil || config.Inventory == nil {
		return nil, accesspolicy.ErrConfiguration
	}
	policy, err := accesspolicy.NewService(config.Store, config.Authorize)
	if err != nil {
		return nil, err
	}
	return &Service{system: config.System, policy: policy, authorize: config.Authorize, users: config.Users, inventory: config.Inventory}, nil
}

// Preview performs only live-authorized reads and returns a detached review.
// Existing disabled/expired policies are preserved. No target is inferred from
// the actor, an email, a role or request-supplied system identifier.
func (s *Service) Preview(ctx context.Context, userID string, limits accesspolicy.TokenLimits) (accesspolicy.TokenLimitPreview, error) {
	plan, err := s.plan(ctx, userID, limits)
	if err != nil {
		return accesspolicy.TokenLimitPreview{}, err
	}
	return plan.Preview(), nil
}

// Apply re-plans on the server and requires the operator's reviewed revision.
// Zero means create-only. It prepares inventory before audited policy CAS, so a
// failed apply can leave an authority-neutral fence; never delete it on failure.
// Errors return no policy/rollback receipt and do not prove the write rolled back.
// A new user's first grant is provisioned only by this explicit admin operation.
// Once the lower domain reports a known successful apply, preserve that receipt
// even if cancellation races its return; the transport may still fail to deliver it.
func (s *Service) Apply(ctx context.Context, userID string, expected int64, limits accesspolicy.TokenLimits) (accesspolicy.Grant, error) {
	if expected < 0 || expected > 9007199254740991 {
		return accesspolicy.Grant{}, ErrInvalidRequest
	}
	plan, err := s.plan(ctx, userID, limits)
	if err != nil {
		return accesspolicy.Grant{}, err
	}
	preview := plan.Preview()
	revision := int64(0)
	if preview.Before != nil {
		revision = preview.Before.Revision
	}
	if revision != expected {
		return accesspolicy.Grant{}, accesspolicy.ErrConflict
	}
	if err := s.checkManagement(ctx); err != nil {
		return accesspolicy.Grant{}, err
	}
	err = s.inventory.PrepareTokenInventory(ctx, userID)
	if contextErr := ctx.Err(); contextErr != nil {
		return accesspolicy.Grant{}, contextErr
	}
	if err != nil {
		return accesspolicy.Grant{}, err
	}
	receipt, err := s.policy.ApplyTokenLimits(ctx, plan)
	if err != nil {
		return accesspolicy.Grant{}, err
	}
	return receipt.Snapshot(), nil
}

// plan verifies authority before exposing target existence, then delegates the
// token-only patch to the policy domain. Runtime token issuance still verifies
// the target's current active state; this read is not a user-lifecycle lock.
func (s *Service) plan(ctx context.Context, userID string, limits accesspolicy.TokenLimits) (*accesspolicy.TokenLimitPlan, error) {
	if !identifier(userID) || accesspolicy.ValidateTokenLimits(limits) != nil {
		return nil, ErrInvalidRequest
	}
	if err := s.checkManagement(ctx); err != nil {
		return nil, err
	}
	user, err := s.users.GetUserByID(ctx, userID)
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	if missingTarget(err) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	if user == nil || user.ID != userID {
		return nil, accesspolicy.ErrConfiguration
	}
	return s.policy.PlanTokenLimits(ctx, accesspolicy.Subject{System: s.system, Kind: accesspolicy.UserSubject, ID: userID}, limits)
}

// missingTarget accepts only a bounded unary chain ending in the repository's
// actual absence sentinel. Joined/typed-nil/cyclic failures and custom Is aliases
// cannot establish absence. Preserve their original causes for the response
// boundary; an HTTP error-map override must not change provisioning decisions.
func missingTarget(err error) bool {
	for depth := 0; err != nil && depth < 64; depth++ {
		value := reflect.ValueOf(err)
		switch value.Kind() {
		case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
			if value.IsNil() {
				return false
			}
		}
		if _, multi := err.(interface{ Unwrap() []error }); multi {
			return false
		}
		if err == userv2.ErrUserNotFound {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

// checkManagement fails closed without conflating dependency errors and denial.
func (s *Service) checkManagement(ctx context.Context) error {
	if s == nil || ctx == nil || s.authorize == nil || s.policy == nil || s.users == nil || s.inventory == nil {
		return accesspolicy.ErrConfiguration
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	actor, err := s.authorize(ctx, s.system)
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	if err != nil {
		return err
	}
	if !identifier(actor) {
		return accesspolicy.ErrDenied
	}
	return ctx.Err()
}

// identifier enforces the policy domain's bounded byte-exact identity contract.
func identifier(value string) bool {
	if value == "" || len(value) > 256 || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
