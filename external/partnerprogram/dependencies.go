package partnerprogram

import (
	"context"
	"errors"
	"reflect"
)

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

func singleProgramCause(err, target error) bool {
	for n := 0; err != nil && n < 32; n++ {
		if err == target {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

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
