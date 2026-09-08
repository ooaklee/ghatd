package observability

import (
	"context"
	"errors"
	"reflect"
	"slices"
)

// OperationOutcome describes the bounded result of an operation.
type OperationOutcome string

const (
	OutcomeSuccess   OperationOutcome = "success"
	OutcomeRejected  OperationOutcome = "rejected"
	OutcomeCancelled OperationOutcome = "cancelled"
	OutcomeTimeout   OperationOutcome = "timeout"
	OutcomeError     OperationOutcome = "error"
	OutcomePanic     OperationOutcome = "panic"
)

// Classification contains only static diagnostic metadata. Code is empty for
// success; Category uses the same finite vocabulary as Outcome.
type Classification struct {
	Code     string
	Outcome  OperationOutcome
	Category string
}

// ErrorRule associates a static sentinel with a diagnostic code and an expected
// rejection or internal failure. Err must be a non-nil, comparable sentinel.
// Code must be a trusted constant, never data obtained from an error or request.
type ErrorRule struct {
	Err     error
	Code    string
	Outcome OperationOutcome
}

// ErrorClassifier is an immutable, ordered registry of error identities.
// It can be shared by operation instruments and logger policies concurrently.
// Its zero value and a nil pointer both provide the default classifications.
type ErrorClassifier struct {
	rules []ErrorRule
}

const maxErrorRules = 128

// NewErrorClassifier validates and snapshots up to 128 rules. Codes contain
// 1–64 ASCII bytes, starting with a letter and continuing with letters, digits,
// dots, underscores, or hyphens. Built-in codes and duplicate sentinel identities
// are reserved. Validation errors never include supplied values.
//
// Wrapped deadlines and cancellations always take precedence over these rules.
// For joined errors matching multiple rules, the first registered rule wins.
// Sentinel implementations must themselves be immutable and safe for errors.Is.
func NewErrorClassifier(rules ...ErrorRule) (*ErrorClassifier, error) {
	if len(rules) > maxErrorRules {
		return nil, errors.New("observability: error classifier exceeds 128 rules")
	}
	seen := make(map[error]struct{}, len(rules))
	for _, rule := range rules {
		if !validErrorSentinel(rule.Err) {
			return nil, errors.New("observability: error rule requires a non-nil comparable sentinel")
		}
		if rule.Err == context.Canceled || rule.Err == context.DeadlineExceeded {
			return nil, errors.New("observability: error rule cannot override a built-in sentinel")
		}
		if _, duplicate := seen[rule.Err]; duplicate {
			return nil, errors.New("observability: error rules contain a duplicate sentinel")
		}
		seen[rule.Err] = struct{}{}
		if !validErrorCode(rule.Code) {
			return nil, errors.New("observability: error rule code must be a 1–64 byte ASCII identifier")
		}
		switch rule.Code {
		case "success", "rejected", "cancelled", "timeout", "error", "internal", "panic":
			return nil, errors.New("observability: error rule code is reserved")
		}
		if rule.Outcome != OutcomeRejected && rule.Outcome != OutcomeError {
			return nil, errors.New("observability: error rule outcome must be rejected or error")
		}
	}
	return &ErrorClassifier{rules: slices.Clone(rules)}, nil
}

// Classify recognizes wrapped and joined errors using errors.Is. It never reads
// error messages, concrete types, or user-defined ErrorType values. A nil error
// is successful even if the surrounding operation's context was cancelled.
func (classifier *ErrorClassifier) Classify(err error) Classification {
	if err == nil {
		return classifyOperation("", OutcomeSuccess)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return classifyOperation("timeout", OutcomeTimeout)
	}
	if errors.Is(err, context.Canceled) {
		return classifyOperation("cancelled", OutcomeCancelled)
	}
	if classifier != nil {
		for _, rule := range classifier.rules {
			if errors.Is(err, rule.Err) {
				return classifyOperation(rule.Code, rule.Outcome)
			}
		}
	}
	return classifyOperation("internal", OutcomeError)
}

func classifyOperation(code string, outcome OperationOutcome) Classification {
	return Classification{Code: code, Outcome: outcome, Category: string(outcome)}
}

func validErrorSentinel(err error) bool {
	if err == nil {
		return false
	}
	value := reflect.ValueOf(err)
	if !value.Comparable() {
		return false
	}
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return !value.IsNil()
	default:
		return true
	}
}

func validErrorCode(code string) bool {
	if len(code) == 0 || len(code) > 64 || !operationIdentifierLetter(code[0]) {
		return false
	}
	for index := 1; index < len(code); index++ {
		char := code[index]
		if !operationIdentifierLetter(char) && !(char >= '0' && char <= '9') && char != '.' && char != '_' && char != '-' {
			return false
		}
	}
	return true
}

func operationIdentifierLetter(char byte) bool {
	return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
}
