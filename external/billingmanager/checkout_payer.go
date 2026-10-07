package billingmanager

import (
	"context"
	"strings"

	"github.com/ooaklee/ghatd/external/billing"
	user "github.com/ooaklee/ghatd/external/user/v2"
)

type CheckoutPayerUserService interface {
	GetUserByID(context.Context, *user.GetUserByIDRequest) (*user.GetUserByIDResponse, error)
}

// UserCheckoutPayerConfig explicitly identifies self-paying owning account types
// and current statuses. There are no assumed type/status names or seat fallbacks.
type UserCheckoutPayerConfig struct {
	OwningAccountTypes, ActiveAccountStatuses []string
}
type UserCheckoutPayerAuthority struct {
	owner  CheckoutPayerUserService
	config UserCheckoutPayerConfig
}

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
func checkoutPayerContains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
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
