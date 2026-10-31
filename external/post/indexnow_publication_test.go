package post

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type legacyPublicationRepository struct{ contenterRepository }

func TestPublicationSourceRejectsUnboundedOrInvalidReads(t *testing.T) {
	for _, tc := range []struct {
		name      string
		req       *ListIndexNowPostsRequest
		cancelled bool
		missing   bool
	}{
		{name: "legacy adapter has no bounded capability", req: &ListIndexNowPostsRequest{Limit: 100}},
		{name: "nil repository", req: &ListIndexNowPostsRequest{Limit: 100}, missing: true},
		{name: "nil request"},
		{name: "zero limit", req: &ListIndexNowPostsRequest{}},
		{name: "oversized limit", req: &ListIndexNowPostsRequest{Limit: 101}},
		{name: "negative limit", req: &ListIndexNowPostsRequest{Limit: -1}},
		{name: "padded cursor", req: &ListIndexNowPostsRequest{Limit: 100, AfterID: " padded"}},
		{name: "control cursor", req: &ListIndexNowPostsRequest{Limit: 100, AfterID: "cursor\x00"}},
		{name: "cancelled before adapter", req: &ListIndexNowPostsRequest{Limit: 100}, cancelled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			if tc.cancelled {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			service := NewService(legacyPublicationRepository{}, nil)
			if tc.missing {
				service = NewService(nil, nil)
			}
			response, err := service.ListIndexNowPosts(ctx, tc.req)
			require.Nil(t, response)
			if tc.cancelled {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.ErrorIs(t, err, ErrPostPublicationScanUnavailable)
			}
		})
	}
}
