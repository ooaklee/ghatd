package group

import "github.com/ooaklee/ghatd/external/logger"

func safeLogValue(value any) any {
	return logger.SafeValue(value)
}
