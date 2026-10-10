package notifier

import "github.com/ooaklee/ghatd/external/logger"

// safeLogValue forwards to logger.SafeValue so notifier logging reduces values
// to non-secret fields.
func safeLogValue(value any) any {
	return logger.SafeValue(value)
}
