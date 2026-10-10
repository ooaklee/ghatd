package billingmanager

import "github.com/ooaklee/ghatd/external/logger"

// emailPresentForLog reports whether an email value is populated enough to log,
// forwarding to the shared logger policy.
func emailPresentForLog(value string) bool {
	return logger.EmailPresentForLog(value)
}

// emailDomainForLog extracts the loggable domain portion of an email address
// under the shared logger policy.
func emailDomainForLog(value string) string {
	return logger.EmailDomainForLog(value)
}
