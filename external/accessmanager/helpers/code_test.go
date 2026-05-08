package accessmanagerhelpers_test

import (
	"context"
	"errors"
	"testing"
	"time"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

// mockCodeStore implements the CodeStore interface for testing
type mockCodeStore struct {
	codeExistsFunc func(ctx context.Context, code string) (bool, error)
	storeCodeFunc  func(ctx context.Context, code string, ttl time.Duration) error
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

// testCtx returns a context with a no-op logger attached
func testCtx() context.Context {
	return logger.TransitWith(context.Background(), zap.NewNop())
}

func TestGenerateUniqueCode_Success(t *testing.T) {
	t.Parallel()

	ctx := testCtx()
	store := &mockCodeStore{
		codeExistsFunc: func(ctx context.Context, code string) (bool, error) {
			return false, nil
		},
		storeCodeFunc: func(ctx context.Context, code string, ttl time.Duration) error {
			return nil
		},
	}

	code, err := accessmanagerhelpers.GenerateUniqueCode(ctx, store, 10*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(code) != 8 {
		t.Errorf("expected code length 8, got %d: %q", len(code), code)
	}

	for _, c := range code {
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			t.Errorf("code contains invalid character: %c in %q", c, code)
		}
	}
}

func TestGenerateUniqueCode_CollisionRetry(t *testing.T) {
	t.Parallel()

	ctx := testCtx()

	callCount := 0
	store := &mockCodeStore{
		codeExistsFunc: func(ctx context.Context, code string) (bool, error) {
			callCount++
			if callCount <= 2 {
				return true, nil
			}
			return false, nil
		},
		storeCodeFunc: func(ctx context.Context, code string, ttl time.Duration) error {
			return nil
		},
	}

	code, err := accessmanagerhelpers.GenerateUniqueCode(ctx, store, 10*time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(code) != 8 {
		t.Errorf("expected code length 8, got %d: %q", len(code), code)
	}

	if callCount != 3 {
		t.Errorf("expected 3 CodeExists calls (2 collisions + 1 success), got %d", callCount)
	}
}

func TestGenerateUniqueCode_MaxRetriesExceeded(t *testing.T) {
	t.Parallel()

	ctx := testCtx()
	store := &mockCodeStore{
		codeExistsFunc: func(ctx context.Context, code string) (bool, error) {
			return true, nil
		},
	}

	code, err := accessmanagerhelpers.GenerateUniqueCode(ctx, store, 10*time.Minute)
	if err == nil {
		t.Fatal("expected error when max retries exceeded, got nil")
	}
	if !errors.Is(err, accessmanagerhelpers.ErrCodeGenerationFailure) {
		t.Errorf("expected error %q, got %q", accessmanagerhelpers.ErrCodeGenerationFailure, err.Error())
	}
	if code != "" {
		t.Errorf("expected empty code on failure, got %q", code)
	}
}

func TestGenerateUniqueCode_CodeExistsError(t *testing.T) {
	t.Parallel()

	ctx := testCtx()
	store := &mockCodeStore{
		codeExistsFunc: func(ctx context.Context, code string) (bool, error) {
			return false, errors.New("redis unavailable")
		},
	}

	code, err := accessmanagerhelpers.GenerateUniqueCode(ctx, store, 10*time.Minute)
	if err == nil {
		t.Fatal("expected error when CodeExists fails, got nil")
	}
	if !errors.Is(err, accessmanagerhelpers.ErrCodeGenerationFailure) {
		t.Errorf("expected error %q, got %q", accessmanagerhelpers.ErrCodeGenerationFailure, err.Error())
	}
	if code != "" {
		t.Errorf("expected empty code on failure, got %q", code)
	}
}

func TestGenerateUniqueCode_StoreCodeError(t *testing.T) {
	t.Parallel()

	ctx := testCtx()
	store := &mockCodeStore{
		codeExistsFunc: func(ctx context.Context, code string) (bool, error) {
			return false, nil
		},
		storeCodeFunc: func(ctx context.Context, code string, ttl time.Duration) error {
			return errors.New("redis unavailable")
		},
	}

	code, err := accessmanagerhelpers.GenerateUniqueCode(ctx, store, 10*time.Minute)
	if err == nil {
		t.Fatal("expected error when StoreCode fails, got nil")
	}
	if !errors.Is(err, accessmanagerhelpers.ErrCodeGenerationFailure) {
		t.Errorf("expected error %q, got %q", accessmanagerhelpers.ErrCodeGenerationFailure, err.Error())
	}
	if code != "" {
		t.Errorf("expected empty code on failure, got %q", code)
	}
}

func TestGenerateUniqueCode_FormatValidation(t *testing.T) {
	t.Parallel()

	ctx := testCtx()
	store := &mockCodeStore{
		codeExistsFunc: func(ctx context.Context, code string) (bool, error) {
			return false, nil
		},
		storeCodeFunc: func(ctx context.Context, code string, ttl time.Duration) error {
			return nil
		},
	}

	for i := 0; i < 20; i++ {
		code, err := accessmanagerhelpers.GenerateUniqueCode(ctx, store, 10*time.Minute)
		if err != nil {
			t.Fatalf("unexpected error on iteration %d: %v", i, err)
		}
		if len(code) != 8 {
			t.Errorf("iteration %d: expected length 8, got %d: %q", i, len(code), code)
		}
		for _, c := range code {
			if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
				t.Errorf("iteration %d: invalid char %c in code %q", i, c, code)
			}
		}
	}
}
