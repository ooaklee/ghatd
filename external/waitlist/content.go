package waitlist

import (
	"maps"
	"slices"
	"strings"
	"unicode"
)

// AnnouncementContent supplies trusted host validation and presentation without
// performing I/O or changing delivery state. Implementations must escape user
// copy in HTML. Each call receives independent maps/slices; errors never fall
// back to generic content or claim an unsent recipient.
type AnnouncementContent interface {
	Validate(Announcement) error
	Preview(Announcement, []Entry, string) (AnnouncementPresentation, error)
	Render(Announcement, Entry, string) (string, error)
}

// AnnouncementVariant is a named email preview with a current audience count.
// Its key and display label are host configuration, not recipient identity.
type AnnouncementVariant struct {
	Key            string `json:"key"`
	Label          string `json:"label"`
	HTML           string `json:"html"`
	RecipientCount int    `json:"recipientCount"`
}

// AnnouncementPresentation contains the default preview and optional variants.
// HTML is trusted host-rendered content, never interpreted as template source.
type AnnouncementPresentation struct {
	HTML     string
	Variants []AnnouncementVariant
}

func cloneAnnouncement(a Announcement) Announcement { a.Data = maps.Clone(a.Data); return a }

func validText(value string, max int, multiline bool) bool {
	if len(value) > max {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) && (!multiline || r != '\n') {
			return false
		}
	}
	return true
}

func (s *AnnouncementService) validateContent(a Announcement) error {
	if err := a.validate(); err != nil {
		return err
	}
	if len(a.Data) > 16 {
		return ErrInvalidAnnouncement
	}
	total := 0
	for key, value := range a.Data {
		if !configIdentifier.MatchString(key) || !validText(value, 4000, true) {
			return ErrInvalidAnnouncement
		}
		total += len(key) + len(value)
	}
	if total > 8000 {
		return ErrInvalidAnnouncement
	}
	if s.Content == nil {
		if len(a.Data) > 0 {
			return ErrInvalidAnnouncement
		}
		return nil
	}
	return s.Content.Validate(cloneAnnouncement(a))
}

func (s *AnnouncementService) presentation(a Announcement, entries []Entry) (AnnouncementPresentation, error) {
	if err := s.validateContent(a); err != nil {
		return AnnouncementPresentation{}, err
	}
	if s.Content == nil {
		return AnnouncementPresentation{HTML: s.renderAnnouncement(a, "#unsubscribe-preview")}, nil
	}
	result, err := s.Content.Preview(cloneAnnouncement(a), slices.Clone(entries), "#unsubscribe-preview")
	if err != nil {
		return AnnouncementPresentation{}, err
	}
	if !validHTML(result.HTML) || len(result.Variants) > 16 {
		return AnnouncementPresentation{}, ErrInvalidAnnouncement
	}
	seen := map[string]bool{}
	for _, variant := range result.Variants {
		if !configIdentifier.MatchString(variant.Key) || seen[variant.Key] || strings.TrimSpace(variant.Label) == "" || !validText(variant.Label, 128, false) || !validHTML(variant.HTML) || variant.RecipientCount < 0 || variant.RecipientCount > len(entries) {
			return AnnouncementPresentation{}, ErrInvalidAnnouncement
		}
		seen[variant.Key] = true
	}
	result.Variants = slices.Clone(result.Variants)
	return result, nil
}

func validHTML(value string) bool { return strings.TrimSpace(value) != "" && len(value) <= 1024*1024 }

func (s *AnnouncementService) renderRecipient(a Announcement, entry Entry, unsubscribe string) (string, error) {
	if err := s.validateContent(a); err != nil {
		return "", err
	}
	if s.Content == nil {
		return s.renderAnnouncement(a, unsubscribe), nil
	}
	result, err := s.Content.Render(cloneAnnouncement(a), entry, unsubscribe)
	if err != nil {
		return "", err
	}
	if !validHTML(result) {
		return "", ErrInvalidAnnouncement
	}
	return result, nil
}
