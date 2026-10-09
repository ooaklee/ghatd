package paymentprovider

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// VerifyRetainedRevenueSnapshot derives a versioned identity only from bytes
// that reproduce the exact legacy source hash. It makes no provider request,
// creates no delivery signature and changes no source or financial record.
// Authenticated event retrieval must separately match the derived identity.
func (s *StripeProvider) VerifyRetainedRevenueSnapshot(ctx context.Context, req RevenueSnapshotRequest) (RevenueSnapshotIdentity, error) {
	if ctx == nil {
		return RevenueSnapshotIdentity{}, ErrPaymentProviderInvalidPayload
	}
	if err := ctx.Err(); err != nil {
		return RevenueSnapshotIdentity{}, err
	}
	if s == nil || s.config == nil || s.config.Revenue == nil {
		return RevenueSnapshotIdentity{}, ErrRevenueNotEnabled
	}
	if len(req.OriginalSnapshot) == 0 || int64(len(req.OriginalSnapshot)) > s.maxWebhookBodySize || len(req.OriginalFingerprint) != 64 || strings.ToLower(req.OriginalFingerprint) != req.OriginalFingerprint {
		return RevenueSnapshotIdentity{}, ErrPaymentProviderInvalidPayload
	}
	if _, err := hex.DecodeString(req.OriginalFingerprint); err != nil {
		return RevenueSnapshotIdentity{}, ErrPaymentProviderInvalidPayload
	}
	var event stripeRevenueEvent
	if json.Unmarshal(req.OriginalSnapshot, &event) != nil || !stripeRevenueObjectID.MatchString(event.ID) || event.ID != req.EnvelopeID || event.LiveMode == nil || event.Created <= 0 || event.Data.Object == nil {
		return RevenueSnapshotIdentity{}, ErrPaymentProviderInvalidPayload
	}
	switch event.Type {
	case "invoice.paid", "invoice.payment_succeeded", "charge.refunded", "charge.dispute.created", "charge.dispute.updated", "charge.dispute.closed":
	default:
		return RevenueSnapshotIdentity{}, ErrRevenueEventNotRelevant
	}
	scope, err := s.revenueScope(event.Account, *event.LiveMode)
	if err != nil {
		return RevenueSnapshotIdentity{}, err
	}
	if scope != req.Scope {
		return RevenueSnapshotIdentity{}, ErrRevenueUnassessable
	}
	legacy, err := legacyStripeRevenueDigest(event)
	if err != nil {
		return RevenueSnapshotIdentity{}, err
	}
	if legacy != req.OriginalFingerprint {
		return RevenueSnapshotIdentity{}, ErrRevenueUnassessable
	}
	canonical, err := canonicalStripeRevenueDigest(event)
	if err != nil {
		return RevenueSnapshotIdentity{}, err
	}
	return RevenueSnapshotIdentity{Scope: scope, EnvelopeID: event.ID, OriginalFingerprint: legacy, CanonicalFingerprint: canonical}, nil
}
