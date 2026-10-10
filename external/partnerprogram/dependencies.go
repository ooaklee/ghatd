package partnerprogram

import (
	"context"
	"errors"
	"reflect"
)

// nilProgramDependency detects nil values including nil pointers stored in non-
// nil interfaces.
func nilProgramDependency(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Func, reflect.Map, reflect.Slice, reflect.Chan:
		return v.IsNil()
	}
	return false
}

// singleProgramCause reports whether target appears within 32 unwrap levels of
// err.
func singleProgramCause(err, target error) bool {
	for n := 0; err != nil && n < 32; n++ {
		if err == target {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

// ready rejects a nil or expired context and a service with nil repository,
// clock or ID generator.
func (s *Service) ready(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || nilProgramDependency(s.repo) || nilProgramDependency(s.clock) || nilProgramDependency(s.ids) {
		return ErrUnavailable
	}
	return nil
}
