package group

import (
	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

func safeLogValue(value any) any {
	return logger.SafeValue(value)
}

func emailLogFields(prefix, value string) []zap.Field {
	return []zap.Field{
		zap.Bool(prefix+"-present", logger.EmailPresentForLog(value)),
		zap.String(prefix+"-domain", logger.EmailDomainForLog(value)),
	}
}
