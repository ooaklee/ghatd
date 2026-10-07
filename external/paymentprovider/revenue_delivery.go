package paymentprovider

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

// RevenueDeliveryVerifier authenticates immutable delivery identity before any
// financial API lookup, so an already committed delivery can be acknowledged
// during provider outages without changing original acceptance.
type RevenueDeliveryVerifier interface {
	VerifyRevenueDelivery(context.Context, *http.Request) (RevenueDeliveryIdentity, error)
}
type RevenueDeliveryIdentity struct {
	Scope             RevenueScope
	EnvelopeID        string
	SourceFingerprint string `json:"-"`
}

func canonicalStripeRevenueDigest(event stripeRevenueEvent) (string, error) {
	body, err := json.Marshal(event)
	if err != nil {
		return "", ErrPaymentProviderAPIResponseInvalid
	}
	var value any
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	if d.Decode(&value) != nil {
		return "", ErrPaymentProviderAPIResponseInvalid
	}
	body, err = json.Marshal(value)
	if err != nil {
		return "", ErrPaymentProviderAPIResponseInvalid
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}
func (s *StripeProvider) VerifyRevenueDelivery(ctx context.Context, req *http.Request) (RevenueDeliveryIdentity, error) {
	if ctx == nil {
		return RevenueDeliveryIdentity{}, ErrPaymentProviderInvalidPayload
	}
	if err := ctx.Err(); err != nil {
		return RevenueDeliveryIdentity{}, err
	}
	if s == nil || s.config == nil || s.config.Revenue == nil {
		return RevenueDeliveryIdentity{}, ErrRevenueNotEnabled
	}
	if err := s.VerifyWebhook(ctx, req); err != nil {
		return RevenueDeliveryIdentity{}, err
	}
	body, err := readAndRestoreWebhookBody(req, s.maxWebhookBodySize)
	if err != nil {
		return RevenueDeliveryIdentity{}, err
	}
	var event stripeRevenueEvent
	if json.Unmarshal(body, &event) != nil || !stripeRevenueObjectID.MatchString(event.ID) || event.LiveMode == nil || event.Created <= 0 || event.Data.Object == nil {
		return RevenueDeliveryIdentity{}, ErrPaymentProviderInvalidPayload
	}
	switch event.Type {
	case "invoice.paid", "invoice.payment_succeeded", "charge.refunded", "charge.dispute.created", "charge.dispute.updated", "charge.dispute.closed":
	default:
		return RevenueDeliveryIdentity{}, ErrRevenueEventNotRelevant
	}
	scope, err := s.revenueScope(event.Account, *event.LiveMode)
	if err != nil {
		return RevenueDeliveryIdentity{}, err
	}
	digest, err := canonicalStripeRevenueDigest(event)
	if err != nil {
		return RevenueDeliveryIdentity{}, err
	}
	return RevenueDeliveryIdentity{Scope: scope, EnvelopeID: event.ID, SourceFingerprint: digest}, nil
}
