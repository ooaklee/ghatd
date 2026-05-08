package oauth

import "errors"

var (
	ErrProviderCodeExchangeIncorrect    = errors.New(ErrKeyProviderCodeExchangeIncorrect)
	ErrProviderCodeNotDetected          = errors.New(ErrKeyProviderCodeNotDetected)
	ErrProviderFailedGettingUserInfo    = errors.New(ErrKeyProviderFailedGettingUserInfo)
	ErrProviderFailedToMarshallUserInfo = errors.New(ErrKeyProviderFailedToMarshallUserInfo)
)
