package referral

import (
	"context"
	"errors"
	"reflect"
)

// nilReferralDependency reports whether value is nil or a nil pointer,
// interface, function, map, slice or channel.
func nilReferralDependency(value any) bool {
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

// A joined absence/conflict and outage cannot establish new admission or replay.
func singleReferralCause(err, target error) bool {
	for n := 0; err != nil && n < 32; n++ {
		if err == target {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

// ready rejects nil or cancelled contexts with their error, and a service
// missing its repository, clock or ID generator with ErrUnavailable.
func (s *Service) ready(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || nilReferralDependency(s.repo) || nilReferralDependency(s.clock) || nilReferralDependency(s.ids) {
		return ErrUnavailable
	}
	return nil
}
