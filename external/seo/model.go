package seo

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/toolbox"
)

// ChangeFrequency is a sitemap changefreq value stored and rendered lowercase.
type ChangeFrequency string

// SitemapItem is a stored URL entry used to generate sitemap XML.
type SitemapItem struct {
	ID              string          `json:"id" bson:"_id"`
	URI             string          `json:"uri" bson:"uri"`
	LastMod         string          `json:"last_mod" bson:"last_mod"`
	Priority        float64         `json:"priority" bson:"priority"`
	ChangeFrequency ChangeFrequency `json:"change_frequency" bson:"change_frequency"`
	// PublishedAt optionally schedules public sitemap inclusion. Empty values
	// keep the legacy behaviour of immediate inclusion. Non-empty values must
	// hold a valid timestamp; entries become eligible for generated output only
	// at or after that instant. It is independent from LastMod and CreatedAt.
	PublishedAt string `json:"published_at,omitempty" bson:"published_at,omitempty"`
	CreatedByID string `json:"created_by_id,omitempty" bson:"created_by_id,omitempty"`
	CreatedAt   string `json:"created_at,omitempty" bson:"created_at,omitempty"`
	UpdatedAt   string `json:"updated_at,omitempty" bson:"updated_at,omitempty"`
	UpdatedByID string `json:"updated_by_id,omitempty" bson:"updated_by_id,omitempty"`
}

// NewSitemapItem returns a normalised sitemap item with defaults.
func NewSitemapItem(req *CreateSitemapItemRequest) *SitemapItem {
	if req == nil {
		return &SitemapItem{
			Priority:        defaultSitemapPriority,
			ChangeFrequency: ChangeFrequencyWeekly,
		}
	}

	priority := defaultSitemapPriority
	if req.Priority != nil {
		priority = *req.Priority
	}

	// A nil PublishedAt request field omits scheduling entirely (legacy
	// inclusion). An empty or whitespace-only string clears the gate. Any
	// other value is preserved raw: validateSitemapItem normalises valid
	// timestamps and rejects invalid ones, so invalid caller input can never
	// become an immediately eligible record.
	publishedAt := ""
	if req.PublishedAt != nil {
		publishedAt = strings.TrimSpace(*req.PublishedAt)
	}

	return &SitemapItem{
		URI:             normaliseSitemapURI(req.URI),
		LastMod:         strings.TrimSpace(req.LastMod),
		Priority:        priority,
		ChangeFrequency: NormaliseChangeFrequency(req.ChangeFrequency),
		PublishedAt:     publishedAt,
		CreatedByID:     strings.TrimSpace(req.CreatedByID),
	}
}

// normaliseSitemapItemPublishedAtInput interprets the triple-state request
// field: nil (omitted or JSON null) means no publication intent and returns
// empty; an empty or whitespace-only string clears the gate for legacy
// inclusion; any other value is normalised to a precise UTC timestamp or
// rejected with ErrSitemapItemInvalidPublishedAt.
func normaliseSitemapItemPublishedAtInput(value *string) (string, error) {
	if value == nil {
		return "", nil
	}
	return normaliseSitemapItemPublishedAt(*value)
}

// normaliseSitemapItemPublishedAt normalises a stored or requested
// publication value: empty stays empty (legacy inclusion), and a valid value
// is re-rendered as a precise UTC timestamp. It rejects zero instants, year
// zero/out-of-range results and offsets Go's parser would otherwise accept
// (for example +24:00 or +00:60), returning ErrSitemapItemInvalidPublishedAt.
// Whitespace is trimmed; JSON null decodes to a nil request pointer which
// callers treat as omission before reaching this function.
func normaliseSitemapItemPublishedAt(value string) (string, error) {
	instant, err := parseSitemapPublishedTimestamp(value)
	if err != nil {
		return "", err
	}
	if instant.IsZero() {
		return "", nil
	}
	return instant.UTC().Format(time.RFC3339Nano), nil
}

// Keep the wire grammar strict: time.Parse alone accepts malformed offsets,
// comma fractions and excess precision. Zone-less platform timestamps are UTC.
var sitemapPublicationTimestamp = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,9})?(?:Z|[+-](?:[01][0-9]|2[0-3]):[0-5][0-9])?$`)

func parseSitemapPublishedTimestamp(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	if !sitemapPublicationTimestamp.MatchString(value) || strings.HasPrefix(value, "0000-") {
		return time.Time{}, ErrSitemapItemInvalidPublishedAt
	}
	instant, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		instant, err = time.Parse("2006-01-02T15:04:05.999999999", value)
	}
	instant = instant.UTC()
	if err != nil || instant.IsZero() || instant.Year() < 1 || instant.Year() > 9999 {
		return time.Time{}, ErrSitemapItemInvalidPublishedAt
	}
	return instant, nil
}

// publishedAtInstant returns the parsed publication instant for a stored
// value. Missing or empty values report a zero instant with no error and keep
// the legacy inclusion rules. Invalid non-empty values return
// ErrSitemapItemInvalidPublishedAt so generation never silently includes a
// malformed schedule.
func (s SitemapItem) publishedAtInstant() (time.Time, error) {
	instant, err := parseSitemapPublishedTimestamp(s.PublishedAt)
	if err != nil {
		return time.Time{}, err
	}
	return instant, nil
}

// isPublishedForEligibility reports whether the item is eligible for generated
// public output at the captured evaluation instant. Items with an absent,
// empty or invalid stored publication value are discussed by the caller: this
// method only compares a parsed valid instant. Items whose publication instant
// is in the future relative to the evaluation time are excluded.
func (s SitemapItem) isPublishedForEligibility(evaluation time.Time) (bool, error) {
	instant, err := s.publishedAtInstant()
	if err != nil {
		return false, err
	}
	if instant.IsZero() {
		return true, nil
	}
	return !instant.After(evaluation), nil
}

// GenerateID creates a UUID for the sitemap item.
func (s *SitemapItem) GenerateID() *SitemapItem {
	s.ID = toolbox.GenerateUuidV4()
	return s
}

// SetCreatedAtTimeToNow sets the created timestamp to the platform UTC format.
func (s *SitemapItem) SetCreatedAtTimeToNow() *SitemapItem {
	s.CreatedAt = toolbox.TimeNowUTC()
	return s
}

// SetUpdatedAtTimeToNow sets the updated timestamp to the platform UTC format.
func (s *SitemapItem) SetUpdatedAtTimeToNow() *SitemapItem {
	s.UpdatedAt = toolbox.TimeNowUTC()
	return s
}

// LastModForXML converts the platform UTC timestamp into the sitemap timestamp format.
func (s SitemapItem) LastModForXML() (string, error) {
	return formatSitemapTimestamp(s.LastMod)
}

// FormatPriority renders priority without losing precision, while preserving 1.0 style values.
func (s SitemapItem) FormatPriority() string {
	formatted := strconv.FormatFloat(s.Priority, 'f', -1, 64)
	if !strings.Contains(formatted, ".") {
		return formatted + ".0"
	}
	return formatted
}

// IsValid reports whether the change frequency is supported by the sitemap package.
func (c ChangeFrequency) IsValid() bool {
	switch c {
	case ChangeFrequencyAlways,
		ChangeFrequencyHourly,
		ChangeFrequencyDaily,
		ChangeFrequencyWeekly,
		ChangeFrequencyMonthly,
		ChangeFrequencyYearly,
		ChangeFrequencyNever:
		return true
	default:
		return false
	}
}

// String returns the lowercase change frequency value.
func (c ChangeFrequency) String() string {
	return string(c)
}

// MarshalJSON renders the lowercase change frequency value.
func (c ChangeFrequency) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.String())
}

// UnmarshalJSON normalises supported change frequency values from JSON.
func (c *ChangeFrequency) UnmarshalJSON(data []byte) error {
	var raw string
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	*c = NormaliseChangeFrequency(raw)
	return nil
}

// NormaliseChangeFrequency trims and lowercases a change frequency value.
func NormaliseChangeFrequency(value interface{}) ChangeFrequency {
	return ChangeFrequency(strings.ToLower(strings.TrimSpace(fmt.Sprint(value))))
}

// validateSitemapItem normalises and validates a sitemap item in place,
// defaulting change frequency to weekly and last_mod to now, and rejecting
// missing, malformed or out-of-range fields with specific errors.
func validateSitemapItem(item *SitemapItem) error {
	if item == nil {
		return ErrSitemapItemError
	}

	item.URI = normaliseSitemapURI(item.URI)
	if item.URI == "" {
		return ErrSitemapItemURIIsRequired
	}
	if err := validateSitemapURI(item.URI); err != nil {
		return err
	}
	if item.Priority < 0 || item.Priority > 1 {
		return ErrSitemapItemInvalidPriority
	}
	if item.ChangeFrequency == "" {
		item.ChangeFrequency = ChangeFrequencyWeekly
	}
	if !item.ChangeFrequency.IsValid() {
		return ErrSitemapItemInvalidChangeFrequency
	}
	if item.LastMod == "" {
		item.LastMod = toolbox.TimeNowUTC()
	}
	if _, err := formatSitemapTimestamp(item.LastMod); err != nil {
		return ErrSitemapItemInvalidLastMod
	}
	// Publication scheduling is independent from last_mod: valid values are
	// normalised to precise UTC, empty values keep legacy inclusion, and
	// invalid values (including zero instants) reject the item.
	normalisedPublishedAt, err := normaliseSitemapItemPublishedAt(item.PublishedAt)
	if err != nil {
		return err
	}
	item.PublishedAt = normalisedPublishedAt

	return nil
}

// normaliseSitemapURI trims the value and prefixes relative paths with a slash,
// leaving http/https URLs untouched; blank input returns blank.
func normaliseSitemapURI(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}

	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		return value
	}

	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}

	return value
}

// validateSitemapURI accepts http/https absolute URLs with a host, or root-
// relative paths not starting with '//'; anything else fails with
// ErrSitemapItemInvalidURI.
func validateSitemapURI(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return ErrSitemapItemInvalidURI
	}
	if parsed.IsAbs() {
		if parsed.Scheme != "http" && parsed.Scheme != "https" {
			return ErrSitemapItemInvalidURI
		}
		if parsed.Host == "" {
			return ErrSitemapItemInvalidURI
		}
		return nil
	}
	if !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return ErrSitemapItemInvalidURI
	}
	return nil
}

// formatSitemapTimestamp parses an RFC3339-like or date-only timestamp and
// renders it in the UTC sitemap format, defaulting empty input to now and
// returning the parse error for unsupported values.
func formatSitemapTimestamp(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		value = toolbox.TimeNowUTC()
	}

	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}

	var parsed time.Time
	var err error
	for _, layout := range layouts {
		parsed, err = time.Parse(layout, value)
		if err == nil {
			return parsed.UTC().Format("2006-01-02T15:04:05+00:00"), nil
		}
	}

	return "", err
}
