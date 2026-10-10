package group

import (
	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

// safeLogValue delegates redaction of a value to the shared logger package.
func safeLogValue(value any) any {
	return logger.SafeValue(value)
}

// emailLogFields logs only the presence and domain of an email address, never
// the full value, using the given field prefix.
func emailLogFields(prefix, value string) []zap.Field {
	return []zap.Field{
		zap.Bool(prefix+"-present", logger.EmailPresentForLog(value)),
		zap.String(prefix+"-domain", logger.EmailDomainForLog(value)),
	}
}
