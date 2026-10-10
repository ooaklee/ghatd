package partnerstore

import (
	"context"
	"errors"

	"github.com/ooaklee/ghatd/external/referral"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

// ReadConversionSnapshot adds complete immutable bindings to owning traffic
// evidence in ONE native read. It changes no analytics schema/revision input.
// Shared pagination and binding walkers keep scope, expiry and combined capacity
// checks consistent. Callback reentry resets the whole result and budget.
func (r *ReferralRepository) ReadConversionSnapshot(ctx context.Context, program, partner string, q referral.AnalyticsQuery) (referral.ConversionSnapshot, error) {
	if err := validStoreContext(ctx); err != nil {
		return referral.ConversionSnapshot{}, referralError(err)
	}
	if r == nil || nilStoreDependency(r.store) {
		return referral.ConversionSnapshot{}, referral.ErrUnavailable
	}
	if program != referral.ProgramID || partner == "" || len(partner) > 256 || q.Validate() != nil {
		return referral.ConversionSnapshot{}, referral.ErrInvalid
	}
	var out referral.ConversionSnapshot
	completed := false
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out, completed = referral.ConversionSnapshot{}, false
		if nilStoreDependency(tx) {
			return recordstore.ErrUnavailable
		}
		budget := referral.RelationshipEvidenceCapacity
		var err error
		out.Analytics, err = readAnalyticsEvidence(ctx, tx, program, partner, q, &budget, true)
		if err != nil {
			return err
		}
		out.Bindings = map[string][]referral.PaymentAttribution{}
		for _, member := range out.Analytics.Relationships {
			if _, duplicate := out.Bindings[member.ReferredCustomer]; duplicate {
				return recordstore.ErrUnavailable
			}
			bindings, err := readCustomerBindingEvidence(ctx, tx, program, member.ReferredCustomer, &budget)
			if err != nil {
				return err
			}
			out.Bindings[member.ReferredCustomer] = bindings
		}
		completed = true
		return ctx.Err()
	})
	if err != nil {
		if errors.Is(err, referral.ErrCapacity) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return referral.ConversionSnapshot{}, err
		}
		return referral.ConversionSnapshot{}, errors.Join(referral.ErrUnavailable, referralError(err))
	}
	if !completed {
		return referral.ConversionSnapshot{}, referral.ErrUnavailable
	}
	if err := ctx.Err(); err != nil {
		return referral.ConversionSnapshot{}, err
	}
	return out, nil
}
