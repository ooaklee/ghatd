package emailprovider

import (
	"strings"

	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

func emailLogFields(provider string, email *Email) []zap.Field {
	fields := []zap.Field{
		zap.String("provider", provider),
	}
	if email == nil {
		return append(fields, zap.Bool("email-present", false))
	}

	return append(fields,
		zap.Bool("email-present", true),
		zap.String("recipient-domain", logger.EmailDomainForLog(email.To)),
		zap.String("sender-domain", logger.EmailDomainForLog(email.From)),
		zap.Bool("has-reply-to", strings.TrimSpace(email.ReplyTo) != ""),
		zap.Bool("has-html-body", strings.TrimSpace(email.HTMLBody) != ""),
		zap.Bool("has-text-body", strings.TrimSpace(email.TextBody) != ""),
		zap.Int("subject-length", len(strings.TrimSpace(email.Subject))),
	)
}
