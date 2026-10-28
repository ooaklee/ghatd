package contentmanager

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/post"
)

// GetChangelogSitemapItemsRequest retains the article ingestion query shape.
// Limit counts publicly eligible source posts, including posts with invalid
// slugs. URIPathPrefix accepts a clean site-relative directory path; empty uses
// /whats-new/. Prefixes cannot contain traversal, escaped aliases or URL parts.
// ActorID remains transport-excluded and is bound from verified HTTP context.
type GetChangelogSitemapItemsRequest = GetArticleSitemapItemsRequest

// GetChangelogSitemapItemsResponse retains the existing ingestion wire format,
// including processed_articles and seedable_articles counters, for clients
// sharing the article and changelog ingestion flow.
type GetChangelogSitemapItemsResponse = GetArticleSitemapItemsResponse

var changelogSitemapPublicSlug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var changelogSitemapPrefix = regexp.MustCompile(`^/(?:[A-Za-z0-9_-]+/)*[A-Za-z0-9_-]+/?$`)

// GetChangelogSitemapItems returns public, due, non-deleted changelog candidates
// across pages, preserving the complete changelog- slug. It performs no writes;
// HTTP access is protected by the administrator route adapter. Trusted jobs may
// call it without an actor. Missing dependencies fail with unavailable errors.
func (s *Service) GetChangelogSitemapItems(ctx context.Context, req *GetChangelogSitemapItemsRequest) (*GetChangelogSitemapItemsResponse, error) {
	if err := s.validateOperation(ctx, req); err != nil {
		return nil, err
	}
	return getChangelogSitemapItems(ctx, s.postService, req, time.Now().UTC())
}

// parseChangelogSitemapTime supports the stored platform UTC format and
// explicit RFC3339 offsets, without treating a malformed date as published.
func parseChangelogSitemapTime(value string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, nil
	}
	return time.Parse("2006-01-02T15:04:05.999999999", value)
}

// getChangelogSitemapItems retains the article ingestion wire shape without
// writing sitemap rows. Publication and creation dates are compared as instants;
// lexical repository date filters must not discard offset-formatted records.
func getChangelogSitemapItems(ctx context.Context, source postService, req *GetChangelogSitemapItemsRequest, now time.Time) (*GetChangelogSitemapItemsResponse, error) {
	if ctx == nil || req == nil || req.Limit < 0 || req.PerPage < 0 || req.PerPage > 100 {
		return nil, post.ErrPostBadRequest
	}
	prefix := strings.TrimSpace(req.URIPathPrefix)
	if prefix == "" {
		prefix = "/whats-new/"
	}
	if prefix != "/" && !changelogSitemapPrefix.MatchString(prefix) {
		return nil, post.ErrPostBadRequest
	}
	prefix = strings.TrimSuffix(prefix, "/") + "/"
	createdAtFrom := strings.TrimSpace(req.CreatedAtFrom)
	var createdCutoff time.Time
	if createdAtFrom != "" {
		parsed, err := parseChangelogSitemapTime(createdAtFrom)
		if err != nil {
			return nil, post.ErrPostBadRequest
		}
		createdCutoff = parsed
	}
	if nilContentDependency(source) {
		return nil, ErrContentManagerUnavailable
	}
	result := &GetChangelogSitemapItemsResponse{Items: []ArticleSitemapItem{}}
	perPage := 100
	if req.PerPage > 0 {
		perPage = req.PerPage
	}
	if req.Limit > 0 && req.Limit < perPage {
		perPage = req.Limit
	}
	seen := map[string]bool{}
	for page := 1; ; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Keep page size fixed: reducing it for the final partial page changes
		// Mongo's offset and would repeat/omit posts when a limit spans pages.
		response, err := source.GetChangelogItems(ctx, &post.GetChangelogItemsRequest{GetPostsRequest: &post.GetPostsRequest{
			Order: "created_at_asc", PerPage: perPage, Page: page,
			IsNotDeleted: true,
		}})
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if response == nil || response.GetPostsResponse == nil {
			return nil, ErrContentManagerUnavailable
		}
		for _, item := range response.Posts {
			if req.Limit > 0 && result.ProcessedArticles >= req.Limit {
				break
			}
			published, err := parseChangelogSitemapTime(item.PublishedAt)
			if item.Type != post.PostTypeChangelog || item.DeletedAt != "" || len(item.Visibility) != 0 || err != nil || published.IsZero() || published.After(now) {
				continue
			}
			if createdAtFrom != "" {
				created, err := parseChangelogSitemapTime(item.CreatedAt)
				if err != nil || created.IsZero() || created.Before(createdCutoff) {
					continue
				}
			}
			result.ProcessedArticles++
			if !strings.HasPrefix(item.UrlFriendlyId, "changelog-") || !changelogSitemapPublicSlug.MatchString(item.UrlFriendlyId) {
				result.SkippedMissingURLFriendlyID++
				continue
			}
			uri := prefix + item.UrlFriendlyId
			if seen[uri] {
				continue
			}
			seen[uri] = true
			lastMod := published
			if updated, err := parseChangelogSitemapTime(item.UpdatedAt); err == nil && !updated.Before(published) && !updated.After(now) {
				lastMod = updated
			}
			result.Items = append(result.Items, ArticleSitemapItem{
				PostID: item.Id, URLFriendlyID: item.UrlFriendlyId, PublicSlug: item.UrlFriendlyId,
				URI: uri, LastMod: lastMod.UTC().Format("2006-01-02T15:04:05.000000000"),
				PublishedAt: item.PublishedAt, UpdatedAt: item.UpdatedAt,
			})
		}
		if len(response.Posts) == 0 || response.TotalPages <= page || (req.Limit > 0 && result.ProcessedArticles >= req.Limit) {
			break
		}
	}
	result.SeedableArticles = len(result.Items)
	return result, nil
}
