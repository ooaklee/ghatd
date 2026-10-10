package billingmanager

import (
	"context"
	"strings"

	"github.com/ooaklee/ghatd/external/billing"
	user "github.com/ooaklee/ghatd/external/user/v2"
)

// CheckoutPayerUserService is the owning user-lookup port used to confirm a
// checkout payer's account type, status and email verification. The host
// supplies the trusted user service; responses are validated by the caller.
type CheckoutPayerUserService interface {
	// GetUserByID retrieves the user account by ID from the trusted host user
	// service, supplying the account details used to confirm a checkout payer's
	// type, status and email verification.
	GetUserByID(context.Context, *user.GetUserByIDRequest) (*user.GetUserByIDResponse, error)
}

// UserCheckoutPayerConfig explicitly identifies self-paying owning account types
// and current statuses. There are no assumed type/status names or seat fallbacks.
type UserCheckoutPayerConfig struct {
	OwningAccountTypes, ActiveAccountStatuses []string
}

// UserCheckoutPayerAuthority implements CheckoutPayerAuthority by resolving the
// actor through the owning user service and comparing its account against
// explicitly configured owning types, active statuses and email verification.
type UserCheckoutPayerAuthority struct {
	owner  CheckoutPayerUserService
	config UserCheckoutPayerConfig
}

// NewUserCheckoutPayerAuthority validates the owner and both configuration
// lists, then copies the lists so later caller mutation cannot change authority
// decisions. A nil owner returns ErrRevenueUnavailable; invalid lists return
// ErrRevenueInvalid.
func NewUserCheckoutPayerAuthority(owner CheckoutPayerUserService, config UserCheckoutPayerConfig) (*UserCheckoutPayerAuthority, error) {
	if nilRevenueDependency(owner) {
		return nil, billing.ErrRevenueUnavailable
	}
	if !checkoutPayerChoices(config.OwningAccountTypes) || !checkoutPayerChoices(config.ActiveAccountStatuses) {
		return nil, billing.ErrRevenueInvalid
	}
	config.OwningAccountTypes = append([]string(nil), config.OwningAccountTypes...)
	config.ActiveAccountStatuses = append([]string(nil), config.ActiveAccountStatuses...)
	return &UserCheckoutPayerAuthority{owner, config}, nil
}

// checkoutPayerChoices accepts only a non-empty list of at most 100 unique,
// trimmed, non-blank values without control characters, each at most 128 bytes.
func checkoutPayerChoices(values []string) bool {
	if len(values) == 0 || len(values) > 100 {
		return false
	}
	seen := map[string]bool{}
	for _, value := range values {
		if value == "" || value != strings.TrimSpace(value) || len(value) > 128 || strings.ContainsAny(value, "\r\n\x00") || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

// checkoutPayerContains reports whether value appears verbatim in values;
// matching is exact with no trimming or case folding.
func checkoutPayerContains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// AuthorizeCheckoutPayer loads the actor's account from the owning user service
// and denies with ErrBillingManagerUserUnauthorisedToCarryOutOperation unless
// the account type and status are configured as owning/active and the email is
// verified. Lookup and transport failures are returned unchanged.
func (a *UserCheckoutPayerAuthority) AuthorizeCheckoutPayer(ctx context.Context, actor string) error {
	if ctx == nil || actor == "" || actor != strings.TrimSpace(actor) || len(actor) > 256 || strings.ContainsAny(actor, "\r\n\x00") {
		return billing.ErrRevenueInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if a == nil || nilRevenueDependency(a.owner) {
		return billing.ErrRevenueUnavailable
	}
	response, err := a.owner.GetUserByID(ctx, &user.GetUserByIDRequest{ID: actor})
	if err != nil {
		return err
	}
	if response == nil || response.User == nil || response.User.ID != actor {
		return billing.ErrRevenueUnavailable
	}
	account := response.User
	if !checkoutPayerContains(a.config.OwningAccountTypes, account.Type) || !checkoutPayerContains(a.config.ActiveAccountStatuses, account.Status) || account.Verification == nil || !account.Verification.EmailVerified {
		return ErrBillingManagerUserUnauthorisedToCarryOutOperation
	}
	return nil
}

var _ CheckoutPayerAuthority = (*UserCheckoutPayerAuthority)(nil)
