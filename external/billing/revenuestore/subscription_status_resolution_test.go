package revenuestore

import (
	"context"
	"testing"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
)

type resolutionInvalidReadStore struct {
	recordstore.Store
	scenario string
}

func (s resolutionInvalidReadStore) Read(ctx context.Context, fn func(recordstore.Tx) error) error {
	switch s.scenario {
	case "no_callback":
		return nil
	case "nil_tx":
		return fn(nil)
	case "typed_nil_tx":
		var tx *statusResolutionReadTx
		return fn(tx)
	case "suppressed_callback_failure":
		_ = fn(nil)
		return nil
	}
	return recordstore.ErrUnavailable
}
func TestSubscriptionStatusResolutionAdapterReadContract(t *testing.T) {
	for _, name := range []string{"no_callback", "nil_tx", "typed_nil_tx", "suppressed_callback_failure"} {
		t.Run(name, func(t *testing.T) {
			r, err := NewRepository(resolutionInvalidReadStore{scenario: name})
			require.NoError(t, err)
			out, err := r.ReadSubscriptionStatusOriginal(context.Background(), billing.RevenueScope{Provider: "stripe", AccountID: "merchant"}, "subscription", "capture")
			require.ErrorIs(t, err, billing.ErrRevenueUnavailable)
			require.Zero(t, out)
		})
	}
}
