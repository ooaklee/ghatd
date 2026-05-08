package emailmanager

import "errors"

var (
	ErrEmailMailerAuditFailed              = errors.New(ErrKeyEmailMailerAuditFailed)
	ErrEmailMailerProviderUnavailable      = errors.New(ErrKeyEmailMailerProviderUnavailable)
	ErrEmailMailerSendFailed               = errors.New(ErrKeyEmailMailerSendFailed)
	ErrEmailMailerTemplateGenerationFailed = errors.New(ErrKeyEmailMailerTemplateGenerationFailed)
)
