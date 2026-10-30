package contentmanager

import (
	"slices"
	"time"

	"github.com/ooaklee/ghatd/external/post"
)

// ServiceOption configures optional host composition before concurrent use.
// Options must not mutate services after requests start.
type ServiceOption func(*Service)

// WithPublicationClock supplies the clock shared by public visibility and
// notification eligibility. A nil clock keeps the default system clock.
func WithPublicationClock(now func() time.Time) ServiceOption {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

func (s *Service) publicationTime() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}

func publicTimestampAt(value string, now time.Time) bool {
	when, err := parseChangelogSitemapTime(value)
	return err == nil && !when.IsZero() && !when.After(now)
}

// IsPublicPostAt reports whether a post is due, non-deleted and unrestricted at
// the supplied instant. Empty, malformed and zero publication dates fail closed;
// timestamps accept platform UTC values and RFC3339 offsets. It does not decide
// whether a host exposes a route, SEO entry or compiled frontend for that post.
func IsPublicPostAt(item *post.Post, now time.Time) bool {
	return item != nil && item.DeletedAt == "" && len(item.Visibility) == 0 && publicTimestampAt(item.PublishedAt, now)
}

// publicPostProjection owns its slice before filtering or author enrichment.
// Repository totals remain source totals: filtering unsafe adapter records may
// shorten a page, and cannot establish a corrected global count.
func publicPostProjection(source *post.GetPostsResponse, administrator bool, now time.Time) *post.GetPostsResponse {
	result := *source
	result.Posts = slices.Clone(source.Posts)
	if !administrator {
		result.Posts = slices.DeleteFunc(result.Posts, func(item post.Post) bool { return !IsPublicPostAt(&item, now) })
	}
	return &result
}
