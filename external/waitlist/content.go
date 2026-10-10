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
	// Validate checks the given Announcement through the AnnouncementContent port,
	// which supplies trusted host validation without I/O or delivery-state changes;
	// implementations report acceptance or an error and never fall back to generic
	// content.
	Validate(Announcement) error
	// Preview builds an AnnouncementPresentation for the given announcement, sample
	// entries, and locale through the AnnouncementContent port without I/O;
	// implementations must escape user copy in HTML and use independent maps and
	// slices per call.
	Preview(Announcement, []Entry, string) (AnnouncementPresentation, error)
	// Render produces the delivered copy for one announcement and entry in the
	// given locale through the AnnouncementContent port; implementations must
	// escape user copy in HTML and return an error rather than generic fallback
	// content.
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

// cloneAnnouncement returns a copy with an independent Data map so recipients
// cannot mutate shared metadata.
func cloneAnnouncement(a Announcement) Announcement { a.Data = maps.Clone(a.Data); return a }

// validText reports whether value is within max bytes and free of control
// characters, permitting newlines only when multiline is set.
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

// validateContent applies base announcement validation plus host-data rules: at
// most 16 identifier-keyed entries within a combined size budget, no data
// without a content provider, and finally host validation on a clone.
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

// presentation produces validated preview HTML and variants, using the built-in
// renderer when no content provider is configured; host-produced variants are
// checked for keys, labels, sizes, HTML validity and plausible recipient counts
// before being cloned.
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

// validHTML reports whether the HTML is non-blank and at most 1 MiB.
func validHTML(value string) bool { return strings.TrimSpace(value) != "" && len(value) <= 1024*1024 }

// renderRecipient validates the announcement and renders per-recipient HTML,
// using the built-in template when no content provider is configured and
// rejecting host output that is not valid HTML.
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
