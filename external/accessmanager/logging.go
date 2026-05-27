package accessmanager

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

func verificationCodeLogFields(code string) []zap.Field {
	trimmed := strings.TrimSpace(code)
	return []zap.Field{
		zap.Bool("code-present", trimmed != ""),
		zap.Int("code-length", len(trimmed)),
	}
}

func requestURLLogFields(value string) []zap.Field {
	trimmed := strings.TrimSpace(value)
	return []zap.Field{
		zap.Bool("request-url-present", trimmed != ""),
		zap.Int("request-url-length", len(trimmed)),
	}
}
