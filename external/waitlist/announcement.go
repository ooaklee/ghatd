package waitlist

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"html/template"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/ooaklee/ghatd/external/emailprovider"
)

var (
	// ErrInvalidAnnouncement rejects unsafe or out-of-bounds administrator copy.
	ErrInvalidAnnouncement = errors.New("check the announcement details")
	// ErrPreviewExpired requires a fresh preview before any initial dispatch.
	ErrPreviewExpired = errors.New("preview the announcement again before sending")
	// ErrAnnouncementFrozen prevents replacing the one campaign already started.
	ErrAnnouncementFrozen = errors.New("an announcement has already started; resume its original preview")
	// ErrSendingDisabled reports unavailable provider, sender or delivery settings.
	ErrSendingDisabled = errors.New("email delivery is not configured")
)

// Announcement is a single prerelease notice, not an ongoing marketing campaign.
// Copy is supplied by the administrator when prerelease is actually ready.
type Announcement struct {
	// ID is a random preview handle, not the fixed campaign identity.
	ID string `bson:"_id" json:"id"`
	// Subject is a bounded single line; control characters are rejected.
	Subject string `bson:"subject" json:"subject"`
	// Message is escaped plain text with newline-separated paragraphs.
	Message string `bson:"message" json:"message"`
	// URL is the HTTPS early-access destination; userinfo is forbidden.
	URL string `bson:"url" json:"url"`
	// PreparedAt anchors the one-hour deadline for an unstarted preview.
	PreparedAt time.Time `bson:"preparedAt" json:"preparedAt"`
	// RecipientCount is informational; dispatch rechecks current eligibility.
	RecipientCount int `bson:"recipientCount" json:"recipientCount"`
}

func (a Announcement) validate() error {
	for _, field := range []struct {
		value     string
		max       int
		multiline bool
	}{
		{a.Subject, 150, false}, {a.Message, 4000, true},
	} {
		if strings.TrimSpace(field.value) == "" || len(field.value) > field.max {
			return ErrInvalidAnnouncement
		}
		for _, r := range field.value {
			if unicode.IsControl(r) && (!field.multiline || r != '\n') {
				return ErrInvalidAnnouncement
			}
		}
	}
	u, err := url.Parse(a.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || len(a.URL) > 2048 {
		return ErrInvalidAnnouncement
	}
	return nil
}

// AnnouncementRepository persists the one-campaign state machine. Claim must
// atomically win at most once per address; ambiguous claims cannot be retried.
type AnnouncementRepository interface {
	Store
	// SavePreview persists immutable administrator copy before returning its ID.
	SavePreview(context.Context, Announcement) error
	// Preview reads the exact saved proposal selected by the administrator.
	Preview(context.Context, string) (Announcement, error)
	// Started returns the frozen campaign or nil when nothing has started.
	Started(context.Context) (*Announcement, error)
	// Start freezes the first proposal, allowing only repeats of that same ID.
	Start(context.Context, Announcement) error
	// Claim records a send attempt before provider I/O and stores only a token hash.
	Claim(context.Context, Entry, string, string) (bool, error)
	// Finish records acceptance or an uncertain outcome without clearing the claim.
	Finish(context.Context, string, string, string) error
	// DeliveryStates returns historical outcomes, including unsubscribed entries.
	DeliveryStates(context.Context) (map[string]string, error)
	// Unsubscribe suppresses future sends by opaque token hash; repeats are harmless.
	Unsubscribe(context.Context, string) error
}

// AnnouncementService handles one bounded prerelease campaign, not a marketing
// platform. Construct it once with trusted host dependencies before serving.
type AnnouncementService struct {
	// BrandName is escaped display text. Empty uses the neutral "Early access".
	BrandName string
	// Store owns durable consent, previews and send claims.
	Store AnnouncementRepository
	// Provider is the explicitly selected sender; there is no fallback or retry.
	Provider emailprovider.EmailProvider
	// From is a valid mailbox address supplied by host composition.
	From string
	// FrontendURL is the origin/base for the existing unsubscribe page.
	FrontendURL string
	// Enabled gates remote sends. An explicit local-output provider may capture
	// while false; arbitrary providers cannot bypass this switch.
	Enabled bool
	// Now optionally supplies a deterministic clock; nil uses UTC wall time.
	Now func() time.Time
}

// AnnouncementPreview contains escaped email HTML and an explicit delivery mode.
type AnnouncementPreview struct {
	Announcement
	// HTML is a preview with a non-functional unsubscribe placeholder.
	HTML string `json:"html"`
	// Mode is "disabled", "local" (capture only), or "live" (provider submission).
	Mode string `json:"mode"`
}

// DispatchSummary reports lifetime delivery outcomes and current consent separately.
// An accepted message remains accepted after its recipient unsubscribes.
type DispatchSummary struct {
	// Accepted counts provider acceptances, or captures in local mode, not delivery.
	Accepted int `json:"accepted"`
	// Pending counts eligible addresses with no claim yet.
	Pending int `json:"pending"`
	// NeedsReview includes uncertain provider results and interrupted claims.
	NeedsReview int `json:"needsReview"`
	// Suppressed counts historical delivery records no longer in the audience.
	Suppressed int `json:"suppressed"`
	// Mode disambiguates remote acceptance from local-only capture.
	Mode string `json:"mode"`
}

func (s *AnnouncementService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *AnnouncementService) mode() string {
	local := false
	if provider, ok := s.Provider.(interface{ IsLocalOutputProvider() bool }); ok {
		local = provider.IsLocalOutputProvider()
	}
	if s.Provider == nil || (!s.Enabled && !local) || s.From == "" {
		return "disabled"
	}
	if _, err := mail.ParseAddress(s.From); err != nil || strings.ContainsAny(s.From, "\r\n") {
		return "disabled"
	}
	u, err := url.Parse(s.FrontendURL)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "disabled"
	}
	localHTTP := local && u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1")
	if u.Scheme != "https" && !localHTTP {
		return "disabled"
	}
	if local {
		return "local"
	}
	return "live"
}

// Prepare validates and saves a new preview without sending. A campaign which
// already started must be resumed instead; its copy cannot be replaced.
func (s *AnnouncementService) Prepare(ctx context.Context, a Announcement) (AnnouncementPreview, error) {
	if err := a.validate(); err != nil {
		return AnnouncementPreview{}, err
	}
	if active, err := s.Store.Started(ctx); err != nil {
		return AnnouncementPreview{}, err
	} else if active != nil {
		return AnnouncementPreview{}, ErrAnnouncementFrozen
	}
	entries, err := s.Store.Export(ctx)
	if err != nil {
		return AnnouncementPreview{}, err
	}
	a.ID = randomToken()
	a.PreparedAt = s.now()
	a.RecipientCount = len(entries)
	if err := s.Store.SavePreview(ctx, a); err != nil {
		return AnnouncementPreview{}, err
	}
	return s.preview(a), nil
}

func (s *AnnouncementService) preview(a Announcement) AnnouncementPreview {
	return AnnouncementPreview{Announcement: a, HTML: s.renderAnnouncement(a, "#unsubscribe-preview"), Mode: s.mode()}
}

// Current reads the frozen campaign with current audience size, or nil if absent.
func (s *AnnouncementService) Current(ctx context.Context) (*AnnouncementPreview, error) {
	a, err := s.Store.Started(ctx)
	if err != nil || a == nil {
		return nil, err
	}
	entries, err := s.Store.Export(ctx)
	if err != nil {
		return nil, err
	}
	a.RecipientCount = len(entries)
	p := s.preview(*a)
	return &p, nil
}

// Dispatch performs one bounded batch. The durable claim is made BEFORE calling
// the provider. Ambiguous errors and interrupted claims are never retried here:
// the shared provider interface does not promise idempotency or reconciliation.
func (s *AnnouncementService) Dispatch(ctx context.Context, id string) (DispatchSummary, error) {
	if s.mode() == "disabled" {
		return DispatchSummary{}, ErrSendingDisabled
	}
	a, err := s.Store.Preview(ctx, id)
	if err != nil {
		return DispatchSummary{}, ErrPreviewExpired
	}
	active, err := s.Store.Started(ctx)
	if err != nil {
		return DispatchSummary{}, err
	}
	if active == nil && s.now().Sub(a.PreparedAt) > time.Hour {
		return DispatchSummary{}, ErrPreviewExpired
	}
	if err := s.Store.Start(ctx, a); err != nil {
		return DispatchSummary{}, err
	}
	entries, err := s.Store.Export(ctx)
	if err != nil {
		return DispatchSummary{}, err
	}
	states, err := s.Store.DeliveryStates(ctx)
	if err != nil {
		return DispatchSummary{}, err
	}
	n := 0
	for _, e := range entries {
		if states[e.ID] != "" {
			continue
		}
		if n == 10 || ctx.Err() != nil {
			break
		}
		token := randomToken()
		claimed, err := s.Store.Claim(ctx, e, a.ID, tokenHash(token))
		if err != nil {
			return DispatchSummary{}, err
		}
		if !claimed {
			continue
		}
		n++
		unsubscribe := strings.TrimRight(s.FrontendURL, "/") + "/waitlist/unsubscribe#" + token
		body := s.renderAnnouncement(a, unsubscribe)
		sendCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		result, sendErr := s.Provider.Send(sendCtx, &emailprovider.Email{To: e.Email, From: s.From, Subject: a.Subject, HTMLBody: body})
		cancel()
		state, messageID := "needs_review", ""
		if sendErr == nil && result != nil && result.Success && result.Error == nil {
			state, messageID = "accepted", result.MessageID
		}
		// Persist the outcome even if the HTTP caller went away. A crash here
		// leaves the existing claim in needs-review territory, never retryable.
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		err = s.Store.Finish(finishCtx, e.ID, state, messageID)
		finishCancel()
		if err != nil {
			return DispatchSummary{}, err
		}
		if state == "needs_review" {
			break
		}
	}
	return s.Summary(ctx)
}

// Summary reports durable outcomes separately from consent. A cancelled context
// may prevent confirmation after a partial batch; never interpret that as rollback.
func (s *AnnouncementService) Summary(ctx context.Context) (DispatchSummary, error) {
	entries, err := s.Store.Export(ctx)
	if err != nil {
		return DispatchSummary{}, err
	}
	states, err := s.Store.DeliveryStates(ctx)
	if err != nil {
		return DispatchSummary{}, err
	}
	summary := DispatchSummary{Mode: s.mode()}
	active := make(map[string]bool, len(entries))
	for _, e := range entries {
		active[e.ID] = true
		if states[e.ID] == "" {
			summary.Pending++
		}
	}
	for id, state := range states {
		if !active[id] {
			summary.Suppressed++
		}
		switch state {
		case "accepted":
			summary.Accepted++
		case "sending", "needs_review":
			summary.NeedsReview++
		}
	}
	return summary, nil
}

func randomToken() string { return rand.Text() }
func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

var announcementTemplate = template.Must(template.New("announcement").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"></head><body style="margin:0;background:#fdfbf6;color:#0c0b08;font-family:Arial,sans-serif"><main style="max-width:560px;margin:0 auto;padding:40px 24px"><p style="font-size:22px;font-weight:600">{{.BrandName}}</p><h1 style="font-size:32px;line-height:1.15">{{.Subject}}</h1>{{range .Paragraphs}}<p style="font-size:16px;line-height:1.7">{{.}}</p>{{end}}<p style="margin:32px 0"><a href="{{.URL}}" style="display:inline-block;background:#dc4a25;color:#fdfbf6;padding:16px 24px;border-radius:30px;text-decoration:none">Take a first look ↗</a></p><p style="font-size:13px;line-height:1.7;color:#6b6453">You joined the {{.BrandName}} prerelease waitlist. <a href="{{.Unsubscribe}}" style="color:#b6311e">Unsubscribe from waitlist emails</a>.</p></main></body></html>`))

func (s *AnnouncementService) renderAnnouncement(a Announcement, unsubscribe string) string {
	brand := s.BrandName
	if brand == "" {
		brand = "Early access"
	}
	var out bytes.Buffer
	_ = announcementTemplate.Execute(&out, struct {
		Subject, URL, Unsubscribe, BrandName string
		Paragraphs                           []string
	}{a.Subject, a.URL, unsubscribe, brand, strings.Split(a.Message, "\n")})
	return out.String()
}
