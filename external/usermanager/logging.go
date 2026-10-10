package usermanager

import "github.com/ooaklee/ghatd/external/logger"

// safeLogValue delegates to the shared logger's redaction so sensitive values
// are sanitised before structured logging.
func safeLogValue(value any) any {
	return logger.SafeValue(value)
}
