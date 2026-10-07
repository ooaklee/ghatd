package referral

import (
	"context"
	"errors"
	"reflect"
)

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
