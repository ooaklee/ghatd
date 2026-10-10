package partnermanager

import "errors"

// NoEntitlementError is a conclusive owning-use-case decision based on frozen
// payment terms. Ordinary absence, outages and commercial pauses are not this
// decision. ErrNotFound remains its compatibility classification for callers.
type NoEntitlementError struct{ ReasonCode string }

// Error renders the decision with the package-prefixed reason code for
// diagnostics.
func (e *NoEntitlementError) Error() string { return "partnermanager/no-entitlement: " + e.ReasonCode }

// Unwrap returns ErrNotFound so the conclusive no-entitlement decision keeps
// its compatibility classification as absence.
func (*NoEntitlementError) Unwrap() error { return ErrNotFound }

// noEntitlementReason searches up to 32 unwrap levels for a NoEntitlementError
// with a well-formed reason code and returns it, or "" if none is found.
func noEntitlementReason(err error) string {
	for n := 0; err != nil && n < 32; n++ {
		if decision, ok := err.(*NoEntitlementError); ok && decision != nil && workCode.MatchString(decision.ReasonCode) {
			return decision.ReasonCode
		}
		err = errors.Unwrap(err)
	}
	return ""
}
