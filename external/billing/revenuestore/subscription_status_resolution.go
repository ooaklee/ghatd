package revenuestore

import (
	"context"
	"reflect"
	"strings"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
)

// ReadSubscriptionStatusOriginal samples the exact capture and, if conclusively
// absent, joined current head/receipt in ONE encrypted native read. It performs
// no transaction writes and does not interpret failures as absence.
func (r *Repository) ReadSubscriptionStatusOriginal(ctx context.Context, scope billing.RevenueScope, sub, capture string) (billing.SubscriptionStatusOriginalSnapshot, error) {
	if ctx == nil || !statusScopeValid(scope, sub) || capture == "" || strings.TrimSpace(capture) != capture || len(capture) > 256 || strings.ContainsAny(capture, "\r\n\x00") {
		return billing.SubscriptionStatusOriginalSnapshot{}, billing.ErrRevenueInvalid
	}
	if r == nil || r.store == nil {
		return billing.SubscriptionStatusOriginalSnapshot{}, billing.ErrRevenueUnavailable
	}
	var out billing.SubscriptionStatusOriginalSnapshot
	readOK := false
	err := r.store.Read(ctx, func(tx recordstore.Tx) error {
		out = billing.SubscriptionStatusOriginalSnapshot{}
		readOK = false
		if tx == nil || (reflect.ValueOf(tx).Kind() == reflect.Pointer && reflect.ValueOf(tx).IsNil()) {
			return billing.ErrRevenueUnavailable
		}
		b := statusBound(tx, scope, sub)
		receipt, err := b.GetCapture(ctx, capture)
		if err == nil {
			out.CaptureFound = true
			out.Capture = receipt
			readOK = true
			return nil
		}
		if !singleCause(err, billing.ErrRevenueNotFound) {
			return err
		}
		current, err := b.GetCurrent(ctx)
		if err == nil {
			out.CurrentFound = true
			out.Current = current
			readOK = true
			return nil
		}
		if singleCause(err, billing.ErrRevenueNotFound) {
			readOK = true
			return nil
		}
		return err
	})
	if err != nil {
		return billing.SubscriptionStatusOriginalSnapshot{}, mapped(err)
	}
	if !readOK {
		return billing.SubscriptionStatusOriginalSnapshot{}, billing.ErrRevenueUnavailable
	}
	if err = ctx.Err(); err != nil {
		return billing.SubscriptionStatusOriginalSnapshot{}, err
	}
	return out, nil
}

var _ billing.SubscriptionStatusResolutionRepository = (*Repository)(nil)
