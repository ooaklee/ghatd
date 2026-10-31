package contentmanager

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/indexnow"
	"github.com/ooaklee/ghatd/external/post"
	"github.com/ooaklee/ghatd/external/seo"
)

// IndexNowSubmitter is the notification port implemented by indexnow.Service.
// Implementations must bound network work and must not retry implicitly.
type IndexNowSubmitter interface {
	SubmitURLs(context.Context, *indexnow.SubmitURLsRequest) (*indexnow.SubmitURLsResponse, error)
}

// IndexNowSitemapSource is the bounded read-only SEO inventory capability.
type IndexNowSitemapSource = seo.SitemapPublicationSource

// IndexNowConfig supplies trusted host URL and storage policy. Configure callbacks
// before serving; they must be concurrency safe and must not mutate their inputs.
type IndexNowConfig struct {
	// CanonicalOrigin is a fixed HTTP(S) origin, never the incoming Host header.
	CanonicalOrigin string
	// URLForPost maps supported post types to clean site-relative public paths.
	// Empty means this post has no public URL. It is required; no route is inferred.
	URLForPost func(post.Post) string
	// LookupPost snapshots persisted state by target ID before update/deletion.
	// Bind the post repository's GetPostById; request replacements are not history.
	LookupPost func(context.Context, string) (*post.Post, error)
	// AllowURL optionally applies host build/crawler or independent SEO policy.
	// Returning false excludes the URL; an error produces a safe warning.
	// Existing public URLs are checked before mutation so removal events survive.
	AllowURL func(context.Context, string) (bool, error)
	// PublicationSource supplies bounded current CMS inventory for combined due
	// reconciliation. It includes drafts, deletions and restricted records so a
	// matching SEO entry cannot bypass their public eligibility rules.
	PublicationSource post.PostPublicationSource
	// SitemapSource supplies independently scheduled SEO entries. It is required
	// only by ReconcileIndexNowEligibility; mutation notifications are unchanged.
	SitemapSource IndexNowSitemapSource
	// EligibleURL checks current host availability at the captured Through time.
	// It is required only by combined reconciliation, and must reject missing or
	// unavailable dynamic targets while permitting supported static routes.
	// Keep removal-aware mutation policy in AllowURL, not this callback.
	EligibleURL func(context.Context, string, time.Time) (bool, error)
	// Now optionally supplies the same clock used by public CMS visibility.
	Now func() time.Time
}

type indexNowConfiguration struct {
	submitter IndexNowSubmitter
	config    IndexNowConfig
	origin    *url.URL
	valid     bool
}

// WithIndexNow enables post-commit URL notifications. Invalid configuration
// fails closed for notifications and is reported as a safe receipt; it never
// turns an otherwise committed CMS mutation into an apparent failed write.
// A nil submitter disables integration, preserving the original I/O behavior.
func WithIndexNow(submitter IndexNowSubmitter, config IndexNowConfig) ServiceOption {
	return func(s *Service) {
		if nilContentDependency(submitter) {
			s.indexNow = nil
			return
		}
		origin, err := url.Parse(config.CanonicalOrigin)
		valid := err == nil && origin != nil && (origin.Scheme == "https" || origin.Scheme == "http") && origin.Host != "" && origin.User == nil && origin.Opaque == "" && origin.RawPath == "" && origin.RawQuery == "" && !origin.ForceQuery && origin.Fragment == "" && !strings.Contains(config.CanonicalOrigin, "#") && (origin.Path == "" || origin.Path == "/") && config.URLForPost != nil && config.LookupPost != nil
		s.indexNow = &indexNowConfiguration{submitter: submitter, config: config, origin: origin, valid: valid}
		if config.Now != nil {
			s.now = config.Now
		}
	}
}

// IndexNowNotificationReceipt separates notification status from the committed
// mutation. Status is skipped, acknowledged, pending or failed; no status proves
// crawling/indexing. Warning is a bounded category without dependency diagnostics.
type IndexNowNotificationReceipt struct {
	// Status separates skipped, acknowledged, pending and failed notifications.
	Status string `json:"status"`
	// AttemptID links optional IndexNow history; an empty value means no stored attempt receipt.
	AttemptID string `json:"attempt_id,omitempty"`
	// URLCount counts deduplicated URLs handed to the notifier, not indexed pages.
	URLCount int `json:"url_count"`
	// Warning is a fixed safe category; it never contains dependency diagnostics.
	Warning string `json:"warning,omitempty"`
	// RetryAfter retains a positive protocol-requested delay for trusted job
	// composition, including when completion auditing failed. It is not part of
	// mutation response JSON/headers, and does not make a successful 202 a retry.
	RetryAfter time.Duration `json:"-"`
}

type indexNowSnapshot struct{ url string }

func (s *Service) indexNowSnapshot(ctx context.Context, id string) (*indexNowSnapshot, string) {
	if s.indexNow == nil {
		return nil, ""
	}
	if !s.indexNow.valid {
		return nil, "configuration_invalid"
	}
	item, err := s.indexNow.config.LookupPost(ctx, id)
	if err != nil || item == nil || item.Id != id {
		return nil, "snapshot_unavailable"
	}
	value, warning := s.indexNowURL(ctx, item, s.publicationTime())
	return &indexNowSnapshot{url: value}, warning
}

var indexNowPostSlug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func (s *Service) indexNowURL(ctx context.Context, item *post.Post, now time.Time) (string, string) {
	if !IsPublicPostAt(item, now) {
		return "", ""
	}
	return s.indexNowPublicURL(ctx, item)
}

func (s *Service) indexNowPublicURL(ctx context.Context, item *post.Post) (string, string) {
	if item == nil || item.Id == "" || item.Type == "" || !strings.HasPrefix(item.UrlFriendlyId, string(item.Type)+"-") || !indexNowPostSlug.MatchString(item.UrlFriendlyId) {
		return "", ""
	}
	// Clone mutable slices before handing persisted data to host callbacks.
	copyItem := *item
	copyItem.Tags = append([]string(nil), item.Tags...)
	copyItem.Visibility = append([]string(nil), item.Visibility...)
	raw := s.indexNow.config.URLForPost(copyItem)
	if raw == "" {
		return "", ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || parsed.Opaque != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.ContainsAny(raw, "#\\%") || parsed.Path != path.Clean(parsed.Path) || strings.ContainsAny(raw, "\r\n\t ") {
		return "", "invalid_public_url"
	}
	// Private surfaces cannot be exposed accidentally by a callback.
	lower := strings.ToLower(parsed.Path)
	for _, prefix := range []string{"/admin", "/api", "/app", "/auth", "/offline", "/portal", "/settings", "/t", "/ref", "/waitlist/unsubscribe", "/__pages"} {
		if lower == prefix || strings.HasPrefix(lower, prefix+"/") {
			return "", "invalid_public_url"
		}
	}
	absolute := s.indexNow.origin.ResolveReference(parsed).String()
	if s.indexNow.config.AllowURL != nil {
		allowed, policyErr := s.indexNow.config.AllowURL(ctx, absolute)
		if policyErr != nil {
			return "", "policy_unavailable"
		}
		if !allowed {
			return "", ""
		}
	}
	return absolute, ""
}

func (s *Service) notifyIndexNow(ctx context.Context, previous *indexNowSnapshot, current *post.Post, warning string) *IndexNowNotificationReceipt {
	if s.indexNow == nil {
		return nil
	}
	if !s.indexNow.valid {
		return &IndexNowNotificationReceipt{Status: "skipped", Warning: "configuration_invalid"}
	}
	urls := []string{}
	if previous != nil && previous.url != "" {
		urls = append(urls, previous.url)
	}
	if current != nil {
		value, policyWarning := s.indexNowURL(ctx, current, s.publicationTime())
		if policyWarning != "" {
			warning = policyWarning
		}
		if value != "" && (len(urls) == 0 || urls[0] != value) {
			urls = append(urls, value)
		}
	}
	return s.submitIndexNow(ctx, urls, warning)
}

func (s *Service) submitIndexNow(ctx context.Context, urls []string, warning string) *IndexNowNotificationReceipt {
	receipt := &IndexNowNotificationReceipt{Status: "skipped", URLCount: len(urls), Warning: warning}
	if len(urls) == 0 {
		return receipt
	}
	response, err := s.indexNow.submitter.SubmitURLs(ctx, &indexnow.SubmitURLsRequest{URLs: urls})
	receipt.Status = "failed"
	var auditError *indexnow.AuditError
	acknowledgedWithAuditWarning := errors.As(err, &auditError) && auditError.Dispatched && response != nil && response.Success
	if acknowledgedWithAuditWarning {
		receipt.Warning = "audit_unavailable"
	}
	if response != nil {
		receipt.AttemptID = response.AttemptID
		if response.RetryAfter > 0 {
			receipt.RetryAfter = response.RetryAfter
		}
		if (err == nil || acknowledgedWithAuditWarning) && response.StatusCode == http.StatusOK && response.Success {
			receipt.Status = "acknowledged"
		}
		if (err == nil || acknowledgedWithAuditWarning) && response.StatusCode == http.StatusAccepted && response.Success {
			receipt.Status = "pending"
		}
	}
	if receipt.Status == "failed" {
		receipt.Warning = "notification_failed"
	}
	return receipt
}

func writeIndexNowHeaders(w http.ResponseWriter, receipt *IndexNowNotificationReceipt) {
	if receipt == nil {
		return
	}
	w.Header().Set("X-IndexNow-Status", receipt.Status)
	if receipt.Warning != "" {
		w.Header().Set("X-IndexNow-Warning", receipt.Warning)
	}
}

// ErrIndexNowReconciliationInvalid identifies a missing, future or oversized
// explicit checkpoint window, or invalid scan bound. No notification is sent.
var ErrIndexNowReconciliationInvalid = errors.New("contentmanager: invalid indexnow reconciliation window")

// ErrIndexNowReconciliationIncomplete means the bounded scan or submission did
// not complete. Retain the previous checkpoint; a deliberate retry may duplicate
// a prior notification because no distributed exactly-once scheduler is claimed.
var ErrIndexNowReconciliationIncomplete = errors.New("contentmanager: indexnow reconciliation incomplete")

// ReconcileIndexNowPublicationsRequest is a trusted host job request, never a
// transport-decoded actor. Choose After when enabling integration, then persist
// successful checkpoints. The window is (After,Through], at most 24 hours.
type ReconcileIndexNowPublicationsRequest struct {
	// After is the exclusive last successful checkpoint, initialized at activation.
	After time.Time
	// Through is the inclusive upper bound, no later than the current clock.
	Through time.Time
	// Limit bounds returned catalogue records, at most 10000; zero uses 10000.
	// Combined eligibility counts both CMS and SEO rows. Native keyset sources
	// may read one additional lookahead document per page to establish completion.
	Limit int
}

// ReconcileIndexNowPublicationsResponse reports one completely scanned window.
// Advance the host checkpoint only when Complete is true. A receipt acknowledges
// notification delivery, not search-engine indexing or cross-process uniqueness.
type ReconcileIndexNowPublicationsResponse struct {
	// Complete is true only after every bounded source read and notification succeeded.
	Complete bool
	// Checkpoint remains After on incomplete results, otherwise advances to Through.
	Checkpoint time.Time
	// Inspected counts returned source records, including unpublished/unmapped
	// records and both CMS/SEO sources when using combined eligibility.
	Inspected int
	// IndexNow carries the batch delivery receipt; it is nil before dispatch.
	IndexNow *IndexNowNotificationReceipt
}

// ReconcileIndexNowPublications announces posts becoming public in an explicit
// window without rewriting SEO rows. There is no implicit scheduler, activation
// backfill, retry or distributed claim. Source reads finish before any submission.
func (s *Service) ReconcileIndexNowPublications(ctx context.Context, req *ReconcileIndexNowPublicationsRequest) (*ReconcileIndexNowPublicationsResponse, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}
	now := s.publicationTime()
	if req.After.IsZero() || req.Through.IsZero() || !req.Through.After(req.After) || req.Through.After(now) || req.Through.Sub(req.After) > 24*time.Hour || req.Limit < 0 || req.Limit > 10000 {
		return nil, ErrIndexNowReconciliationInvalid
	}
	result := &ReconcileIndexNowPublicationsResponse{Checkpoint: req.After}
	if s.indexNow == nil || !s.indexNow.valid {
		return result, ErrIndexNowReconciliationIncomplete
	}
	limit := req.Limit
	if limit == 0 {
		limit = 10000
	}
	urls := []string{}
	seen := map[string]bool{}
	for page := 1; ; page++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		response, err := s.postService.GetPosts(ctx, &post.GetPostsRequest{Order: "created_at_asc", PerPage: 100, Page: page, Meta: true, IsNotDeleted: true})
		if err != nil || response == nil {
			return result, ErrIndexNowReconciliationIncomplete
		}
		if len(response.Posts) > 100 || response.TotalPages < 0 || (len(response.Posts) > 0 && response.TotalPages < page) || (len(response.Posts) == 0 && response.TotalPages > page) {
			return result, ErrIndexNowReconciliationIncomplete
		}
		for i := range response.Posts {
			if result.Inspected >= limit {
				return result, ErrIndexNowReconciliationIncomplete
			}
			result.Inspected++
			item := &response.Posts[i]
			publication, parseErr := parseChangelogSitemapTime(item.PublishedAt)
			if parseErr != nil || !publication.After(req.After) || publication.After(req.Through) {
				continue
			}
			value, warning := s.indexNowURL(ctx, item, now)
			if warning != "" {
				return result, ErrIndexNowReconciliationIncomplete
			}
			if value != "" && !seen[value] {
				seen[value] = true
				urls = append(urls, value)
			}
		}
		if len(response.Posts) == 0 || page >= response.TotalPages {
			break
		}
		if result.Inspected >= limit {
			return result, ErrIndexNowReconciliationIncomplete
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.IndexNow = s.submitIndexNow(ctx, urls, "")
	if result.IndexNow.Status == "failed" {
		return result, ErrIndexNowReconciliationIncomplete
	}
	result.Complete = true
	result.Checkpoint = req.Through
	return result, nil
}
