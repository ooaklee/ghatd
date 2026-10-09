package billinglifecyclehelper

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/billing"
	"github.com/ooaklee/ghatd/external/billinglifecycle"
	"github.com/ooaklee/ghatd/external/encryption"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/partnerprogram"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type preparationBindingKey struct{}

type nativePreparationAuthority struct {
	calls  int
	deny   error
	cancel context.CancelFunc
	t      *testing.T
}

func (a *nativePreparationAuthority) AuthorizeLifecyclePreparation(ctx context.Context, actor string, scope *billing.RevenueScope) error {
	a.calls++
	require.Equal(a.t, "worker", actor)
	require.Equal(a.t, "bound", ctx.Value(preparationBindingKey{}))
	deadline, ok := ctx.Deadline()
	require.True(a.t, ok)
	require.LessOrEqual(a.t, time.Until(deadline), 30*time.Second)
	if a.calls == 1 {
		require.Nil(a.t, scope, "all scopes must be checked before any storage write")
	}
	if a.cancel != nil {
		a.cancel()
	}
	return a.deny
}

func nativePreparationConfig() billinglifecycle.PreparationConfig {
	return billinglifecycle.PreparationConfig{ActorID: "worker", Scopes: []billing.RevenueScope{{Provider: "stripe", AccountID: "account"}, {Provider: "stripe", AccountID: "second"}}, PageSize: 2, MaxPages: 30, Timeout: 30 * time.Second}
}

func TestPrepareNativeRejectsBeforeStorageIO(t *testing.T) {
	// The inert database would panic if validation or denied admission ever
	// crossed into storage. Actual additive-write ordering is also checked below.
	for _, tc := range []struct {
		name  string
		want  error
		calls int
	}{
		{"nil_context", billing.ErrRevenueUnavailable, 0}, {"nil_database", billing.ErrRevenueUnavailable, 0},
		{"nil_clock", billing.ErrRevenueUnavailable, 0}, {"typed_nil_clock", billing.ErrRevenueUnavailable, 0},
		{"nil_authority", billing.ErrRevenueUnavailable, 0}, {"typed_nil_authority", billing.ErrRevenueUnavailable, 0},
		{"cancelled", context.Canceled, 0}, {"invalid_actor", billing.ErrRevenueInvalid, 0},
		{"invalid_scope", billing.ErrRevenueInvalid, 0}, {"invalid_budget", billing.ErrRevenueInvalid, 0},
		{"denied", partnermanager.ErrDenied, 1}, {"late_cancel", context.Canceled, 1},
		{"invalid_key", encryption.ErrInvalidKey, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.WithValue(t.Context(), preparationBindingKey{}, "bound"))
			defer cancel()
			var callCtx context.Context = ctx
			db := &mongo.Database{}
			cfg := nativePreparationConfig()
			key := bytes.Repeat([]byte{0x43}, 32)
			var clock billing.RevenueClock = partnerprogram.RealClock{}
			a := &nativePreparationAuthority{t: t}
			var authority billinglifecycle.PreparationAuthority = a
			switch tc.name {
			case "nil_context":
				callCtx = nil
			case "nil_database":
				db = nil
			case "nil_clock":
				clock = nil
			case "typed_nil_clock":
				clock = (*partnerprogram.RealClock)(nil)
			case "nil_authority":
				authority = nil
			case "typed_nil_authority":
				authority = (*nativePreparationAuthority)(nil)
			case "cancelled":
				cancel()
			case "invalid_actor":
				cfg.ActorID = ""
			case "invalid_scope":
				cfg.Scopes[1].AccountID = ""
			case "invalid_budget":
				cfg.MaxPages = 0
			case "denied":
				a.deny = partnermanager.ErrDenied
			case "late_cancel":
				a.cancel = cancel
			case "invalid_key":
				key = nil
			}
			report, err := PrepareNative(callCtx, db, key, clock, authority, cfg)
			require.ErrorIs(t, err, tc.want)
			require.Zero(t, report)
			require.Equal(t, tc.calls, a.calls)
		})
	}
}

func TestPrepareNativeStorageOrderingAndResume(t *testing.T) {
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_URI to an isolated native replica set")
	}
	for _, tc := range []struct {
		name string
		want error
	}{
		{"all_scope_denial", partnermanager.ErrDenied}, {"unknown_authority", errors.New("authority unavailable")},
		{"invalid_key", encryption.ErrInvalidKey}, {"complete_and_repeat", nil}, {"budget_then_resume", billinglifecycle.ErrPreparationBudget},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.WithValue(t.Context(), preparationBindingKey{}, "bound"), time.Minute)
			defer cancel()
			client, err := mongo.Connect(options.Client().ApplyURI(uri))
			require.NoError(t, err)
			db := client.Database(fmt.Sprintf("hostapp_helper_prepare_%d", time.Now().UnixNano()))
			t.Cleanup(func() {
				cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
				defer done()
				require.NoError(t, db.Drop(cleanup))
				require.NoError(t, client.Disconnect(cleanup))
			})
			cfg := nativePreparationConfig()
			a := &nativePreparationAuthority{t: t}
			key := bytes.Repeat([]byte{0x43}, 32)
			switch tc.name {
			case "all_scope_denial", "unknown_authority":
				a.deny = tc.want
			case "invalid_key":
				key = nil
			case "budget_then_resume":
				cfg.MaxPages = 1
			}
			names, err := db.ListCollectionNames(ctx, bson.D{})
			require.NoError(t, err)
			require.Empty(t, names)
			report, err := PrepareNative(ctx, db, key, partnerprogram.RealClock{}, a, cfg)
			if tc.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.want)
			}
			if tc.name == "complete_and_repeat" || tc.name == "budget_then_resume" {
				require.Positive(t, report.Pages)
				require.Equal(t, tc.name == "complete_and_repeat", report.Complete)
				cfg.MaxPages = 30
				replayed, err := PrepareNative(ctx, db, key, partnerprogram.RealClock{}, a, cfg)
				require.NoError(t, err)
				require.True(t, replayed.Complete)
				require.Equal(t, len(cfg.Scopes), replayed.ScopesCompleted)
				if tc.name == "complete_and_repeat" {
					require.Equal(t, len(cfg.Scopes), replayed.Pages)
				}
			} else {
				require.Zero(t, report)
				require.Equal(t, 1, a.calls)
				names, err = db.ListCollectionNames(ctx, bson.D{})
				require.NoError(t, err)
				require.Empty(t, names, "refusal must precede EnsureIndexes and Probe")
			}
		})
	}
}
