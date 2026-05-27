package emailmanager

import (
	"strings"

	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

// emailLogFields returns safe email metadata fields using the supplied prefix.
// It records address presence and domain without logging the full address.
func emailLogFields(prefix, value string) []zap.Field {
	return []zap.Field{
		zap.Bool(prefix+"-present", logger.EmailPresentForLog(value)),
		zap.String(prefix+"-domain", logger.EmailDomainForLog(value)),
	}
}

// subjectLogFields returns safe subject metadata without logging subject text.
func subjectLogFields(value string) []zap.Field {
	trimmed := strings.TrimSpace(value)
	return []zap.Field{
		zap.Bool("subject-present", trimmed != ""),
		zap.Int("subject-length", len(trimmed)),
	}
}

// outboundEmailLogFields returns the standard safe field set for outbound email logs.
// It includes provider, optional message ID, recipient/sender domains, and subject metadata.
func outboundEmailLogFields(provider, messageID, to, from, subject string) []zap.Field {
	fields := []zap.Field{
		zap.String("provider", provider),
	}
	if messageID != "" {
		fields = append(fields, zap.String("message-id", messageID))
	}
	fields = append(fields, emailLogFields("recipient", to)...)
	fields = append(fields, emailLogFields("sender", from)...)
	fields = append(fields, subjectLogFields(subject)...)
	return fields
}
