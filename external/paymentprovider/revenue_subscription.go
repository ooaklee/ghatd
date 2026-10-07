package paymentprovider

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
)

// RevenueSubscriptionProvider is optional authenticated current lifecycle
// evidence. Status is not proof of paid revenue, entitlement or commission.
type RevenueSubscriptionProvider interface {
	LookupRevenueSubscription(context.Context, RevenueScope, string) (RevenueSubscriptionEvidence, error)
}

// RevenueSubscriptionEvidence is private provider output, never browser input.
// CancellationScheduled preserves an active subscription's scheduled ending;
// trialing and paused remain distinct from active, without consulting access.
type RevenueSubscriptionEvidence struct {
	Scope                              RevenueScope `json:"-"`
	SubscriptionID, CustomerID, Status string       `json:"-"`
	CancellationScheduled              bool         `json:"-"`
}

func validSubscriptionStatus(status string) bool {
	switch status {
	case "active", "trialing", "incomplete", "incomplete_expired", "past_due", "unpaid", "canceled", "paused":
		return true
	default:
		return false
	}
}

// LookupRevenueSubscription retrieves the exact subscription with merchant
// authentication, configured account allowlisting and explicit mode validation.
// No metadata/email/customer search or catalogue-price fallback is permitted.
func (s *StripeProvider) LookupRevenueSubscription(ctx context.Context, scope RevenueScope, id string) (RevenueSubscriptionEvidence, error) {
	if ctx == nil {
		return RevenueSubscriptionEvidence{}, ErrPaymentProviderAPIRequestFailed
	}
	if err := ctx.Err(); err != nil {
		return RevenueSubscriptionEvidence{}, err
	}
	if !strings.HasPrefix(id, "sub_") || !stripeRevenueObjectID.MatchString(id) {
		return RevenueSubscriptionEvidence{}, ErrRevenueUnassessable
	}
	if err := s.verifyCheckoutMerchant(ctx, scope); err != nil {
		return RevenueSubscriptionEvidence{}, err
	}
	object, err := s.revenueGet(ctx, scope, "/v1/subscriptions/"+url.PathEscape(id))
	if err != nil {
		return RevenueSubscriptionEvidence{}, err
	}
	status, customer := rawStripeString(object["status"]), rawStripeID(object["customer"])
	var scheduled *bool
	if rawStripeString(object["object"]) != "subscription" || rawStripeID(object["id"]) != id || !rawStripeMode(object, scope) || !strings.HasPrefix(customer, "cus_") || !stripeRevenueObjectID.MatchString(customer) || !validSubscriptionStatus(status) || json.Unmarshal(object["cancel_at_period_end"], &scheduled) != nil || scheduled == nil {
		return RevenueSubscriptionEvidence{}, ErrRevenueUnassessable
	}
	return RevenueSubscriptionEvidence{Scope: scope, SubscriptionID: id, CustomerID: customer, Status: status, CancellationScheduled: *scheduled}, nil
}

var _ RevenueSubscriptionProvider = (*StripeProvider)(nil)
