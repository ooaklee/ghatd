package contentmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/indexnow"
	"github.com/ooaklee/ghatd/external/post"
	"github.com/ooaklee/ghatd/external/seo"
	"github.com/stretchr/testify/require"
)

type eligibilityPosts struct {
	*cmsIndexNowPostPort
	items    []post.Post
	failure  error
	response *post.ListIndexNowPostsResponse
}

func (p *eligibilityPosts) ListIndexNowPosts(ctx context.Context, req *post.ListIndexNowPostsRequest) (*post.ListIndexNowPostsResponse, error) {
	if p.failure != nil || p.response != nil {
		return p.response, p.failure
	}
	result := &post.ListIndexNowPostsResponse{}
	for _, item := range p.items {
		if item.Id > req.AfterID {
			if len(result.Items) == req.Limit {
				result.NextID = result.Items[len(result.Items)-1].Id
				break
			}
			result.Items = append(result.Items, item)
		}
	}
	return result, ctx.Err()
}

type eligibilitySEO struct {
	items    []seo.SitemapItem
	failure  error
	response *seo.ListIndexNowSitemapItemsResponse
}

func (p *eligibilitySEO) ListIndexNowSitemapItems(ctx context.Context, req *seo.ListIndexNowSitemapItemsRequest) (*seo.ListIndexNowSitemapItemsResponse, error) {
	if p.failure != nil || p.response != nil {
		return p.response, p.failure
	}
	result := &seo.ListIndexNowSitemapItemsResponse{}
	for _, item := range p.items {
		if item.ID > req.AfterID {
			if len(result.Items) == req.Limit {
				result.NextID = result.Items[len(result.Items)-1].ID
				break
			}
			result.Items = append(result.Items, item)
		}
	}
	return result, ctx.Err()
}

func eligibilityService(posts *eligibilityPosts, entries *eligibilitySEO, notifier IndexNowSubmitter, eligible func(context.Context, string, time.Time) (bool, error)) *Service {
	config := cmsIndexNowConfig(posts.cmsIndexNowPostPort)
	config.PublicationSource = posts
	config.SitemapSource = entries
	config.EligibleURL = eligible
	return NewService(posts, nil, WithIndexNow(notifier, config))
}

func TestIndexNowCombinedEligibilityTransitions(t *testing.T) {
	now := cmsIndexNowTime()
	old := now.Add(-2 * time.Hour).Format(time.RFC3339Nano)
	due := now.Format(time.RFC3339Nano)
	future := now.Add(time.Nanosecond).Format(time.RFC3339Nano)
	for _, tc := range []struct {
		name, cmsDate, seoDate, cmsCreated, cmsUpdated, seoCreated, seoUpdated, uri string
		noCMS, noSEO, denied, restricted, deleted, policyError, availabilityError   bool
		want                                                                        bool
		wantError                                                                   error
	}{
		{name: "CMS only due", cmsDate: due, noSEO: true, want: true},
		{name: "older CMS later SEO due", cmsDate: old, seoDate: due, want: true},
		{name: "older SEO later CMS due", cmsDate: due, seoDate: old, want: true},
		{name: "same instant deduplicated", cmsDate: due, seoDate: due, want: true},
		{name: "later SEO gate suppresses CMS due", cmsDate: due, seoDate: future},
		{name: "later CMS gate suppresses SEO due", cmsDate: future, seoDate: due},
		{name: "new immediate SEO entry with old CMS", cmsDate: old, seoCreated: due, want: true},
		{name: "cleared SEO gate with old CMS", cmsDate: old, seoUpdated: due, want: true},
		{name: "retroactive SEO reschedule", cmsDate: old, seoDate: old, seoUpdated: due, want: true},
		{name: "retrospective CMS publication edit", cmsDate: old, cmsUpdated: due, noSEO: true, want: true},
		{name: "new backdated CMS", cmsDate: old, cmsCreated: due, noSEO: true, want: true},
		{name: "old records no event", cmsDate: old, seoDate: old},
		{name: "blank SEO no synthetic event", cmsDate: old},
		{name: "future update is not a transition", cmsDate: old, seoUpdated: future},
		{name: "invalid updated time ignored", cmsDate: old, seoUpdated: "private-invalid"},
		{name: "static SEO publication", noCMS: true, seoDate: due, uri: "/pricing", want: true},
		{name: "static immediate SEO creation", noCMS: true, seoCreated: due, uri: "/pricing", want: true},
		{name: "undated static SEO no activation blast", noCMS: true, uri: "/pricing"},
		{name: "missing dynamic target", noCMS: true, seoDate: due, denied: true},
		{name: "draft CMS blocks SEO", seoDate: due},
		{name: "restricted CMS blocks SEO", cmsDate: old, seoDate: due, restricted: true},
		{name: "deleted CMS blocks SEO", cmsDate: old, seoDate: due, deleted: true},
		{name: "invalid CMS date blocks SEO", cmsDate: "private-invalid", seoDate: due},
		{name: "malformed SEO date fails pass", cmsDate: due, seoDate: "private-invalid", wantError: ErrIndexNowReconciliationIncomplete},
		{name: "malformed SEO offset", cmsDate: due, seoDate: "2026-10-30T12:00:00+24:00", wantError: ErrIndexNowReconciliationIncomplete},
		{name: "UTC platform SEO schedule", cmsDate: old, seoDate: "2026-10-30T12:00:00", want: true},
		{name: "offset SEO same instant", cmsDate: old, seoDate: "2026-10-30T13:00:00+01:00", want: true},
		{name: "policy failure aborts", cmsDate: due, noSEO: true, policyError: true, wantError: ErrIndexNowReconciliationIncomplete},
		{name: "availability failure aborts", cmsDate: due, noSEO: true, availabilityError: true, wantError: ErrIndexNowReconciliationIncomplete},
		{name: "compiled build denies", cmsDate: due, noSEO: true, denied: true},
		{name: "absolute canonical SEO", cmsDate: old, seoDate: due, uri: "https://example.com/blog/due", want: true},
		{name: "foreign absolute SEO excluded", noCMS: true, seoDate: due, uri: "https://foreign.example/blog/due"},
		{name: "different scheme excluded", noCMS: true, seoDate: due, uri: "http://example.com/blog/due"},
		{name: "trailing slash alias excluded", noCMS: true, seoDate: due, uri: "/blog/due/"},
		{name: "uppercase alias excluded", noCMS: true, seoDate: due, uri: "/blog/Due"},
		{name: "escaped alias excluded", noCMS: true, seoDate: due, uri: "/blog/%64ue"},
		{name: "query alias excluded", noCMS: true, seoDate: due, uri: "/blog/due?x=1"},
		{name: "fragment alias excluded", noCMS: true, seoDate: due, uri: "/blog/due#x"},
		{name: "protocol relative excluded", noCMS: true, seoDate: due, uri: "//example.com/blog/due"},
		{name: "private SEO excluded", noCMS: true, seoDate: due, uri: "/admin"},
		{name: "root SEO scheduled", noCMS: true, seoDate: due, uri: "/", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := cmsIndexNowPost("article-due")
			item.PublishedAt, item.CreatedAt, item.UpdatedAt = tc.cmsDate, tc.cmsCreated, tc.cmsUpdated
			if tc.restricted {
				item.Visibility = []string{"members"}
			}
			if tc.deleted {
				item.DeletedAt = due
			}
			posts := &eligibilityPosts{cmsIndexNowPostPort: &cmsIndexNowPostPort{}}
			if !tc.noCMS {
				posts.items = []post.Post{*item}
			}
			uri := tc.uri
			if uri == "" {
				uri = "/blog/due"
			}
			entries := &eligibilitySEO{}
			if !tc.noSEO {
				entries.items = []seo.SitemapItem{{ID: "seo-1", URI: uri, PublishedAt: tc.seoDate, CreatedAt: tc.seoCreated, UpdatedAt: tc.seoUpdated}}
			}
			notifier := &cmsIndexNowSubmitter{t: t, status: 202}
			service := eligibilityService(posts, entries, notifier, func(_ context.Context, value string, at time.Time) (bool, error) {
				require.Equal(t, now, at)
				require.True(t, strings.HasPrefix(value, "https://example.com/"))
				if tc.availabilityError {
					return false, errors.New("private availability diagnostic")
				}
				return !tc.denied, nil
			})
			if tc.policyError {
				service.indexNow.config.AllowURL = func(context.Context, string) (bool, error) { return false, errors.New("private policy diagnostic") }
			}
			result, err := service.ReconcileIndexNowEligibility(t.Context(), &ReconcileIndexNowEligibilityRequest{After: now.Add(-time.Hour), Through: now})
			require.ErrorIs(t, err, tc.wantError)
			require.NotContains(t, fmt.Sprint(err), "private")
			if tc.wantError != nil {
				require.False(t, result.Complete)
				require.Equal(t, now.Add(-time.Hour), result.Checkpoint)
			} else {
				require.True(t, result.Complete)
				require.Equal(t, now, result.Checkpoint)
			}
			if tc.want {
				require.Len(t, notifier.calls, 1)
				wantURL := "https://example.com/blog/due"
				if tc.noCMS {
					wantURL = "https://example.com" + uri
				}
				require.Equal(t, []string{wantURL}, notifier.calls[0])
				require.Equal(t, "pending", result.IndexNow.Status)
			} else {
				require.Empty(t, notifier.calls)
			}
		})
	}
}

func TestIndexNowEligibilityWindowValidation(t *testing.T) {
	now := cmsIndexNowTime()
	for _, tc := range []struct {
		name           string
		after, through time.Time
		limit          int
		nilRequest     bool
		want           error
	}{
		{name: "explicit activation window", after: now.Add(-time.Hour), through: now},
		{name: "maximum full day", after: now.Add(-24 * time.Hour), through: now},
		{name: "nil request", nilRequest: true, want: post.ErrPostBadRequest},
		{name: "missing checkpoint", through: now, want: ErrIndexNowReconciliationInvalid},
		{name: "missing upper bound", after: now.Add(-time.Hour), want: ErrIndexNowReconciliationInvalid},
		{name: "empty window", after: now, through: now, want: ErrIndexNowReconciliationInvalid},
		{name: "reversed window", after: now, through: now.Add(-time.Hour), want: ErrIndexNowReconciliationInvalid},
		{name: "future upper bound", after: now.Add(-time.Hour), through: now.Add(time.Nanosecond), want: ErrIndexNowReconciliationInvalid},
		{name: "more than a full day", after: now.Add(-24*time.Hour - time.Nanosecond), through: now, want: ErrIndexNowReconciliationInvalid},
		{name: "negative scan limit", after: now.Add(-time.Hour), through: now, limit: -1, want: ErrIndexNowReconciliationInvalid},
		{name: "oversized scan limit", after: now.Add(-time.Hour), through: now, limit: 10001, want: ErrIndexNowReconciliationInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			posts := &eligibilityPosts{cmsIndexNowPostPort: &cmsIndexNowPostPort{}}
			notifier := &cmsIndexNowSubmitter{t: t, status: 200}
			service := eligibilityService(posts, &eligibilitySEO{}, notifier, func(context.Context, string, time.Time) (bool, error) { return true, nil })
			request := &ReconcileIndexNowEligibilityRequest{After: tc.after, Through: tc.through, Limit: tc.limit}
			if tc.nilRequest {
				request = nil
			}
			result, err := service.ReconcileIndexNowEligibility(t.Context(), request)
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, notifier.calls)
			if err == nil {
				require.True(t, result.Complete)
				require.Equal(t, tc.through, result.Checkpoint)
			} else {
				require.Nil(t, result)
			}
		})
	}
}

func TestIndexNowCombinedScanBoundsAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name       string
		cms, seo   int
		limit      int
		fault      string
		incomplete bool
	}{
		{name: "multiple native-sized pages", cms: 105, seo: 105, limit: 210},
		{name: "CMS exact bound empty SEO", cms: 100, limit: 100},
		{name: "combined inventory exceeds bound", cms: 100, seo: 1, limit: 100, incomplete: true},
		{name: "CMS inventory exceeds bound", cms: 101, limit: 100, incomplete: true},
		{name: "SEO inventory exceeds remaining bound", cms: 1, seo: 100, limit: 100, incomplete: true},
		{name: "CMS read failure", cms: 1, fault: "cms", incomplete: true},
		{name: "SEO read failure after CMS candidates", cms: 1, fault: "seo", incomplete: true},
		{name: "CMS cursor does not advance", fault: "cms-cursor", incomplete: true},
		{name: "SEO cursor does not advance", fault: "seo-cursor", incomplete: true},
		{name: "missing native CMS source", fault: "missing-cms", incomplete: true},
		{name: "missing native SEO source", fault: "missing-seo", incomplete: true},
		{name: "missing current availability callback", fault: "missing-eligible", incomplete: true},
		{name: "notification failure retains checkpoint", cms: 1, fault: "notification", incomplete: true},
		{name: "completion audit failure retains checkpoint", cms: 1, fault: "audit", incomplete: true},
		{name: "duplicate CMS canonical identity", cms: 2, fault: "duplicate", incomplete: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			posts := &eligibilityPosts{cmsIndexNowPostPort: &cmsIndexNowPostPort{}}
			for i := 0; i < tc.cms; i++ {
				item := cmsIndexNowPost(fmt.Sprintf("article-story-%d", i))
				item.Id = fmt.Sprintf("post-%04d", i)
				posts.items = append(posts.items, *item)
			}
			entries := &eligibilitySEO{}
			for i := 0; i < tc.seo; i++ {
				entries.items = append(entries.items, seo.SitemapItem{ID: fmt.Sprintf("seo-%04d", i), URI: fmt.Sprintf("/static-%d", i), PublishedAt: cmsIndexNowTime().Format(time.RFC3339)})
			}
			notifier := &cmsIndexNowSubmitter{t: t, status: 200}
			service := eligibilityService(posts, entries, notifier, func(context.Context, string, time.Time) (bool, error) { return true, nil })
			switch tc.fault {
			case "cms":
				posts.failure = errors.New("private database failure")
			case "seo":
				entries.failure = errors.New("private database failure")
			case "cms-cursor":
				posts.response = &post.ListIndexNowPostsResponse{NextID: "stuck"}
			case "seo-cursor":
				entries.response = &seo.ListIndexNowSitemapItemsResponse{NextID: "stuck"}
			case "missing-cms":
				service.indexNow.config.PublicationSource = nil
			case "missing-seo":
				service.indexNow.config.SitemapSource = nil
			case "missing-eligible":
				service.indexNow.config.EligibleURL = nil
			case "notification":
				notifier.failure = errors.New("private transport failure")
			case "audit":
				notifier.failure = &indexnow.AuditError{Dispatched: true}
			case "duplicate":
				posts.items[1].UrlFriendlyId = posts.items[0].UrlFriendlyId
			}
			request := &ReconcileIndexNowEligibilityRequest{After: cmsIndexNowTime().Add(-2 * time.Hour), Through: cmsIndexNowTime(), Limit: tc.limit}
			result, err := service.ReconcileIndexNowEligibility(t.Context(), request)
			if tc.incomplete {
				require.ErrorIs(t, err, ErrIndexNowReconciliationIncomplete)
				require.False(t, result.Complete)
				require.Equal(t, request.After, result.Checkpoint)
				if tc.fault != "notification" && tc.fault != "audit" {
					require.Empty(t, notifier.calls, "all source reads must complete before dispatch")
				}
			} else {
				require.NoError(t, err)
				require.True(t, result.Complete)
				require.Equal(t, tc.cms+tc.seo, result.Inspected)
				require.Equal(t, request.Through, result.Checkpoint)
				require.Len(t, notifier.calls, 1)
				require.Len(t, notifier.calls[0], tc.cms+tc.seo)
				require.True(t, sort.StringsAreSorted(notifier.calls[0]))
			}
			require.NotContains(t, fmt.Sprint(err), "private")
		})
	}
}

func TestIndexNowCombinedDelayedWindowAndBoundaries(t *testing.T) {
	now := cmsIndexNowTime()
	for _, tc := range []struct {
		name        string
		publication time.Time
		want        bool
	}{
		{name: "exclusive After", publication: now.Add(-2 * time.Hour)},
		{name: "inclusive Through", publication: now.Add(-time.Hour), want: true},
		{name: "after historical Through", publication: now.Add(-time.Hour + time.Nanosecond)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := cmsIndexNowPost("article-due")
			item.PublishedAt = tc.publication.Format(time.RFC3339Nano)
			posts := &eligibilityPosts{cmsIndexNowPostPort: &cmsIndexNowPostPort{}, items: []post.Post{*item}}
			notifier := &cmsIndexNowSubmitter{t: t, status: 200}
			through := now.Add(-time.Hour)
			service := eligibilityService(posts, &eligibilitySEO{}, notifier, func(_ context.Context, _ string, at time.Time) (bool, error) {
				require.Equal(t, through, at, "current wall clock cannot replace the pass boundary")
				return true, nil
			})
			result, err := service.ReconcileIndexNowEligibility(t.Context(), &ReconcileIndexNowEligibilityRequest{After: now.Add(-2 * time.Hour), Through: through})
			require.NoError(t, err)
			require.True(t, result.Complete)
			require.Equal(t, tc.want, len(notifier.calls) == 1)
		})
	}
}

func TestIndexNowCombinedRetainsProtocolRetryDelay(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		retryAfter time.Duration
		audit      bool
		complete   bool
	}{
		{name: "throttled longer than runner cadence", status: 429, retryAfter: 30 * time.Minute},
		{name: "protocol duration is not truncated", status: 429, retryAfter: time.Duration(math.MaxInt64)},
		{name: "audit failure retains throttling delay", status: 429, retryAfter: 30 * time.Minute, audit: true},
		{name: "ordinary failure no synthetic delay", status: 503},
		{name: "negative duration adds no delay", status: 429, retryAfter: -time.Second},
		{name: "pending receipt never implies retry", status: 202, retryAfter: 30 * time.Minute, complete: true},
		{name: "acknowledged receipt never implies retry", status: 200, retryAfter: 30 * time.Minute, complete: true},
		{name: "pending completion audit failure preserves observed delay", status: 202, retryAfter: 30 * time.Minute, audit: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := cmsIndexNowPost("article-due")
			posts := &eligibilityPosts{cmsIndexNowPostPort: &cmsIndexNowPostPort{}, items: []post.Post{*item}}
			notifier := &cmsIndexNowSubmitter{t: t, status: tc.status, retryAfter: tc.retryAfter}
			if tc.audit {
				notifier.failure = errors.Join(&indexnow.AuditError{Dispatched: true}, errors.New("private audit diagnostic"))
			}
			service := eligibilityService(posts, &eligibilitySEO{}, notifier, func(context.Context, string, time.Time) (bool, error) { return true, nil })
			req := &ReconcileIndexNowEligibilityRequest{After: cmsIndexNowTime().Add(-2 * time.Hour), Through: cmsIndexNowTime()}
			result, err := service.ReconcileIndexNowEligibility(t.Context(), req)
			require.Equal(t, tc.complete, result.Complete)
			require.Len(t, notifier.calls, 1)
			if tc.complete {
				require.NoError(t, err)
				require.Equal(t, req.Through, result.Checkpoint)
			} else {
				require.ErrorIs(t, err, ErrIndexNowReconciliationIncomplete)
				require.Equal(t, req.After, result.Checkpoint)
				var retry interface {
					error
					RetryDelay() time.Duration
				}
				if tc.retryAfter > 0 {
					require.ErrorAs(t, err, &retry)
					require.Equal(t, tc.retryAfter, retry.RetryDelay())
					require.NotContains(t, err.Error(), "private")
				} else {
					require.False(t, errors.As(err, &retry))
				}
			}
			require.Equal(t, max(time.Duration(0), tc.retryAfter), result.IndexNow.RetryAfter)
			wire, marshalErr := json.Marshal(result.IndexNow)
			require.NoError(t, marshalErr)
			baseline := *result.IndexNow
			baseline.RetryAfter = 0
			baselineWire, marshalErr := json.Marshal(&baseline)
			require.NoError(t, marshalErr)
			require.Equal(t, string(baselineWire), string(wire), "protocol scheduling metadata cannot change mutation response JSON")
		})
	}
}
