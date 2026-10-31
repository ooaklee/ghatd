package post

import (
	"context"
	"testing"
	"time"

	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/common"
	"github.com/stretchr/testify/require"
)

func TestPublicOverviewPublicationBoundaries(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, date, deleted       string
		restricted, nilPost, want bool
	}{
		{name: "due UTC", date: "2026-10-30T12:00:00Z", want: true},
		{name: "due platform", date: "2026-10-30T12:00:00.000000000", want: true},
		{name: "equivalent offset", date: "2026-10-30T13:00:00+01:00", want: true},
		{name: "future nanosecond", date: "2026-10-30T12:00:00.000000001Z"},
		{name: "empty draft"}, {name: "malformed", date: "invalid"}, {name: "date only", date: "2026-10-30"},
		{name: "zero", date: "0001-01-01T00:00:00Z"},
		{name: "overflow offset hour", date: "2026-10-30T12:00:00+24:00"},
		{name: "overflow offset minute", date: "2026-10-30T12:00:00+01:60"},
		{name: "comma fractional second", date: "2026-10-30T12:00:00,1Z"},
		{name: "restricted", date: "2026-10-30T12:00:00Z", restricted: true},
		{name: "deleted", date: "2026-10-30T12:00:00Z", deleted: "deleted"},
		{name: "nil record", nilPost: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := &Post{PublishedAt: tc.date, DeletedAt: tc.deleted}
			if tc.restricted {
				item.Visibility = []string{"member"}
			}
			if tc.nilPost {
				item = nil
			}
			require.Equal(t, tc.want, publicOverviewPostAt(item, now))
		})
	}
}

func TestLatestPublicFeedsDefendAgainstUnsafeAdapterRecords(t *testing.T) {
	for _, operation := range []string{"post overview", "notification overview"} {
		for _, tc := range []struct {
			name, date                         string
			deleted, restricted, authenticated bool
			count                              int
		}{
			{name: "published", date: "2020-01-01T00:00:00.000000000", count: 1},
			{name: "restricted", date: "2020-01-01T00:00:00Z", restricted: true},
			{name: "authenticated identity grants no preview", date: "2020-01-01T00:00:00Z", restricted: true, authenticated: true},
			{name: "deleted", date: "2020-01-01T00:00:00Z", deleted: true},
			{name: "future", date: "2999-01-01T00:00:00Z"},
			{name: "malformed", date: "0000"}, {name: "draft"}, {name: "zero", date: "0001-01-01T00:00:00Z"},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				item := Post{Id: "selected", Title: "Selected title", Type: PostTypeArticle, UrlFriendlyId: "article-selected", PublishedAt: tc.date}
				if tc.deleted {
					item.DeletedAt = "deleted"
				}
				if tc.restricted {
					item.Visibility = []string{"members"}
				}
				source := &postFailurePort{rows: []Post{item}, total: 1}
				service := NewService(source, nil)
				ctx := context.Background()
				if tc.authenticated {
					ctx = accesshelpers.TransitAuthenticatedWith(accesshelpers.TransitWith(ctx, "verified-caller"), true)
				}
				if operation == "post overview" {
					response, err := service.GetLatestPostsByType(ctx, &GetLatestPostsByTypeRequest{Types: "article", Limit: 5})
					require.NoError(t, err)
					require.Len(t, response.Overviews, tc.count)
				} else {
					response, err := service.GetLatestNotificationOverviews(ctx, &common.GetLatestNotificationOverviewsRequest{UserID: "selected-recipient", Kinds: string(common.NotificationKindPostArticle), Limit: 5})
					require.NoError(t, err)
					require.Len(t, response.Overviews, tc.count)
				}
				require.True(t, source.query.IsPublished)
				require.True(t, source.query.IsNotDeleted)
				require.Equal(t, item, source.rows[0], "projecting a public feed must not mutate adapter-owned records")
			})
		}
	}
}
