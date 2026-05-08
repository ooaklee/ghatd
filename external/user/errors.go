package user

import "errors"

var (
	ErrInvalidQueryParam       = errors.New(ErrKeyInvalidQueryParam)
	ErrInvalidUserBody         = errors.New(ErrKeyInvalidUserBody)
	ErrInvalidUserID           = errors.New(ErrKeyInvalidUserID)
	ErrInvalidUserOriginStatus = errors.New(ErrKeyInvalidUserOriginStatus)
	ErrNoChangesDetected       = errors.New(ErrKeyNoChangesDetected)
	ErrPageOutOfRange          = errors.New(ErrKeyPageOutOfRange)
	ErrResourceConflict        = errors.New(ErrKeyResourceConflict)
	ErrResourceNotFound        = errors.New(ErrKeyResourceNotFound)
	ErrUserNeverActivated      = errors.New(ErrKeyUserNeverActivated)
)
