package timezonecoder

import (
	"sort"
	"time"

	"github.com/ooaklee/ghatd/external/catalogue"
)

// ResolveLocalTime rejects nonexistent local times and chooses the earliest
// occurrence of a repeated time. It reads the timezone database without catalogue
// or provider I/O. Hosts must check current availability/policy separately and
// persist the resulting instant where replay stability is required.
func ResolveLocalTime(date, timezone, clock string) (time.Time, error) {
	day, err := time.Parse("2006-01-02", date)
	if err != nil || day.Format("2006-01-02") != date {
		return time.Time{}, catalogue.ErrInvalidPayload
	}
	localClock, err := time.Parse("15:04", clock)
	if err != nil || localClock.Format("15:04") != clock {
		return time.Time{}, catalogue.ErrInvalidPayload
	}
	zone, err := time.LoadLocation(timezone)
	if err != nil || timezone == "Local" {
		return time.Time{}, catalogue.ErrInvalidPayload
	}
	year, month, dayOfMonth := day.Date()
	naive := time.Date(year, month, dayOfMonth, localClock.Hour(), localClock.Minute(), 0, 0, time.UTC)
	offsets := map[int]bool{}
	for hour := -48; hour <= 48; hour++ {
		_, offset := naive.Add(time.Duration(hour) * time.Hour).In(zone).Zone()
		offsets[offset] = true
	}
	candidates := []time.Time{}
	for offset := range offsets {
		candidate := naive.Add(-time.Duration(offset) * time.Second)
		local := candidate.In(zone)
		if local.Year() == year && local.Month() == month && local.Day() == dayOfMonth && local.Hour() == localClock.Hour() && local.Minute() == localClock.Minute() && local.Second() == 0 {
			candidates = append(candidates, candidate)
		}
	}
	if len(candidates) == 0 {
		return time.Time{}, catalogue.ErrInvalidPayload
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Before(candidates[j]) })
	return candidates[0].UTC(), nil
}
