package streaker

import "errors"

var (
	ErrCreatedByUserIdIsRequired  = errors.New(ErrKeyCreatedByUserIdIsRequired)
	ErrCurrentCountCannotBeZero   = errors.New(ErrKeyCurrentCountCannotBeZero)
	ErrDatabaseError              = errors.New(ErrKeyDatabaseError)
	ErrIdIsRequired               = errors.New(ErrKeyIdIsRequired)
	ErrInvalidCurrentCount        = errors.New(ErrKeyInvalidCurrentCount)
	ErrInvalidOccurredAt          = errors.New(ErrKeyInvalidOccurredAt)
	ErrInvalidPeriodType          = errors.New(ErrKeyInvalidPeriodType)
	ErrNanoIdIsRequired           = errors.New(ErrKeyNanoIdIsRequired)
	ErrOwnerIdIsRequired          = errors.New(ErrKeyOwnerIdIsRequired)
	ErrPeriodKeyIsRequired        = errors.New(ErrKeyPeriodKeyIsRequired)
	ErrPeriodTypeIsRequired       = errors.New(ErrKeyPeriodTypeIsRequired)
	ErrPreviousEntryMustBeRelated = errors.New(ErrKeyPreviousEntryMustBeRelated)
	ErrResourceConflict           = errors.New(ErrKeyResourceConflict)
	ErrResourceNotFound           = errors.New(ErrKeyResourceNotFound)
	ErrStreakTypeIsRequired       = errors.New(ErrKeyStreakTypeIsRequired)
	ErrTargetIdIsRequired         = errors.New(ErrKeyTargetIdIsRequired)
	ErrTargetTypeIsRequired       = errors.New(ErrKeyTargetTypeIsRequired)
)
