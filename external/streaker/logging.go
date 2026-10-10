package streaker

import "github.com/ooaklee/ghatd/external/logger"

// safeLogValue forwards a value to the shared logger's redaction wrapper so
// callers consistently sanitise sensitive fields before logging.
func safeLogValue(value any) any {
	return logger.SafeValue(value)
}
