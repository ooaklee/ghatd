package reminder

import "github.com/ooaklee/ghatd/external/logger"

// safeLogValue forwards to logger.SafeValue so log fields are redacted
// consistently with the platform logger.
func safeLogValue(value any) any {
	return logger.SafeValue(value)
}
