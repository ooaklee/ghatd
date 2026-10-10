package partnermanager

import (
	"context"
	"fmt"

	"github.com/ooaklee/ghatd/external/partnerearnings"
)

// ClaimsConfig supplies a host-approved minimum in the program currency's minor
// units. Zero preserves the existing positive-amount contract; it does not
// imply commercial approval. This value gates new claims, never original receipts.
type ClaimsConfig struct{ MinimumMinor int64 }

// SelfClaimRequest freezes the amount and destination revision selected for one
// customer intent. Actor identity is a separate verified argument. The owning
// destination supplies its address; a browser cannot supply another recipient.
type SelfClaimRequest struct {
	AmountMinor                int64
	ExpectedDestinationVersion int64
	IdempotencyKey             string
}

// RequestClaimWithDestination recovers an authorized original receipt before
// evaluating changed minimums, destination state or new-admission pauses. A
// changed amount or destination revision under its original key conflicts.
func (m *Manager) RequestClaimWithDestination(ctx context.Context, actor string, req SelfClaimRequest) (partnerearnings.Claim, error) {
	if req.ExpectedDestinationVersion < 1 {
		return partnerearnings.Claim{}, ErrInvalid
	}
	return m.requestClaim(ctx, actor, req.AmountMinor, req.ExpectedDestinationVersion, req.IdempotencyKey)
}

// RequestClaim retains the original API's owning current-destination selection.
// New interactive hosts should use RequestClaimWithDestination to bind the
// customer's observed destination revision and detect stale selection.
func (m *Manager) RequestClaim(ctx context.Context, actor string, amount int64, key string) (partnerearnings.Claim, error) {
	return m.requestClaim(ctx, actor, amount, 0, key)
}

// requestClaim reserves a self-service payout for the authenticated partner. It
// replays an original receipt only when partner, actor, amount, currency and
// (when supplied) destination version match, rechecking capability on replay.
// Fresh requests enforce claim controls, payout admission, the configured
// minimum and a current owned destination, optionally requiring an exact
// destination version.
func (m *Manager) requestClaim(ctx context.Context, actor string, amount, destinationVersion int64, key string) (partnerearnings.Claim, error) {
	p, err := m.self(ctx, actor, CapabilityClaims)
	if err != nil {
		return partnerearnings.Claim{}, err
	}
	if amount <= 0 || !validWorkText(key, 256) {
		return partnerearnings.Claim{}, ErrInvalid
	}
	claim, err := m.deps.Earnings.FindClaimRequest(ctx, actor, p.ID, key)
	if err == nil {
		if claim.PartnerID != p.ID || claim.RequestedBy != actor || claim.AmountMinor != amount || claim.Currency != m.deps.Program.Config().Currency || (destinationVersion > 0 && claim.DestinationSnapshot["version"] != fmt.Sprint(destinationVersion)) {
			return partnerearnings.Claim{}, partnerearnings.ErrConflict
		}
		if err := m.authorize(ctx, actor, CapabilityClaims, actor); err != nil {
			return partnerearnings.Claim{}, err
		}
		return claim, nil
	}
	if !singleManagerAbsence(err, partnerearnings.ErrNotFound) {
		return partnerearnings.Claim{}, err
	}
	if !m.deps.Controls.Claims {
		return partnerearnings.Claim{}, ErrDenied
	}
	if !p.CanRequestPayouts {
		return partnerearnings.Claim{}, ErrDenied
	}
	if amount < m.deps.Claims.MinimumMinor {
		return partnerearnings.Claim{}, ErrInvalid
	}
	d, err := m.deps.Program.GetPayoutDestination(ctx, actor)
	if err != nil {
		return partnerearnings.Claim{}, err
	}
	if d.CustomerID != actor || d.ID == "" || d.Version < 1 {
		return partnerearnings.Claim{}, ErrUnavailable
	}
	if destinationVersion > 0 && d.Version != destinationVersion {
		return partnerearnings.Claim{}, partnerearnings.ErrStaleWrite
	}
	if err := m.authorize(ctx, actor, CapabilityClaims, actor); err != nil {
		return partnerearnings.Claim{}, err
	}
	return m.deps.Earnings.RequestClaim(ctx, partnerearnings.ClaimRequest{ActorID: actor, PartnerID: p.ID, AmountMinor: amount, Currency: m.deps.Program.Config().Currency, DestinationID: d.ID, DestinationSnapshot: map[string]string{"destination_id": d.ID, "method": d.Method, "email": d.Email, "version": fmt.Sprint(d.Version)}, IdempotencyKey: key})
}
