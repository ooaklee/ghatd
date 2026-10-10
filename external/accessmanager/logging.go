package accessmanager

import (
	"strings"

	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

// emailLogFields returns only presence and domain fields for an email value,
// never the address itself, keyed by the supplied prefix.
func emailLogFields(prefix, value string) []zap.Field {
	return []zap.Field{
		zap.Bool(prefix+"-present", logger.EmailPresentForLog(value)),
		zap.String(prefix+"-domain", logger.EmailDomainForLog(value)),
	}
}

// verificationCodeLogFields exposes only whether a trimmed code is present and
// its length, never the code value.
func verificationCodeLogFields(code string) []zap.Field {
	trimmed := strings.TrimSpace(code)
	return []zap.Field{
		zap.Bool("code-present", trimmed != ""),
		zap.Int("code-length", len(trimmed)),
	}
}

// requestURLLogFields exposes only presence and length of a trimmed request
// URL, avoiding logging query strings or full URLs.
func requestURLLogFields(value string) []zap.Field {
	trimmed := strings.TrimSpace(value)
	return []zap.Field{
		zap.Bool("request-url-present", trimmed != ""),
		zap.Int("request-url-length", len(trimmed)),
	}
}
