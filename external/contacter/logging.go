package contacter

import "github.com/ooaklee/ghatd/external/logger"

// safeLogValue forwards to the shared logger's privacy policy for loggable
// values.
func safeLogValue(value any) any {
	return logger.SafeValue(value)
}
