package user

import (
	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

func emailLogFields(prefix, value string) []zap.Field {
	return []zap.Field{
		zap.Bool(prefix+"-present", logger.EmailPresentForLog(value)),
		zap.String(prefix+"-domain", logger.EmailDomainForLog(value)),
	}
}
