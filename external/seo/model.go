package seo

import (
	"encoding/json"
	"fmt"
	"net/url"
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
	CreatedByID     string          `json:"created_by_id,omitempty" bson:"created_by_id,omitempty"`
	CreatedAt       string          `json:"created_at,omitempty" bson:"created_at,omitempty"`
	UpdatedAt       string          `json:"updated_at,omitempty" bson:"updated_at,omitempty"`
	UpdatedByID     string          `json:"updated_by_id,omitempty" bson:"updated_by_id,omitempty"`
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

	return &SitemapItem{
		URI:             normaliseSitemapURI(req.URI),
		LastMod:         strings.TrimSpace(req.LastMod),
		Priority:        priority,
		ChangeFrequency: NormaliseChangeFrequency(req.ChangeFrequency),
		CreatedByID:     strings.TrimSpace(req.CreatedByID),
	}
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

	return nil
}

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
