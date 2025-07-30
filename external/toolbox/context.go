package toolbox

import (
	"context"
)

// CtxKey is a type alias for string used for context keys.
type CtxKey string

const (
	// CtxKeyCorrelationId is the key used for correlation ID in context.
	CtxKeyCorrelationId CtxKey = "correlation-id"
)

// TransitWithCtxByKey creates a new context with the specified key-value pair, allowing transit of contextual information.
// It takes an existing context, a key string, and a value of any type, and returns a new context with the added value.
func TransitWithCtxByKey[T any](ctx context.Context, key CtxKey, value any) context.Context {
	return context.WithValue(ctx, key, value)
}

// AcquireFromCtxByKey retrieves a value of type T from the context by the given key.
// It returns the retrieved value and a boolean indicating whether the value was successfully retrieved.
// If the value is not found or cannot be type-asserted to T, it returns the zero value of T and false.
func AcquireFromCtxByKey[T any](ctx context.Context, key CtxKey) (T, bool) {
	var retrievedValue T

	value := ctx.Value(key)
	if value == nil {
		return retrievedValue, false
	}

	retrievedValue, ok := value.(T)
	return retrievedValue, ok
}
