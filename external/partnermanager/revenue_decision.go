package partnermanager

import "errors"

// NoEntitlementError is a conclusive owning-use-case decision based on frozen
// payment terms. Ordinary absence, outages and commercial pauses are not this
// decision. ErrNotFound remains its compatibility classification for callers.
type NoEntitlementError struct{ ReasonCode string }

func (e *NoEntitlementError) Error() string { return "partnermanager/no-entitlement: " + e.ReasonCode }
func (*NoEntitlementError) Unwrap() error   { return ErrNotFound }

func noEntitlementReason(err error) string {
	for n := 0; err != nil && n < 32; n++ {
		if decision, ok := err.(*NoEntitlementError); ok && decision != nil && workCode.MatchString(decision.ReasonCode) {
			return decision.ReasonCode
		}
		err = errors.Unwrap(err)
	}
	return ""
}
