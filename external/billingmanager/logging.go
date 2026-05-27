package billingmanager

import "github.com/ooaklee/ghatd/external/logger"

func emailPresentForLog(value string) bool {
	return logger.EmailPresentForLog(value)
}

func emailDomainForLog(value string) string {
	return logger.EmailDomainForLog(value)
}
