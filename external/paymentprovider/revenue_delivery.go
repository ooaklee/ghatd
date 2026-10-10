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
	// VerifyRevenueDelivery authenticates the signed delivery request and returns
	// its immutable RevenueDeliveryIdentity before any financial API lookup,
	// allowing acknowledgement of committed deliveries during provider outages.
	// Part of the RevenueDeliveryVerifier contract; it does not change original
	// acceptance.
	VerifyRevenueDelivery(context.Context, *http.Request) (RevenueDeliveryIdentity, error)
}

// RevenueDeliveryIdentity is the authenticated result of verifying a delivery:
// scope, envelope ID and current source fingerprint plus a legacy fingerprint
// for exact replay of unchanged pre-versioned snapshots. Both fingerprints are
// private and JSON-omitted.
type RevenueDeliveryIdentity struct {
	Scope             RevenueScope
	EnvelopeID        string
	SourceFingerprint string `json:"-"`
	// LegacySourceFingerprint permits exact replay of an unchanged pre-versioned
	// snapshot. It never permits a changed legacy snapshot to replace history.
	LegacySourceFingerprint string `json:"-"`
}

const stripeRevenueDigestV2 = "stripe-revenue-v2:"

// canonicalStripeRevenueDigest binds the economic snapshot and immutable event
// identity without binding a charge's rendered receipt access URL. Stripe can
// render that URL differently in a signed delivery and the authenticated Event
// API response. Every other snapshot field remains part of the digest.
func canonicalStripeRevenueDigest(event stripeRevenueEvent) (string, error) {
	if event.Type == "charge.refunded" && rawStripeString(event.Data.Object["object"]) == "charge" {
		copyObject := make(map[string]json.RawMessage, len(event.Data.Object))
		// Raw values are read-only; sharing their bytes must never mutate evidence.
		for key, value := range event.Data.Object {
			if key != "receipt_url" {
				copyObject[key] = value
			}
		}
		event.Data.Object = copyObject
	}
	digest, err := legacyStripeRevenueDigest(event)
	if err != nil {
		return "", err
	}
	return stripeRevenueDigestV2 + digest, nil
}

// legacyStripeRevenueDigest reproduces previously retained full-object source
// hashes. It is only an exact original-snapshot/replay check, never permission
// to ignore changed historical evidence.
func legacyStripeRevenueDigest(event stripeRevenueEvent) (string, error) {
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

// VerifyRevenueDelivery authenticates the signed request first, then checks
// envelope shape and relevance and derives the scope and both source
// fingerprints. It performs no economic API lookup; malformed payloads,
// irrelevant events or disabled configuration return provider errors.
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
	legacy, err := legacyStripeRevenueDigest(event)
	if err != nil {
		return RevenueDeliveryIdentity{}, err
	}
	return RevenueDeliveryIdentity{Scope: scope, EnvelopeID: event.ID, SourceFingerprint: digest, LegacySourceFingerprint: legacy}, nil
}
