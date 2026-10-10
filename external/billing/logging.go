package billing

import "github.com/ooaklee/ghatd/external/logger"

// safeLogValue forwards to the shared logger redaction helper for safe
// structured values.
func safeLogValue(value any) any {
	return logger.SafeValue(value)
}

// emailPresentForLog reports whether an email value is present without exposing
// the address.
func emailPresentForLog(value string) bool {
	return logger.EmailPresentForLog(value)
}

// emailDomainForLog returns only the email domain for logging.
func emailDomainForLog(value string) string {
	return logger.EmailDomainForLog(value)
}
