package emailmanager

import (
	"strings"

	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

func emailLogFields(prefix, value string) []zap.Field {
	return []zap.Field{
		zap.Bool(prefix+"-present", logger.EmailPresentForLog(value)),
		zap.String(prefix+"-domain", logger.EmailDomainForLog(value)),
	}
}

func subjectLogFields(value string) []zap.Field {
	trimmed := strings.TrimSpace(value)
	return []zap.Field{
		zap.Bool("subject-present", trimmed != ""),
		zap.Int("subject-length", len(trimmed)),
	}
}

func outboundEmailLogFields(provider, messageID, to, from, subject string) []zap.Field {
	fields := []zap.Field{
		zap.String("provider", provider),
	}
	if messageID != "" {
		fields = append(fields, zap.String("message_id", messageID))
	}
	fields = append(fields, emailLogFields("recipient", to)...)
	fields = append(fields, emailLogFields("sender", from)...)
	fields = append(fields, subjectLogFields(subject)...)
	return fields
}
