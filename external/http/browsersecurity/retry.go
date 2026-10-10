package browsersecurity

import (
	"strconv"
	"time"
)

// RetrySeconds rounds a duration up for a Retry-After header, with a one-second
// minimum. Division before rounding avoids overflowing a maximal duration.
func RetrySeconds(duration time.Duration) string {
	seconds := int64(duration / time.Second)
	if duration%time.Second > 0 {
		seconds++
	}
	if seconds < 1 {
		seconds = 1
	}
	return strconv.FormatInt(seconds, 10)
}
