package observability

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrorClassifierUsesIdentityAndWrappedErrorPrecedence(t *testing.T) {
	rejected := errors.New("synthetic-private-error-message")
	internal := errors.New("synthetic-private-dependency-error")
	classifier, err := NewErrorClassifier(
		ErrorRule{Err: rejected, Code: "APP-001", Outcome: OutcomeRejected},
		ErrorRule{Err: internal, Code: "APP-002", Outcome: OutcomeError},
	)
	require.NoError(t, err)
	for _, test := range []struct {
		name string
		err  error
		want Classification
	}{
		{"success", nil, Classification{"", OutcomeSuccess, "success"}},
		{"registered rejection", rejected, Classification{"APP-001", OutcomeRejected, "rejected"}},
		{"wrapped rejection", fmt.Errorf("private wrapper: %w", rejected), Classification{"APP-001", OutcomeRejected, "rejected"}},
		{"registered internal", internal, Classification{"APP-002", OutcomeError, "error"}},
		{"first matching rule", errors.Join(internal, rejected), Classification{"APP-001", OutcomeRejected, "rejected"}},
		{"equal message different identity", errors.New(rejected.Error()), Classification{"internal", OutcomeError, "error"}},
		{"wrapped cancellation", fmt.Errorf("private wrapper: %w", context.Canceled), Classification{"cancelled", OutcomeCancelled, "cancelled"}},
		{"cancellation before rule", errors.Join(rejected, context.Canceled), Classification{"cancelled", OutcomeCancelled, "cancelled"}},
		{"deadline before cancellation and rule", errors.Join(rejected, context.Canceled, context.DeadlineExceeded), Classification{"timeout", OutcomeTimeout, "timeout"}},
		{"no error message or custom type access", &unprintableOperationError{}, Classification{"internal", OutcomeError, "error"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := classifier.Classify(test.err)
			assert.Equal(t, test.want, got)
			assert.NotContains(t, fmt.Sprint(got), "private")
		})
	}
	for _, classifier := range []*ErrorClassifier{nil, {}} {
		assert.Equal(t, Classification{"", OutcomeSuccess, "success"}, classifier.Classify(nil))
		assert.Equal(t, Classification{"cancelled", OutcomeCancelled, "cancelled"}, classifier.Classify(context.Canceled))
		assert.Equal(t, Classification{"timeout", OutcomeTimeout, "timeout"}, classifier.Classify(context.DeadlineExceeded))
		assert.Equal(t, Classification{"internal", OutcomeError, "error"}, classifier.Classify(rejected))
	}
}

// Neither classification nor operation completion may evaluate these methods.
type unprintableOperationError struct{}

func (*unprintableOperationError) Error() string     { panic("error message evaluated") }
func (*unprintableOperationError) ErrorType() string { panic("custom error type evaluated") }

type incomparableOperationError []string

func (incomparableOperationError) Error() string { return "synthetic-private-error-message" }

func TestErrorClassifierRejectsUnsafeOrAmbiguousRules(t *testing.T) {
	sentinel := errors.New("synthetic-private-sentinel")
	valid := ErrorRule{Err: sentinel, Code: "APP-001", Outcome: OutcomeRejected}
	var typedNil *unprintableOperationError
	for _, test := range []struct {
		name  string
		rules []ErrorRule
	}{
		{"nil sentinel", []ErrorRule{{Code: "APP-001", Outcome: OutcomeRejected}}},
		{"typed nil sentinel", []ErrorRule{{Err: typedNil, Code: "APP-001", Outcome: OutcomeRejected}}},
		{"incomparable sentinel", []ErrorRule{{Err: incomparableOperationError{"private"}, Code: "APP-001", Outcome: OutcomeRejected}}},
		{"duplicate sentinel", []ErrorRule{valid, {Err: sentinel, Code: "APP-002", Outcome: OutcomeError}}},
		{"cancel override", []ErrorRule{{Err: context.Canceled, Code: "APP-001", Outcome: OutcomeError}}},
		{"deadline override", []ErrorRule{{Err: context.DeadlineExceeded, Code: "APP-001", Outcome: OutcomeError}}},
		{"empty code", []ErrorRule{{Err: sentinel, Outcome: OutcomeError}}},
		{"long code", []ErrorRule{{Err: sentinel, Code: strings.Repeat("x", 65), Outcome: OutcomeError}}},
		{"control code", []ErrorRule{{Err: sentinel, Code: "synthetic-private-input\n", Outcome: OutcomeError}}},
		{"unicode code", []ErrorRule{{Err: sentinel, Code: "synthetic-private-ſecret", Outcome: OutcomeError}}},
		{"URL code", []ErrorRule{{Err: sentinel, Code: "https://synthetic-private-input", Outcome: OutcomeError}}},
		{"numeric prefix", []ErrorRule{{Err: sentinel, Code: "1CODE", Outcome: OutcomeError}}},
		{"success outcome", []ErrorRule{{Err: sentinel, Code: "APP-001", Outcome: OutcomeSuccess}}},
		{"arbitrary outcome", []ErrorRule{{Err: sentinel, Code: "APP-001", Outcome: "synthetic-private-input"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			classifier, err := NewErrorClassifier(test.rules...)
			require.Error(t, err)
			assert.Nil(t, classifier)
			assert.NotContains(t, err.Error(), "synthetic-private")
		})
	}
	for _, code := range []string{"success", "rejected", "cancelled", "timeout", "error", "internal", "panic"} {
		_, err := NewErrorClassifier(ErrorRule{Err: sentinel, Code: code, Outcome: OutcomeError})
		require.Error(t, err, "reserved code %s", code)
	}
	rules := make([]ErrorRule, 129)
	for index := range rules {
		rules[index] = ErrorRule{Err: errors.New("private"), Code: fmt.Sprintf("APP-%03d", index), Outcome: OutcomeError}
	}
	classifier, err := NewErrorClassifier(rules[:128]...)
	require.NoError(t, err)
	assert.Equal(t, "APP-127", classifier.Classify(rules[127].Err).Code)
	_, err = NewErrorClassifier(rules...)
	require.Error(t, err)
	_, err = NewErrorClassifier(ErrorRule{Err: sentinel, Code: strings.Repeat("X", 64), Outcome: OutcomeError})
	require.NoError(t, err)
}

func TestErrorClassifierSnapshotsRulesAndKeepsInstancesIndependent(t *testing.T) {
	sentinel := errors.New("synthetic-private-sentinel")
	rules := []ErrorRule{{Err: sentinel, Code: "APP-001", Outcome: OutcomeRejected}}
	first, err := NewErrorClassifier(rules...)
	require.NoError(t, err)
	rules[0].Code = "APP-002"
	rules[0].Outcome = OutcomeError
	second, err := NewErrorClassifier(rules...)
	require.NoError(t, err)
	rules[0].Err = errors.New("replacement")
	wantFirst := Classification{"APP-001", OutcomeRejected, "rejected"}
	wantSecond := Classification{"APP-002", OutcomeError, "error"}
	var calls sync.WaitGroup
	for range 32 {
		calls.Go(func() {
			for range 100 {
				assert.Equal(t, wantFirst, first.Classify(sentinel))
				assert.Equal(t, wantSecond, second.Classify(sentinel))
			}
		})
	}
	calls.Wait()
}
