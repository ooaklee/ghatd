package user

import (
	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

// emailLogFields builds zap fields keyed by prefix that expose only an email's
// presence flag and domain, never the address itself.
func emailLogFields(prefix, value string) []zap.Field {
	return []zap.Field{
		zap.Bool(prefix+"-present", logger.EmailPresentForLog(value)),
		zap.String(prefix+"-domain", logger.EmailDomainForLog(value)),
	}
}
