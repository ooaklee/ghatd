package observability

import (
	"errors"
	"reflect"
	"strings"
	"unicode"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const maxLogFieldValues = 32

// LogOption configures a logger's immutable OpenTelemetry field policy.
// Obtain options with WithLogFieldValues or WithLogErrorClassifier; no
// process-global policy is changed.
type LogOption interface {
	applyLogPolicy(*logFieldPolicy)
}

type staticLogFieldValues struct {
	field  string
	values []string
}

type logErrorClassifierOption struct{ classifier *ErrorClassifier }

// WithLogErrorClassifier uses the same static error codes as Operations for
// zap.Error and zap.NamedError fields. The classifier is immutable and may be
// shared. A nil classifier selects the built-in cancellation, timeout, and
// internal-error classifications; omitting this option retains named error
// types for compatibility. The last classifier option wins.
//
// Arbitrary string error.type and error.category fields remain rejected. The
// local Zap output is unchanged, and application log messages must still avoid
// sensitive data. Matching uses errors.Is without calling Error().
func WithLogErrorClassifier(classifier *ErrorClassifier) LogOption {
	return logErrorClassifierOption{classifier: classifier}
}

func (option logErrorClassifierOption) applyLogPolicy(policy *logFieldPolicy) {
	policy.classifier = option.classifier
	policy.classifyErrors = true
}

// WithLogFieldValues permits additional static source or provider values in
// OpenTelemetry logs. Values are case-sensitive and must be trusted constants,
// never values obtained from requests, users, or arbitrary error messages.
//
// Each option accepts at most 32 distinct values, each 1–64 ASCII bytes starting
// with a letter and containing only letters, digits, dots, underscores or
// hyphens. Invalid configuration returns an error that omits the supplied value.
// The option snapshots its input. The last option for a field replaces earlier
// extensions for that field; built-in values always remain available.
func WithLogFieldValues(field string, values ...string) (LogOption, error) {
	if field != "source" && field != "provider" {
		return nil, errors.New("observability: log field extensions support only source and provider")
	}
	seen := make(map[string]struct{}, min(len(values), maxLogFieldValues))
	snapshot := make([]string, 0, min(len(values), maxLogFieldValues))
	for _, value := range values {
		if !validLogFieldValue(value) {
			return nil, errors.New("observability: log field extension must be a 1–64 byte ASCII identifier")
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		if len(seen) == maxLogFieldValues {
			return nil, errors.New("observability: log field extensions exceed 32 distinct values")
		}
		seen[value] = struct{}{}
		snapshot = append(snapshot, value)
	}
	return staticLogFieldValues{field: field, values: snapshot}, nil
}

// validLogFieldValue validates configured identifiers without accepting runtime
// values into the policy merely because they have a valid shape.
func validLogFieldValue(value string) bool {
	if len(value) == 0 || len(value) > 64 || !logValueLetter(value[0]) {
		return false
	}
	for index := 1; index < len(value); index++ {
		char := value[index]
		if !logValueLetter(char) && !(char >= '0' && char <= '9') && char != '.' && char != '_' && char != '-' {
			return false
		}
	}
	return true
}

// logValueLetter reports whether a byte is an ASCII letter.
func logValueLetter(char byte) bool {
	return (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z')
}

// applyLogPolicy replaces this field's extensions with a private copy.
func (option staticLogFieldValues) applyLogPolicy(policy *logFieldPolicy) {
	values := make(map[string]struct{}, len(option.values))
	for _, value := range option.values {
		values[value] = struct{}{}
	}
	switch option.field {
	case "source":
		policy.sources = values
	case "provider":
		policy.providers = values
	}
}

type logFieldPolicy struct {
	sources        map[string]struct{}
	providers      map[string]struct{}
	classifier     *ErrorClassifier
	classifyErrors bool
}

// newLogFieldPolicy builds a private immutable policy for one logger.
func newLogFieldPolicy(options ...LogOption) *logFieldPolicy {
	policy := &logFieldPolicy{}
	for _, option := range options {
		if option != nil {
			option.applyLogPolicy(policy)
		}
	}
	return policy
}

// sanitise retains correlation context and accepted operational fields without
// evaluating custom encoders or Error methods in the OpenTelemetry branch.
func (policy *logFieldPolicy) sanitise(fields []zapcore.Field) []zapcore.Field {
	safe := make([]zapcore.Field, 0, len(fields))
	for _, field := range fields {
		if field.Type == zapcore.ErrorType {
			if err, ok := field.Interface.(error); ok && err != nil {
				if policy.classifyErrors {
					classification := policy.classifier.Classify(err)
					safe = append(safe,
						zap.String("error.type", classification.Code),
						zap.String("error.category", classification.Category))
				} else {
					safe = append(safe, zap.String("error.type", logErrorType(err)))
				}
			}
			continue
		}
		if isTraceContextField(field) {
			safe = append(safe, field)
			continue
		}
		if !isScalarLogField(field.Type) {
			continue
		}
		if accepted, ok := policy.sanitiseField(field); ok {
			safe = append(safe, accepted)
		}
	}
	return safe
}

// sanitiseField canonicalizes selected aliases while preserving unrelated
// existing field names for downstream query compatibility.
func (policy *logFieldPolicy) sanitiseField(field zapcore.Field) (zapcore.Field, bool) {
	key := normaliseLogKey(field.Key)
	switch key {
	case "source":
		_, custom := policy.sources[field.String]
		return zap.String("source", field.String), field.Type == zapcore.StringType && (field.String == "ghatd" || custom)
	case "provider":
		if field.Type != zapcore.StringType {
			return zapcore.Field{}, false
		}
		_, custom := policy.providers[field.String]
		value := field.String
		if !custom && !isBuiltInLogProvider(value) {
			value = "other"
		}
		return zap.String("provider", value), true
	case "method", "httprequestmethod":
		return zap.String("method", HTTPMethodForTelemetry(field.String)), field.Type == zapcore.StringType
	case "status":
		// Existing user/group lifecycle logs use configured domain state strings.
		// Keep that contract; explicit HTTP aliases accept only numeric statuses.
		if field.Type == zapcore.StringType {
			return zap.String("status", field.String), true
		}
		return sanitiseHTTPLogStatus(field)
	case "statuscode", "httpstatuscode", "httpresponsestatuscode":
		return sanitiseHTTPLogStatus(field)
	case "errortype":
		return zap.String("error.type", "panic"), field.Type == zapcore.StringType && field.String == "panic"
	default:
		_, allowed := telemetryLogFieldAllowlist[key]
		return field, allowed
	}
}

// sanitiseHTTPLogStatus accepts the integer status range supported by net/http.
func sanitiseHTTPLogStatus(field zapcore.Field) (zapcore.Field, bool) {
	switch field.Type {
	case zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type,
		zapcore.Uint64Type, zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type, zapcore.UintptrType:
		if field.Integer >= 100 && field.Integer <= 999 {
			return zap.Int64("status", field.Integer), true
		}
	}
	return zapcore.Field{}, false
}

// isBuiltInLogProvider matches the fixed names supplied by GHATD providers.
func isBuiltInLogProvider(value string) bool {
	switch value {
	case "kofi", "lemonsqueezy", "stripe", "SPARKPOST", "LOCAL":
		return true
	default:
		return false
	}
}

// logErrorType retains named concrete types without reflecting anonymous
// fields, struct tags, or generic type arguments into exported logs.
func logErrorType(err error) string {
	concrete := reflect.TypeOf(err)
	if concrete == nil {
		return "error"
	}
	named := concrete
	for named.Kind() == reflect.Pointer {
		named = named.Elem()
	}
	if named.Name() == "" {
		return "error"
	}
	name, _, _ := strings.Cut(concrete.String(), "[")
	return name
}

// normaliseLogKey recognizes existing punctuation and case aliases.
func normaliseLogKey(key string) string {
	return strings.Map(func(char rune) rune {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			return unicode.ToLower(char)
		}
		return -1
	}, key)
}

// isScalarLogField excludes nested, custom-marshalled, namespace, and skip
// fields. The private trace-context carrier is handled before this filter.
func isScalarLogField(fieldType zapcore.FieldType) bool {
	switch fieldType {
	case zapcore.BoolType, zapcore.DurationType, zapcore.Float64Type, zapcore.Float32Type,
		zapcore.Int64Type, zapcore.Int32Type, zapcore.Int16Type, zapcore.Int8Type,
		zapcore.StringType, zapcore.TimeType, zapcore.TimeFullType,
		zapcore.Uint64Type, zapcore.Uint32Type, zapcore.Uint16Type, zapcore.Uint8Type, zapcore.UintptrType:
		return true
	default:
		return false
	}
}
