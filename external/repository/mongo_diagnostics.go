package repository

import (
	"context"
	"github.com/ooaklee/ghatd/external/observability"
)

type mongoErrorClassifierKey struct{}

// WithMongoErrorClassifier attaches an immutable owner-supplied sentinel registry
// to Mongo telemetry. Codes must be trusted static constants, never request or
// error text. error_identity is a registry match, not proof of harmlessness:
// joined errors may match a sentinel alongside another failure. Classification
// changes neither severity, outcome, returned errors nor transaction behavior.
// A nil context is retained for the caller's existing validation.
func WithMongoErrorClassifier(ctx context.Context, classifier *observability.ErrorClassifier) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(ctx, mongoErrorClassifierKey{}, classifier)
}
