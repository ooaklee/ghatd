package billing

import "github.com/ooaklee/ghatd/external/logger"

func safeLogValue(value any) any {
	return logger.SafeValue(value)
}

func emailPresentForLog(value string) bool {
	return logger.EmailPresentForLog(value)
}

func emailDomainForLog(value string) string {
	return logger.EmailDomainForLog(value)
}
