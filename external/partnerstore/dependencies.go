package partnerstore

import "reflect"

// nilStoreDependency detects nil values including nil pointers stored in non-
// nil interfaces.
func nilStoreDependency(v any) bool {
	if v == nil {
		return true
	}
	r := reflect.ValueOf(v)
	switch r.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return r.IsNil()
	}
	return false
}
