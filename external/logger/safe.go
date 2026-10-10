package logger

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode"

	"go.uber.org/zap"
)

const (
	maxSafeStringLength = 160
	maxSafeMapKeys      = 24
)

var timeType = reflect.TypeOf(time.Time{})

// SafeAny logs a value after reducing it to non-secret operational fields.
func SafeAny(key string, value any) zap.Field {
	return zap.Any(key, SafeValue(value))
}

// SafeValue reduces request, payload, and model values to fields that are
// useful for diagnostics without logging raw bodies, secrets, or free text.
func SafeValue(value any) any {
	return safeValue(reflect.ValueOf(value), 0)
}

// EmailPresentForLog reports whether an email-like value is present without
// exposing the address itself.
func EmailPresentForLog(value string) bool {
	return strings.TrimSpace(value) != ""
}

// EmailDomainForLog returns the lower-case domain for an email-like value.
// The local part is intentionally omitted because it can identify a person.
func EmailDomainForLog(value string) string {
	parts := strings.Split(strings.TrimSpace(value), "@")
	if len(parts) != 2 {
		return ""
	}
	return strings.ToLower(parts[1])
}

// safeValue reduces an arbitrary reflected value to a loggable form. Times are
// formatted as RFC3339 UTC, strings truncated, scalars passed through, and
// composites summarised or recursed up to depth 2 before falling back to a
// type-only summary. Invalid values return nil.
func safeValue(value reflect.Value, depth int) any {
	value = indirectValue(value)
	if !value.IsValid() {
		return nil
	}

	if value.Type() == timeType {
		return formatTime(value)
	}

	if depth > 2 {
		return typeSummary(value)
	}

	switch value.Kind() {
	case reflect.Struct:
		return safeStructValue(value, depth)
	case reflect.Map:
		return safeMapSummary(value)
	case reflect.Slice, reflect.Array:
		return safeSequenceSummary(value)
	case reflect.String:
		return truncateString(value.String())
	case reflect.Bool:
		return value.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint()
	case reflect.Float32, reflect.Float64:
		return value.Float()
	default:
		if value.CanInterface() {
			return value.Interface()
		}
		return typeSummary(value)
	}
}

// safeStructValue renders a struct as a map keyed by converted field names.
// Exported sensitive fields are reduced to presence summaries, explicitly safe
// fields recurse through safeFieldValue, and other exported fields are omitted
// and counted under omitted-fields. Unexported fields are skipped.
func safeStructValue(value reflect.Value, depth int) map[string]any {
	result := map[string]any{
		"type": shortTypeName(value.Type()),
	}
	omitted := 0

	for i := 0; i < value.NumField(); i++ {
		field := value.Type().Field(i)
		if field.PkgPath != "" {
			continue
		}

		fieldValue := value.Field(i)
		key := fieldLogKey(field.Name)
		switch {
		case isSensitiveFieldName(field.Name):
			addPresenceSummary(result, key, fieldValue)
		case isSafeFieldName(field.Name, fieldValue):
			result[key] = safeFieldValue(field.Name, fieldValue, depth+1)
		default:
			omitted++
		}
	}

	if omitted > 0 {
		result["omitted-fields"] = omitted
	}

	return result
}

// safeFieldValue reduces a single known-safe field value for logging. Times are
// formatted, strings truncated, scalars kept, and maps or sequences summarised
// without contents; structs delegate to safeValue. Unlike safeValue it applies
// no depth cutoff.
func safeFieldValue(fieldName string, value reflect.Value, depth int) any {
	value = indirectValue(value)
	if !value.IsValid() {
		return nil
	}

	if value.Type() == timeType {
		return formatTime(value)
	}

	switch value.Kind() {
	case reflect.Map:
		return safeMapSummary(value)
	case reflect.Slice, reflect.Array:
		return safeSequenceSummary(value)
	case reflect.Struct:
		return safeValue(value, depth)
	case reflect.String:
		return truncateString(value.String())
	case reflect.Bool:
		return value.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint()
	case reflect.Float32, reflect.Float64:
		return value.Float()
	default:
		if value.CanInterface() {
			return value.Interface()
		}
		return typeSummary(value)
	}
}

// safeMapSummary describes a map by type and entry count, plus a sorted,
// truncated list of string keys when present. Map values are never included.
func safeMapSummary(value reflect.Value) map[string]any {
	value = indirectValue(value)
	if !value.IsValid() {
		return nil
	}

	result := map[string]any{
		"type":  value.Type().String(),
		"count": value.Len(),
	}

	keys, truncated := safeMapKeys(value)
	if len(keys) > 0 {
		result["keys"] = keys
	}
	if truncated {
		result["truncated-keys"] = true
	}

	return result
}

// safeSequenceSummary describes a slice or array by type and element count
// only; elements are never included.
func safeSequenceSummary(value reflect.Value) map[string]any {
	value = indirectValue(value)
	if !value.IsValid() {
		return nil
	}

	return map[string]any{
		"type":  value.Type().String(),
		"count": value.Len(),
	}
}

// addPresenceSummary records that a sensitive field exists without its value,
// adding a -present flag plus coarse shape for non-zero values: entry count and
// truncated keys for maps, length for sequences and strings.
func addPresenceSummary(result map[string]any, key string, value reflect.Value) {
	value = indirectValue(value)
	present := value.IsValid() && !value.IsZero()
	result[key+"-present"] = present
	if !present {
		return
	}

	switch value.Kind() {
	case reflect.Map:
		result[key+"-count"] = value.Len()
		keys, truncated := safeMapKeys(value)
		if len(keys) > 0 {
			result[key+"-keys"] = keys
		}
		if truncated {
			result[key+"-truncated-keys"] = true
		}
	case reflect.Slice, reflect.Array:
		result[key+"-count"] = value.Len()
	case reflect.String:
		result[key+"-length"] = len(value.String())
	}
}

// safeMapKeys returns the map's keys sorted and individually truncated. When
// more than maxSafeMapKeys exist, only that many are returned and the second
// result reports truncation.
func safeMapKeys(value reflect.Value) ([]string, bool) {
	keys := make([]string, 0, value.Len())
	for _, key := range value.MapKeys() {
		if key.Kind() == reflect.String {
			keys = append(keys, truncateString(key.String()))
			continue
		}
		if key.CanInterface() {
			keys = append(keys, truncateString(fmt.Sprint(key.Interface())))
			continue
		}
		keys = append(keys, key.Type().String())
	}
	sort.Strings(keys)

	if len(keys) <= maxSafeMapKeys {
		return keys, false
	}

	return keys[:maxSafeMapKeys], true
}

// isSensitiveFieldName reports whether a field name contains a sensitive marker
// such as token, password, body or message, making its value unsuitable for
// logging.
func isSensitiveFieldName(name string) bool {
	normalized := strings.ToLower(name)
	sensitiveParts := []string{
		"authorization",
		"body",
		"content",
		"cookie",
		"data",
		"description",
		"email",
		"endpoint",
		"html",
		"message",
		"metadata",
		"password",
		"payload",
		"privatekey",
		"secret",
		"signature",
		"subject",
		"text",
		"token",
	}

	for _, part := range sensitiveParts {
		if strings.Contains(normalized, part) {
			return true
		}
	}

	return false
}

// isSafeFieldName reports whether a field value may be logged by name or shape:
// identifiers, timestamps, a fixed vocabulary of benign names, count/size/total
// suffixes, booleans, and is/has/can prefixes qualify.
func isSafeFieldName(name string, value reflect.Value) bool {
	normalized := strings.ToLower(name)
	if normalized == "id" || strings.HasSuffix(name, "ID") || strings.HasSuffix(name, "Id") {
		return true
	}

	if strings.HasSuffix(name, "At") || strings.HasSuffix(name, "Date") {
		return true
	}

	safeNames := map[string]struct{}{
		"archived":  {},
		"category":  {},
		"count":     {},
		"currency":  {},
		"enabled":   {},
		"interval":  {},
		"kind":      {},
		"limit":     {},
		"mode":      {},
		"offset":    {},
		"page":      {},
		"pagesize":  {},
		"period":    {},
		"provider":  {},
		"published": {},
		"role":      {},
		"scope":     {},
		"slug":      {},
		"source":    {},
		"state":     {},
		"status":    {},
		"type":      {},
	}
	if _, ok := safeNames[normalized]; ok {
		return true
	}

	if strings.HasSuffix(normalized, "count") || strings.HasSuffix(normalized, "size") || strings.HasSuffix(normalized, "total") {
		return true
	}

	value = indirectValue(value)
	if value.IsValid() && value.Kind() == reflect.Bool {
		return true
	}

	return strings.HasPrefix(normalized, "is") || strings.HasPrefix(normalized, "has") || strings.HasPrefix(normalized, "can")
}

// indirectValue follows interface and pointer indirections to the underlying
// value, returning the zero Value when a nil pointer or interface is reached.
func indirectValue(value reflect.Value) reflect.Value {
	for value.IsValid() && (value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer) {
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}

	return value
}

// formatTime renders a time.Time value as UTC RFC3339, returning an empty
// string for zero times, non-times or values that cannot be interfaced.
func formatTime(value reflect.Value) string {
	if !value.CanInterface() {
		return ""
	}
	t, ok := value.Interface().(time.Time)
	if !ok || t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// shortTypeName returns a type's name qualified by its package's last path
// element, falling back to the full type string for unnamed types.
func shortTypeName(valueType reflect.Type) string {
	valueType = indirectType(valueType)
	if valueType.Name() == "" {
		return valueType.String()
	}

	pkg := valueType.PkgPath()
	if idx := strings.LastIndex(pkg, "/"); idx >= 0 {
		pkg = pkg[idx+1:]
	}
	if pkg == "" {
		return valueType.Name()
	}

	return pkg + "." + valueType.Name()
}

// typeSummary returns a minimal description of a value containing only its full
// type string, used when contents must not be logged. Invalid values return
// nil.
func typeSummary(value reflect.Value) map[string]any {
	value = indirectValue(value)
	if !value.IsValid() {
		return nil
	}

	return map[string]any{
		"type": value.Type().String(),
	}
}

// indirectType strips pointer levels from a type, returning the underlying
// element type.
func indirectType(valueType reflect.Type) reflect.Type {
	for valueType.Kind() == reflect.Pointer {
		valueType = valueType.Elem()
	}
	return valueType
}

// fieldLogKey converts an exported Go field name to a kebab-case log key,
// inserting hyphens at lower-to-upper and upper-run boundaries (e.g. UserID
// becomes user-id).
func fieldLogKey(name string) string {
	runes := []rune(name)
	result := make([]rune, 0, len(runes)+4)
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || nextLower {
				result = append(result, '-')
			}
		}
		result = append(result, unicode.ToLower(r))
	}

	return string(result)
}

// truncateString returns value unchanged when within maxSafeStringLength,
// otherwise the first maxSafeStringLength bytes followed by a truncation
// marker.
func truncateString(value string) string {
	if len(value) <= maxSafeStringLength {
		return value
	}
	return value[:maxSafeStringLength] + "...[truncated]"
}
