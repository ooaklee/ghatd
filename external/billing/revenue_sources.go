package billing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
)

// RevenueSourceRepository is the owning source-reconciliation capability. It
// keeps unresolved observations separate from per-consumer fact decisions;
// an envelope with no facts cannot be acknowledged using a fact cursor.
type RevenueSourceRepository interface {
	// GetRevenueObservation returns the unresolved or resolved observation for the
	// given identity; the service implementation validates the ID and requires the
	// optional source-reconciliation extension before forwarding.
	GetRevenueObservation(context.Context, string) (RevenueObservation, error)
	// UnresolvedRevenueObservations returns up to the given limit of unresolved
	// source observations; the service implementation validates a 1–200 limit and
	// requires the optional source-reconciliation extension.
	UnresolvedRevenueObservations(context.Context, int) ([]RevenueObservation, error)
	// FindPaymentRevenueFacts returns the revenue facts associated with a payment
	// in the given revenue scope; the service implementation validates scope and
	// payment identity and requires the optional source-reconciliation extension.
	FindPaymentRevenueFacts(context.Context, RevenueScope, string) ([]RevenueFact, error)
}

// GetRevenueObservation validates the observation ID, requires the optional
// source-reconciliation extension, and forwards the owning read by identity.
func (s *RevenueService) GetRevenueObservation(ctx context.Context, id string) (RevenueObservation, error) {
	if err := revenueContext(ctx); err != nil {
		return RevenueObservation{}, err
	}
	if !validRevenueIdentity(id) {
		return RevenueObservation{}, ErrRevenueInvalid
	}
	repo, ok := s.repo.(RevenueSourceRepository)
	if !ok {
		return RevenueObservation{}, ErrRevenueUnavailable
	}
	return repo.GetRevenueObservation(ctx, id)
}

// UnresolvedRevenueObservations validates the 1–200 limit, requires the
// optional source-reconciliation extension, and forwards the bounded
// unresolved-observation read.
func (s *RevenueService) UnresolvedRevenueObservations(ctx context.Context, limit int) ([]RevenueObservation, error) {
	if err := revenueContext(ctx); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 200 {
		return nil, ErrRevenueInvalid
	}
	repo, ok := s.repo.(RevenueSourceRepository)
	if !ok {
		return nil, ErrRevenueUnavailable
	}
	return repo.UnresolvedRevenueObservations(ctx, limit)
}

// FindPaymentRevenueFacts validates scope and payment identity, requires the
// optional source-reconciliation extension, and forwards the owning fact
// lookup.
func (s *RevenueService) FindPaymentRevenueFacts(ctx context.Context, scope RevenueScope, payment string) ([]RevenueFact, error) {
	if err := revenueContext(ctx); err != nil {
		return nil, err
	}
	if !validRevenueScope(scope) || !validRevenueIdentity(payment) {
		return nil, ErrRevenueInvalid
	}
	repo, ok := s.repo.(RevenueSourceRepository)
	if !ok {
		return nil, ErrRevenueUnavailable
	}
	return repo.FindPaymentRevenueFacts(ctx, scope, payment)
}

// ResolveRevenueRequest is supplied only after authenticated provider lookup
// and current worker/operator authority. ExpectedFingerprint binds the original
// immutable quarantine; no facts means a reasoned no-revenue determination.
type ResolveRevenueRequest struct {
	ObservationID       string
	ExpectedFingerprint string
	RecoveryFingerprint string `json:"-"`
	Facts               []RevenueFact
	Reason              string
	ActorID             string `json:"-"`
}

// ResolveQuarantinedRevenue atomically accepts recovered facts and an immutable
// resolution referencing the original source. Historical quarantine is retained.
// A changed replay conflicts; uncertain commit is reconciled with the same call.
func (s *RevenueService) ResolveQuarantinedRevenue(ctx context.Context, req ResolveRevenueRequest) (RevenueObservation, error) {
	if err := revenueContext(ctx); err != nil {
		return RevenueObservation{}, err
	}
	if !validRevenueIdentity(req.ObservationID) || !validRevenueIdentity(req.ExpectedFingerprint) || !validRevenueIdentity(req.ActorID) || !validRevenueIdentity(req.Reason) || (req.RecoveryFingerprint != "" && !validRevenueIdentity(req.RecoveryFingerprint)) || len(req.Facts) > 200 {
		return RevenueObservation{}, ErrRevenueInvalid
	}
	var result RevenueObservation
	err := s.repo.WithRevenueTransaction(ctx, func(tx RevenueTx) error {
		result = RevenueObservation{}
		original, err := tx.GetObservation(ctx, req.ObservationID)
		if err != nil {
			return err
		}
		if original.QuarantineReason == "" || original.ResolutionOf != "" || original.Fingerprint != req.ExpectedFingerprint {
			return ErrRevenueConflict
		}
		facts := make([]RevenueFact, len(req.Facts))
		seen := map[string]bool{}
		for i, f := range req.Facts {
			if f.Scope != original.Scope {
				return ErrRevenueInvalid
			}
			f, err = canonicalRevenueFact(f)
			if err != nil {
				return err
			}
			if seen[f.ID] {
				return ErrRevenueInvalid
			}
			seen[f.ID] = true
			facts[i] = f
		}
		sort.Slice(facts, func(i, j int) bool { return facts[i].ID < facts[j].ID })
		encoded, err := json.Marshal(struct {
			Original            string
			Fingerprint         string
			Facts               []RevenueFact
			Reason              string
			Actor               string
			RecoveryFingerprint string `json:",omitempty"`
		}{Original: original.ID, Fingerprint: original.Fingerprint, Facts: facts, Reason: req.Reason, Actor: req.ActorID, RecoveryFingerprint: req.RecoveryFingerprint})
		if err != nil {
			return ErrRevenueInvalid
		}
		hash := sha256.Sum256(encoded)
		resolution := RevenueObservation{SourceFingerprint: original.SourceFingerprint, RecoveryFingerprint: req.RecoveryFingerprint, ID: revenueID(original.Scope, "resolution", original.ID, "", "", ""), Scope: original.Scope, EnvelopeID: original.EnvelopeID, Fingerprint: hex.EncodeToString(hash[:]), AcceptedAt: s.clock.Now().UTC(), FactIDs: []string{}, ResolutionOf: original.ID, ResolutionReason: req.Reason, ResolutionBy: req.ActorID}
		for _, f := range facts {
			resolution.FactIDs = append(resolution.FactIDs, f.ID)
		}
		old, err := tx.GetObservation(ctx, resolution.ID)
		if err == nil {
			if old.Fingerprint != resolution.Fingerprint {
				return ErrRevenueConflict
			}
			result = old
			return nil
		}
		if !singleRevenueCause(err, ErrRevenueNotFound) {
			return err
		}
		for _, f := range facts {
			old, err := tx.GetFact(ctx, f.ID)
			if err == nil {
				if old.Fingerprint != f.Fingerprint {
					return ErrRevenueConflict
				}
				continue
			}
			if !singleRevenueCause(err, ErrRevenueNotFound) {
				return err
			}
			f.AcceptedAt = resolution.AcceptedAt
			if _, err := tx.AppendFact(ctx, f); err != nil {
				return err
			}
		}
		if err := tx.InsertObservation(ctx, resolution); err != nil {
			return err
		}
		result = resolution
		return nil
	})
	if err != nil {
		return RevenueObservation{}, err
	}
	return result, nil
}

// Joined absence and outage is an outage, not permission to create new data.
func singleRevenueCause(err, target error) bool {
	for n := 0; err != nil && n < 32; n++ {
		if err == target {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

// GetRevenueSourceResolution reads the immutable owning acceptance for recovery
// before calling a provider again. Absence and outage remain distinct.
func (s *RevenueService) GetRevenueSourceResolution(ctx context.Context, id string) (RevenueObservation, error) {
	original, err := s.GetRevenueObservation(ctx, id)
	if err != nil {
		return RevenueObservation{}, err
	}
	return s.GetRevenueObservation(ctx, revenueID(original.Scope, "resolution", original.ID, "", "", ""))
}

// GetRevenueDelivery reads one owning immutable reception record by its verified
// provider scope and envelope identity, including a durable quarantine receipt.
func (s *RevenueService) GetRevenueDelivery(ctx context.Context, scope RevenueScope, envelope string) (RevenueObservation, error) {
	if !validRevenueScope(scope) || !validRevenueIdentity(envelope) {
		return RevenueObservation{}, ErrRevenueInvalid
	}
	return s.GetRevenueObservation(ctx, revenueID(scope, "delivery", envelope, "", "", ""))
}
