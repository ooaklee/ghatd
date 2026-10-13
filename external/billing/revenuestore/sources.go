package revenuestore

import (
	"context"
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"go.uber.org/zap"
	"sort"
)

// GetRevenueObservation reads one observation in a single snapshot read.
// Missing or corrupt records surface as mapped billing errors, never an empty
// observation with success.
func (r *Repository) GetRevenueObservation(ctx context.Context, id string) (billing.RevenueObservation, error) {
	var result billing.RevenueObservation
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		result = billing.RevenueObservation{}
		v, err := (&bound{tx}).GetObservation(ctx, id)
		if err != nil {
			return err
		}
		result = v
		return nil
	})
	if err != nil {
		return billing.RevenueObservation{}, mapped(err)
	}
	return result, nil
}

// GetRevenueSourceResolution reads the required original and its optional
// resolution together. Expected resolution absence finishes the transaction
// normally; no record is created, and read/commit failures take precedence.
func (r *Repository) GetRevenueSourceResolution(ctx context.Context, id string) (billing.RevenueObservation, error) {
	if ctx != nil {
		ctx = logger.WithPackage(ctx, "external/billing/revenuestore", zap.String("lookup_site", "billing.revenue_source.resolution"))
	}
	var result billing.RevenueObservation
	absent := false
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		// A repeated callback must publish only its final snapshot.
		result = billing.RevenueObservation{}
		absent = false
		reader := &bound{tx}
		original, err := reader.GetObservation(ctx, id)
		if err != nil {
			// The original is required; its absence is never normalized.
			return err
		}
		result, err = reader.GetObservation(ctx, original.ResolutionID())
		if singleCause(err, billing.ErrRevenueNotFound) {
			absent = true
			return nil
		}
		return err
	})
	if err != nil {
		return billing.RevenueObservation{}, mapped(err)
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return billing.RevenueObservation{}, err
		}
	}
	if absent {
		return billing.RevenueObservation{}, billing.ErrRevenueNotFound
	}
	return result, nil
}

// UnresolvedRevenueObservations returns up to limit quarantined observations
// not referenced by any resolution, ordered by AcceptedAt then ID.
func (r *Repository) UnresolvedRevenueObservations(ctx context.Context, limit int) ([]billing.RevenueObservation, error) {
	return r.unresolvedRevenueObservations(ctx, "", limit, false)
}

// UnresolvedRevenueObservationsAfter returns up to lexically paginated
// unresolved observations whose ID is strictly after afterID, ignoring
// AcceptedAt ordering.
func (r *Repository) UnresolvedRevenueObservationsAfter(ctx context.Context, afterID string, limit int) ([]billing.RevenueObservation, error) {
	return r.unresolvedRevenueObservations(ctx, afterID, limit, true)
}

// unresolvedRevenueObservations scans all observations in one read, excluding
// resolved ones. Valid limit is 1..200; ordering is AcceptedAt/ID unless
// lexical, which also filters by afterID. Decode failures abort with no partial
// results.
func (r *Repository) unresolvedRevenueObservations(ctx context.Context, afterID string, limit int, lexical bool) ([]billing.RevenueObservation, error) {
	if ctx == nil || limit < 1 || limit > 200 {
		return nil, billing.ErrRevenueInvalid
	}
	var result []billing.RevenueObservation
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		result = nil
		rows, err := recordstore.FindAll(ctx, tx, recordstore.Query{Kind: kindObservation, Partition: partition})
		if err != nil {
			return mapped(err)
		}
		resolutions := map[string]bool{}
		var pending []billing.RevenueObservation
		for _, row := range rows {
			v, err := decodeObservation(row)
			if err != nil {
				return err
			}
			if v.ResolutionOf != "" {
				resolutions[v.ResolutionOf] = true
			} else if v.QuarantineReason != "" {
				pending = append(pending, v)
			}
		}
		sort.Slice(pending, func(i, j int) bool {
			if lexical || pending[i].AcceptedAt.Equal(pending[j].AcceptedAt) {
				return pending[i].ID < pending[j].ID
			}
			return pending[i].AcceptedAt.Before(pending[j].AcceptedAt)
		})
		for _, v := range pending {
			if !resolutions[v.ID] && (!lexical || v.ID > afterID) {
				result = append(result, v)
				if len(result) == limit {
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, mapped(err)
	}
	return result, nil
}

// FindPaymentRevenueFacts reads retained payment facts for an exact scope and
// payment ID in one snapshot, ordered by sequence. Invalid selectors and
// corrupt or unreadable records return errors rather than partial results.
func (r *Repository) FindPaymentRevenueFacts(ctx context.Context, scope billing.RevenueScope, payment string) ([]billing.RevenueFact, error) {
	if ctx == nil || payment == "" {
		return nil, billing.ErrRevenueInvalid
	}
	var result []billing.RevenueFact
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		result = nil
		rows, err := recordstore.FindAll(ctx, tx, recordstore.Query{Kind: kindFact, Partition: partition})
		if err != nil {
			return mapped(err)
		}
		for _, row := range rows {
			v, err := decodeFact(row)
			if err != nil {
				return err
			}
			if v.Scope == scope && v.PaymentID == payment && v.Kind == billing.RevenuePayment {
				result = append(result, v)
			}
		}
		sort.Slice(result, func(i, j int) bool { return result[i].Sequence < result[j].Sequence })
		return nil
	})
	if err != nil {
		return nil, mapped(err)
	}
	return result, nil
}

var _ billing.RevenuePagingRepository = (*Repository)(nil)
var _ billing.RevenueSourceResolutionRepository = (*Repository)(nil)
