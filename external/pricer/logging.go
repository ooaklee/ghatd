package pricer

import "github.com/ooaklee/ghatd/external/logger"

// safeLogValue forwards a value to the shared logger redaction wrapper.
func safeLogValue(value any) any {
	return logger.SafeValue(value)
}
