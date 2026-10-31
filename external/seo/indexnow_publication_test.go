package seo

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type legacyPublicationSourceRepository struct{ SitemapRepository }

func TestPublicationSourceRejectsUnboundedOrInvalidReads(t *testing.T) {
	for _, tc := range []struct {
		name      string
		req       *ListIndexNowSitemapItemsRequest
		cancelled bool
		missing   bool
	}{
		{name: "legacy adapter has no bounded capability", req: &ListIndexNowSitemapItemsRequest{Limit: 100}},
		{name: "nil repository", req: &ListIndexNowSitemapItemsRequest{Limit: 100}, missing: true},
		{name: "nil request"},
		{name: "zero limit", req: &ListIndexNowSitemapItemsRequest{}},
		{name: "oversized limit", req: &ListIndexNowSitemapItemsRequest{Limit: 101}},
		{name: "negative limit", req: &ListIndexNowSitemapItemsRequest{Limit: -1}},
		{name: "padded cursor", req: &ListIndexNowSitemapItemsRequest{Limit: 100, AfterID: " padded"}},
		{name: "control cursor", req: &ListIndexNowSitemapItemsRequest{Limit: 100, AfterID: "cursor\x00"}},
		{name: "cancelled before adapter", req: &ListIndexNowSitemapItemsRequest{Limit: 100}, cancelled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			if tc.cancelled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			service := NewService(&NewServiceRequest{SitemapRepository: legacyPublicationSourceRepository{}})
			if tc.missing {
				service = NewService(&NewServiceRequest{})
			}
			response, err := service.ListIndexNowSitemapItems(ctx, tc.req)
			require.Nil(t, response)
			if tc.cancelled {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.ErrorIs(t, err, ErrSitemapPublicationScanUnavailable)
			}
		})
	}
}
