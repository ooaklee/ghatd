package contentmanager

import (
	"context"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/post"
	"github.com/ooaklee/ghatd/external/seo"
)

type indexNowEligibility struct {
	publication time.Time
	events      []time.Time
	blocked     bool
}

// IndexNowReconciliationRetryError is an incomplete notification pass with a
// positive server-requested delay. Its fixed message and private duration carry
// no protocol body, credentials or dependency diagnostics. Durable runners can
// discover RetryDelay through errors.As while retaining the incomplete sentinel.
type IndexNowReconciliationRetryError struct {
	retryAfter time.Duration
}

func (e *IndexNowReconciliationRetryError) Error() string {
	return "contentmanager: indexnow reconciliation incomplete; retry deferred"
}

// Unwrap preserves the incomplete-pass identity for existing host callers.
func (e *IndexNowReconciliationRetryError) Unwrap() error {
	return ErrIndexNowReconciliationIncomplete
}

// RetryDelay returns the original positive Retry-After duration without
// truncation. It defers another attempt and never establishes indexing success.
func (e *IndexNowReconciliationRetryError) RetryDelay() time.Duration {
	if e == nil {
		return 0
	}
	return e.retryAfter
}

// ReconcileIndexNowEligibility announces current public URLs whose CMS or SEO
// eligibility changes in (After,Through]. Both inventories are fully read before
// dispatch, within one shared Limit. Existing independent schedules are read,
// never rewritten or inferred from LastMod. Legacy records with no timestamped
// transition do not create an activation-time inventory notification.
//
// Unlike ReconcileIndexNowPublications, this method requires explicit bounded
// PublicationSource and SitemapSource capabilities plus EligibleURL. The maximum
// applicable CMS/SEO publication time gates every candidate, even if the earlier
// source became due before After. Valid creation/update times also identify new,
// cleared or retroactively edited schedules. These reads are not transactional
// snapshots, and external post-commit dispatch, retries and locking remain host
// concerns. A failed pass retains After and never sends a partial source batch.
func (s *Service) ReconcileIndexNowEligibility(ctx context.Context, req *ReconcileIndexNowPublicationsRequest) (*ReconcileIndexNowPublicationsResponse, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}
	if req.After.IsZero() || req.Through.IsZero() || !req.Through.After(req.After) || req.Through.After(s.publicationTime()) || req.Through.Sub(req.After) > 24*time.Hour || req.Limit < 0 || req.Limit > 10000 {
		return nil, ErrIndexNowReconciliationInvalid
	}
	result := &ReconcileIndexNowPublicationsResponse{Checkpoint: req.After}
	if s.indexNow == nil || !s.indexNow.valid || nilContentDependency(s.indexNow.config.PublicationSource) || nilContentDependency(s.indexNow.config.SitemapSource) || s.indexNow.config.EligibleURL == nil {
		return result, ErrIndexNowReconciliationIncomplete
	}
	limit := req.Limit
	if limit == 0 {
		limit = 10000
	}
	states := map[string]*indexNowEligibility{}
	if err := s.scanIndexNowPosts(ctx, req, result, limit, states); err != nil {
		return result, err
	}
	if err := s.scanIndexNowSEO(ctx, result, limit, states); err != nil {
		return result, err
	}
	urls := make([]string, 0, len(states))
	for value, state := range states {
		if state.blocked || state.publication.After(req.Through) || !indexNowEventInWindow(state.events, req.After, req.Through) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		eligible, err := s.indexNow.config.EligibleURL(ctx, value, req.Through)
		if err != nil {
			return result, ErrIndexNowReconciliationIncomplete
		}
		if eligible {
			urls = append(urls, value)
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	sort.Strings(urls)
	result.IndexNow = s.submitIndexNow(ctx, urls, "")
	if result.IndexNow.Status == "failed" || result.IndexNow.Warning != "" {
		// A completion-audit failure is still a received mutation notification,
		// but cannot prove a durably completed automatic reconciliation pass.
		if result.IndexNow.RetryAfter > 0 {
			return result, &IndexNowReconciliationRetryError{retryAfter: result.IndexNow.RetryAfter}
		}
		return result, ErrIndexNowReconciliationIncomplete
	}
	result.Complete = true
	result.Checkpoint = req.Through
	return result, nil
}

func indexNowEventInWindow(events []time.Time, after, through time.Time) bool {
	for _, event := range events {
		if event.After(after) && !event.After(through) {
			return true
		}
	}
	return false
}

func indexNowTransitionTimes(values ...string) []time.Time {
	var result []time.Time
	for _, value := range values {
		if instant, err := parseChangelogSitemapTime(value); err == nil && !instant.IsZero() {
			result = append(result, instant)
		}
	}
	return result
}

func (s *Service) scanIndexNowPosts(ctx context.Context, req *ReconcileIndexNowPublicationsRequest, result *ReconcileIndexNowPublicationsResponse, limit int, states map[string]*indexNowEligibility) error {
	after := ""
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		pageSize := min(100, limit-result.Inspected)
		if pageSize <= 0 {
			return ErrIndexNowReconciliationIncomplete
		}
		page, err := s.indexNow.config.PublicationSource.ListIndexNowPosts(ctx, &post.ListIndexNowPostsRequest{AfterID: after, Limit: pageSize})
		if err != nil || page == nil || len(page.Items) > pageSize {
			return ErrIndexNowReconciliationIncomplete
		}
		previous := after
		for i := range page.Items {
			item := &page.Items[i]
			if !validIndexNowSourceID(item.Id, previous) {
				return ErrIndexNowReconciliationIncomplete
			}
			previous = item.Id
			result.Inspected++
			value, warning := s.indexNowPublicURL(ctx, item)
			if warning != "" {
				return ErrIndexNowReconciliationIncomplete
			}
			if value == "" {
				continue
			}
			if _, duplicate := states[value]; duplicate {
				// Ambiguous CMS identities cannot establish canonical availability.
				return ErrIndexNowReconciliationIncomplete
			}
			publication, _ := parseChangelogSitemapTime(item.PublishedAt)
			states[value] = &indexNowEligibility{
				publication: publication,
				events:      indexNowTransitionTimes(item.PublishedAt, item.CreatedAt, item.UpdatedAt),
				blocked:     !IsPublicPostAt(item, req.Through),
			}
		}
		if page.NextID == "" {
			return nil
		}
		if len(page.Items) != pageSize || page.NextID != previous {
			return ErrIndexNowReconciliationIncomplete
		}
		after = page.NextID
	}
}

func (s *Service) scanIndexNowSEO(ctx context.Context, result *ReconcileIndexNowPublicationsResponse, limit int, states map[string]*indexNowEligibility) error {
	after := ""
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		// One final empty-page read is allowed when the CMS catalogue used the
		// entire bound; it proves an empty SEO inventory without inspecting a row.
		pageSize := min(100, max(1, limit-result.Inspected))
		page, err := s.indexNow.config.SitemapSource.ListIndexNowSitemapItems(ctx, &seo.ListIndexNowSitemapItemsRequest{AfterID: after, Limit: pageSize})
		if err != nil || page == nil || len(page.Items) > pageSize || len(page.Items) > limit-result.Inspected {
			return ErrIndexNowReconciliationIncomplete
		}
		previous := after
		for _, item := range page.Items {
			if !validIndexNowSourceID(item.ID, previous) {
				return ErrIndexNowReconciliationIncomplete
			}
			previous = item.ID
			result.Inspected++
			value, err := s.indexNowCanonicalSEOURL(ctx, item.URI)
			if err != nil {
				return ErrIndexNowReconciliationIncomplete
			}
			if value == "" {
				continue
			}
			publication, err := item.PublicationTime()
			if err != nil {
				return ErrIndexNowReconciliationIncomplete
			}
			state := states[value]
			if state == nil {
				state = &indexNowEligibility{}
				states[value] = state
			}
			if publication.After(state.publication) {
				state.publication = publication
			}
			state.events = append(state.events, indexNowTransitionTimes(item.PublishedAt, item.CreatedAt, item.UpdatedAt)...)
		}
		if page.NextID == "" {
			return nil
		}
		if len(page.Items) != pageSize || page.NextID != previous || result.Inspected >= limit {
			return ErrIndexNowReconciliationIncomplete
		}
		after = page.NextID
	}
}

func validIndexNowSourceID(id, previous string) bool {
	return id != "" && id > previous && id == strings.TrimSpace(id) && !strings.ContainsAny(id, "\x00\r\n\t")
}

func (s *Service) indexNowCanonicalSEOURL(ctx context.Context, raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || raw == "" || parsed.User != nil || parsed.Opaque != "" || parsed.RawPath != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || strings.ContainsAny(raw, "#\\%\r\n\t ") || parsed.Path != path.Clean(parsed.Path) || strings.ToLower(parsed.Path) != parsed.Path {
		return "", nil
	}
	if parsed.IsAbs() {
		if parsed.Scheme != s.indexNow.origin.Scheme || parsed.Host != s.indexNow.origin.Host {
			return "", nil
		}
	} else if !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || parsed.Host != "" {
		return "", nil
	}
	lower := strings.ToLower(parsed.Path)
	for _, prefix := range []string{"/admin", "/api", "/app", "/auth", "/offline", "/portal", "/settings", "/t", "/ref", "/waitlist/unsubscribe", "/__pages"} {
		if lower == prefix || strings.HasPrefix(lower, prefix+"/") {
			return "", nil
		}
	}
	absolute := s.indexNow.origin.ResolveReference(parsed).String()
	if s.indexNow.config.AllowURL != nil {
		allowed, err := s.indexNow.config.AllowURL(ctx, absolute)
		if err != nil || !allowed {
			return "", err
		}
	}
	return absolute, nil
}
