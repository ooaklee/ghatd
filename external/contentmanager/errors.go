package contentmanager

import "errors"

var (
	ErrUnauthorisedCMUser = errors.New(ErrKeyUnauthorisedCMUser)
	// ErrContentManagerUnavailable denotes absent or inconsistent trusted dependencies.
	ErrContentManagerUnavailable = errors.New(ErrKeyContentManagerUnavailable)
)
