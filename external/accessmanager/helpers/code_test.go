package accessmanagerhelpers_test

import (
	"context"
	"errors"
	"fmt"
	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
	"testing"
	"time"
)

// mockCodeStore provides case-owned phase results; it is not an atomic allocator.
type mockCodeStore struct {
	codeExistsFunc func(context.Context, string) (bool, error)
	storeCodeFunc  func(context.Context, string, time.Duration) error
}

func (m *mockCodeStore) CodeExists(ctx context.Context, code string) (bool, error) {
	if m.codeExistsFunc != nil {
		return m.codeExistsFunc(ctx, code)
	}
	return false, nil
}
func (m *mockCodeStore) StoreCode(ctx context.Context, code string, ttl time.Duration) error {
	if m.storeCodeFunc != nil {
		return m.storeCodeFunc(ctx, code, ttl)
	}
	return nil
}
func testCtx() context.Context { return logger.TransitWith(context.Background(), zap.NewNop()) }

func TestGenerateUniqueCode(t *testing.T) {
	native := errors.New("private-store-diagnostic")
	for _, tc := range []struct {
		name                               string
		collisions, samples, reads, writes int
		want                               error
	}{
		{"success", 0, 1, 1, 1, nil}, {"format samples", 0, 20, 20, 20, nil},
		{"collision retry", 2, 1, 3, 1, nil}, {"exhausted", 5, 1, 5, 0, accessmanagerhelpers.ErrCodeGenerationFailure},
		{"lookup failure", 0, 1, 1, 0, native}, {"store failure", 0, 1, 1, 1, native},
		{"nil context", 0, 1, 0, 0, accessmanagerhelpers.ErrCodeGenerationFailure},
		{"nil store", 0, 1, 0, 0, accessmanagerhelpers.ErrCodeGenerationFailure},
		{"typed nil", 0, 1, 0, 0, accessmanagerhelpers.ErrCodeGenerationFailure},
		{"zero ttl", 0, 1, 0, 0, accessmanagerhelpers.ErrCodeGenerationFailure},
		{"canceled", 0, 1, 0, 0, context.Canceled}, {"cancel lookup", 0, 1, 1, 0, context.Canceled}, {"cancel store", 0, 1, 1, 1, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			core, logs := observer.New(zap.DebugLevel)
			ctx, cancel := context.WithCancel(logger.TransitWith(context.Background(), zap.New(core)))
			defer cancel()
			reads, writes := 0, 0
			p := &mockCodeStore{}
			p.codeExistsFunc = func(context.Context, string) (bool, error) {
				reads++
				if tc.name == "lookup failure" {
					return false, native
				}
				if tc.name == "cancel lookup" {
					cancel()
				}
				return reads <= tc.collisions, nil
			}
			p.storeCodeFunc = func(_ context.Context, code string, ttl time.Duration) error {
				writes++
				require.Equal(t, time.Minute, ttl)
				require.Regexp(t, "^[A-Z0-9]{8}$", code)
				if tc.name == "store failure" {
					return native
				}
				if tc.name == "cancel store" {
					cancel()
				}
				return nil
			}
			var store accessmanagerhelpers.CodeStore = p
			ttl := time.Minute
			switch tc.name {
			case "nil context":
				ctx = nil
			case "nil store":
				store = nil
			case "typed nil":
				store = (*mockCodeStore)(nil)
			case "zero ttl":
				ttl = 0
			case "canceled":
				cancel()
			}
			for i := 0; i < tc.samples; i++ {
				code, err := accessmanagerhelpers.GenerateUniqueCode(ctx, store, ttl)
				require.Equal(t, tc.want, err)
				if tc.want == nil {
					require.Regexp(t, "^[A-Z0-9]{8}$", code)
				} else {
					require.Empty(t, code)
				}
			}
			require.Equal(t, tc.reads, reads)
			require.Equal(t, tc.writes, writes)
			for _, entry := range logs.All() {
				require.NotContains(t, fmt.Sprint(entry.Message, entry.ContextMap()), "private-store-diagnostic")
			}
		})
	}
}
