package notifier

import "errors"

var (
	ErrDatabaseError                  = errors.New(ErrKeyDatabaseError)
	ErrInvalidNotificationAddressBody = errors.New(ErrKeyInvalidNotificationAddressBody)
	ErrInvalidNotificationChannel     = errors.New(ErrKeyInvalidNotificationChannel)
	ErrInvalidNotificationPreferences = errors.New(ErrKeyInvalidNotificationPreferences)
	ErrNotificationAddressNotFound    = errors.New(ErrKeyNotificationAddressNotFound)
	ErrNotificationNoActiveAddresses  = errors.New(ErrKeyNotificationNoActiveAddresses)
	ErrNotificationSenderNotEnabled   = errors.New(ErrKeyNotificationSenderNotEnabled)
	ErrNotificationSendFailed         = errors.New(ErrKeyNotificationSendFailed)
	ErrNotificationUserIDRequired     = errors.New(ErrKeyNotificationUserIDRequired)
)
