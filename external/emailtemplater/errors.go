package emailtemplater

import "errors"

var (
	ErrEmailTemplaterDynamicTemplateNotFound = errors.New(ErrKeyEmailTemplaterDynamicTemplateNotFound)
	ErrEmailTemplaterMissingBody             = errors.New(ErrKeyEmailTemplaterMissingBody)
	ErrEmailTemplaterMissingPersonalInfo     = errors.New(ErrKeyEmailTemplaterMissingPersonalInfo)
	ErrEmailTemplaterMissingRecipient        = errors.New(ErrKeyEmailTemplaterMissingRecipient)
	ErrEmailTemplaterMissingSubject          = errors.New(ErrKeyEmailTemplaterMissingSubject)
	ErrEmailTemplaterMissingToken            = errors.New(ErrKeyEmailTemplaterMissingToken)
	ErrEmailTemplaterNoConfigProvided        = errors.New(ErrKeyEmailTemplaterNoConfigProvided)
	ErrEmailTemplaterTemplateNotFound        = errors.New(ErrKeyEmailTemplaterTemplateNotFound)
	ErrEmailTemplaterTemplateRenderingFailed = errors.New(ErrKeyEmailTemplaterTemplateRenderingFailed)
)
