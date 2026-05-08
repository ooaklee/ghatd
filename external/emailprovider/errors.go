package emailprovider

import "errors"

var (
	ErrEmailProviderInvalidEmail     = errors.New(ErrKeyEmailProviderInvalidEmail)
	ErrEmailProviderMissingBody      = errors.New(ErrKeyEmailProviderMissingBody)
	ErrEmailProviderMissingFrom      = errors.New(ErrKeyEmailProviderMissingFrom)
	ErrEmailProviderMissingRecipient = errors.New(ErrKeyEmailProviderMissingRecipient)
	ErrEmailProviderMissingSubject   = errors.New(ErrKeyEmailProviderMissingSubject)
	ErrEmailProviderSendFailed       = errors.New(ErrKeyEmailProviderSendFailed)
	ErrEmailProviderUnavailable      = errors.New(ErrKeyEmailProviderUnavailable)
)
