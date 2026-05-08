package streaker

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/common"
	"github.com/ooaklee/ghatd/external/toolbox"
)

// StreakScope identifies the entity and target a streak belongs to.
type StreakScope struct {
	StreakType string `json:"streak_type" bson:"streak_type"`
	OwnerId    string `json:"owner_id" bson:"owner_id"`
	TargetType string `json:"target_type" bson:"target_type"`
	TargetId   string `json:"target_id" bson:"target_id"`
}

// PreviousStreakEntry is stored on each streak entry when a related previous
// entry exists. It gives consumers a cheap chain back through the streak.
type PreviousStreakEntry struct {
	Id           string `json:"id" bson:"id"`
	NanoId       string `json:"nano_id,omitempty" bson:"_nano_id,omitempty"`
	PeriodKey    string `json:"period_key" bson:"period_key"`
	OccurredAt   string `json:"occurred_at" bson:"occurred_at"`
	CurrentCount int    `json:"current_count" bson:"current_count"`
	CreatedAt    string `json:"created_at" bson:"created_at"`
}

// Streak represents a single qualifying completion for a streak scope.
type Streak struct {
	Id         string `json:"id" bson:"_id"`
	NanoId     string `json:"nano_id" bson:"_nano_id"`
	StreakName string `json:"streak_name,omitempty" bson:"streak_name,omitempty"`

	StreakType string `json:"streak_type" bson:"streak_type"`
	OwnerId    string `json:"owner_id" bson:"owner_id"`
	TargetType string `json:"target_type" bson:"target_type"`
	TargetId   string `json:"target_id" bson:"target_id"`

	PeriodType StreakPeriodType `json:"period_type" bson:"period_type"`
	PeriodKey  string           `json:"period_key" bson:"period_key"`
	OccurredAt string           `json:"occurred_at" bson:"occurred_at"`

	CurrentCount int                    `json:"current_count" bson:"current_count"`
	Previous     *PreviousStreakEntry   `json:"previous,omitempty" bson:"previous,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty" bson:"metadata,omitempty"`

	CreatedAt       string `json:"created_at" bson:"created_at"`
	CreatedByUserId string `json:"created_by_user_id,omitempty" bson:"created_by_user_id,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty" bson:"updated_at,omitempty"`
	UpdatedByUserId string `json:"updated_by_user_id,omitempty" bson:"updated_by_user_id,omitempty"`
}

// GenerateId generates a new Id for the streak entry.
func (s *Streak) GenerateId() *Streak {
	s.Id = toolbox.GenerateUuidV4()
	return s
}

// GenerateNanoId generates a new Nano Id for the streak entry.
func (s *Streak) GenerateNanoId() *Streak {
	s.NanoId = toolbox.GenerateNanoId()
	return s
}

// SetCreatedAtTimeToNow sets the created at date and time to now.
func (s *Streak) SetCreatedAtTimeToNow() *Streak {
	s.CreatedAt = toolbox.TimeNowUTC()
	return s
}

// SetUpdatedAtTimeToNow sets the updated at date and time to now.
func (s *Streak) SetUpdatedAtTimeToNow() *Streak {
	s.UpdatedAt = toolbox.TimeNowUTC()
	return s
}

// ToScope returns the scope for this streak entry.
func (s *Streak) ToScope() StreakScope {
	return StreakScope{
		StreakType: s.StreakType,
		OwnerId:    s.OwnerId,
		TargetType: s.TargetType,
		TargetId:   s.TargetId,
	}
}

// IsSameScopeAs returns true when the provided streak belongs to the same
// streak counter as the receiver.
func (s *Streak) IsSameScopeAs(other *Streak) bool {
	if other == nil {
		return false
	}

	return s.StreakType == other.StreakType &&
		s.OwnerId == other.OwnerId &&
		s.TargetType == other.TargetType &&
		s.TargetId == other.TargetId
}

// ToPreviousEntry creates the lightweight previous-entry reference persisted
// on the next streak entry.
func (s *Streak) ToPreviousEntry() *PreviousStreakEntry {
	if s == nil {
		return nil
	}

	return &PreviousStreakEntry{
		Id:           s.Id,
		NanoId:       s.NanoId,
		PeriodKey:    s.PeriodKey,
		OccurredAt:   s.OccurredAt,
		CurrentCount: s.CurrentCount,
		CreatedAt:    s.CreatedAt,
	}
}

// NormaliseScope trims the fields that make a streak scope.
func NormaliseScope(scope StreakScope) StreakScope {
	return StreakScope{
		StreakType: strings.TrimSpace(scope.StreakType),
		OwnerId:    strings.TrimSpace(scope.OwnerId),
		TargetType: strings.TrimSpace(scope.TargetType),
		TargetId:   strings.TrimSpace(scope.TargetId),
	}
}

// ParseStreakTime parses the package's preferred UTC timestamp format.
func ParseStreakTime(value string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, errors.New(ErrKeyInvalidOccurredAt)
	}

	parsed, err := time.Parse(common.RFC3339NanoUTC, strings.TrimSpace(value))
	if err == nil {
		return parsed.UTC(), nil
	}

	parsed, err = time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err == nil {
		return parsed.UTC(), nil
	}

	return time.Time{}, errors.New(ErrKeyInvalidOccurredAt)
}

// FormatStreakTime formats time using the platform UTC timestamp format.
func FormatStreakTime(value time.Time) string {
	return value.UTC().Format(common.RFC3339NanoUTC)
}

// NormalisePeriodType returns the package default period when none is provided.
func NormalisePeriodType(periodType StreakPeriodType) (StreakPeriodType, error) {
	switch periodType {
	case "":
		return StreakPeriodTypeDaily, nil
	case StreakPeriodTypeDaily, StreakPeriodTypeWeekly, StreakPeriodTypeMonthly, StreakPeriodTypeCustom:
		return periodType, nil
	default:
		return "", errors.New(ErrKeyInvalidPeriodType)
	}
}

// BuildPeriodKey converts a timestamp and period type into a stable key.
func BuildPeriodKey(value time.Time, periodType StreakPeriodType, providedPeriodKey string) (string, error) {
	value = value.UTC()
	providedPeriodKey = strings.TrimSpace(providedPeriodKey)

	switch periodType {
	case StreakPeriodTypeDaily:
		return value.Format("2006-01-02"), nil
	case StreakPeriodTypeWeekly:
		return buildWeekPeriodKey(value), nil
	case StreakPeriodTypeMonthly:
		return value.Format("2006-01"), nil
	case StreakPeriodTypeCustom:
		if providedPeriodKey == "" {
			return "", errors.New(ErrKeyPeriodKeyIsRequired)
		}
		return providedPeriodKey, nil
	default:
		return "", errors.New(ErrKeyInvalidPeriodType)
	}
}

// IsConsecutivePeriod returns true when previousPeriodKey immediately precedes currentPeriodKey.
func IsConsecutivePeriod(previousPeriodKey, currentPeriodKey string, periodType StreakPeriodType) bool {
	previousPeriodKey = strings.TrimSpace(previousPeriodKey)
	currentPeriodKey = strings.TrimSpace(currentPeriodKey)

	if previousPeriodKey == "" || currentPeriodKey == "" {
		return false
	}

	if previousPeriodKey == currentPeriodKey {
		return true
	}

	previous, ok := parsePeriodKey(previousPeriodKey, periodType)
	if !ok {
		return false
	}

	var next string
	switch periodType {
	case StreakPeriodTypeDaily:
		next = previous.AddDate(0, 0, 1).Format("2006-01-02")
	case StreakPeriodTypeWeekly:
		next = buildWeekPeriodKey(previous.AddDate(0, 0, 7))
	case StreakPeriodTypeMonthly:
		next = previous.AddDate(0, 1, 0).Format("2006-01")
	default:
		return false
	}

	return next == currentPeriodKey
}

func parsePeriodKey(periodKey string, periodType StreakPeriodType) (time.Time, bool) {
	switch periodType {
	case StreakPeriodTypeDaily:
		t, err := time.Parse("2006-01-02", periodKey)
		return t, err == nil
	case StreakPeriodTypeWeekly:
		var year int
		var week int
		if _, err := fmt.Sscanf(periodKey, "%d-w%d", &year, &week); err != nil {
			return time.Time{}, false
		}
		return firstDayOfISOWeek(year, week), true
	case StreakPeriodTypeMonthly:
		t, err := time.Parse("2006-01", periodKey)
		return t, err == nil
	default:
		return time.Time{}, false
	}
}

func buildWeekPeriodKey(value time.Time) string {
	year, week := value.UTC().ISOWeek()
	return strconv.Itoa(year) + "-w" + leftPadInt(week, 2)
}

func firstDayOfISOWeek(year int, week int) time.Time {
	date := time.Date(year, 1, 4, 0, 0, 0, 0, time.UTC)
	isoWeekday := int(date.Weekday())
	if isoWeekday == 0 {
		isoWeekday = 7
	}
	monday := date.AddDate(0, 0, -isoWeekday+1)
	return monday.AddDate(0, 0, (week-1)*7)
}

func leftPadInt(value int, length int) string {
	asString := strconv.Itoa(value)
	for len(asString) < length {
		asString = "0" + asString
	}
	return asString
}
