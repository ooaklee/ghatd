package post

import (
	"context"
	"errors"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ErrPostPublicationScanUnavailable means the configured adapter cannot
// provide a valid bounded inventory page. It contains no storage diagnostics.
var ErrPostPublicationScanUnavailable = errors.New("post: publication scan unavailable")

// ListIndexNowPostsRequest selects an internal, read-only inventory page.
// AfterID is the last previously observed string ID, not a caller-supplied query.
// Limit must be from one to 100. It is not an HTTP request contract.
type ListIndexNowPostsRequest struct {
	AfterID string
	Limit   int
}

// ListIndexNowPostsResponse contains records in ascending ID order.
// NextID is empty only when the bounded read observed no further record; otherwise
// it equals the final returned ID. Pages are current reads, not a snapshot.
type ListIndexNowPostsResponse struct {
	Items  []Post
	NextID string
}

// PostPublicationSource is the optional bounded read capability used by
// publication reconciliation. Existing CRUD adapters need not implement it.
type PostPublicationSource interface {
	ListIndexNowPosts(context.Context, *ListIndexNowPostsRequest) (*ListIndexNowPostsResponse, error)
}

func validPublicationScanRequest(ctx context.Context, req *ListIndexNowPostsRequest) error {
	if ctx == nil || req == nil || req.Limit < 1 || req.Limit > 100 || strings.TrimSpace(req.AfterID) != req.AfterID || strings.ContainsAny(req.AfterID, "\x00\r\n\t") {
		return ErrPostPublicationScanUnavailable
	}
	return ctx.Err()
}

// ListIndexNowPosts forwards only to an explicitly bounded repository
// capability. It never falls back to an unbounded GetPosts read.
func (s *Service) ListIndexNowPosts(ctx context.Context, req *ListIndexNowPostsRequest) (*ListIndexNowPostsResponse, error) {
	if err := validPublicationScanRequest(ctx, req); err != nil {
		return nil, err
	}
	if s == nil || nilPostDependency(s.contenterRepository) {
		return nil, ErrPostPublicationScanUnavailable
	}
	source, ok := s.contenterRepository.(PostPublicationSource)
	if !ok || nilPostDependency(source) {
		return nil, ErrPostPublicationScanUnavailable
	}
	copyRequest := *req
	return source.ListIndexNowPosts(ctx, &copyRequest)
}

// ListIndexNowPosts reads at most Limit+1 documents using the native
// string _id index. Publication/deletion/visibility values remain unchanged. Invalid/non-string
// IDs fail decoding or validation instead of silently disappearing from a scan.
func (r *Repository) ListIndexNowPosts(ctx context.Context, req *ListIndexNowPostsRequest) (*ListIndexNowPostsResponse, error) {
	if err := validPublicationScanRequest(ctx, req); err != nil {
		return nil, err
	}
	if r == nil || nilPostDependency(r.Store) {
		return nil, ErrPostPublicationScanUnavailable
	}
	collection, err := r.GetPostCollection(ctx)
	if err != nil {
		return nil, err
	}
	filter := bson.M{}
	if req.AfterID != "" {
		// $expr compares across BSON types rather than type-bracketing the query.
		// An unsupported ID type beyond the checkpoint must fail decoding.
		filter["$expr"] = bson.M{"$gt": bson.A{"$_id", req.AfterID}}
	}
	cursor, err := r.Store.ExecuteFindCommand(ctx, collection, filter, options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetLimit(int64(req.Limit+1)))
	if err != nil {
		return nil, err
	}
	if cursor == nil {
		return nil, ErrPostPublicationScanUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = cursor.Close(cleanup)
	}()
	var items []Post
	if err := r.Store.MapAllInCursorToResult(ctx, cursor, &items, "publication posts"); err != nil {
		return nil, err
	}
	previous := req.AfterID
	for _, item := range items {
		if item.Id == "" || item.Id <= previous || strings.TrimSpace(item.Id) != item.Id || strings.ContainsAny(item.Id, "\x00\r\n\t") {
			return nil, ErrPostPublicationScanUnavailable
		}
		previous = item.Id
	}
	if len(items) > req.Limit+1 {
		return nil, ErrPostPublicationScanUnavailable
	}
	result := &ListIndexNowPostsResponse{Items: items}
	if len(items) > req.Limit {
		result.Items = items[:req.Limit]
		result.NextID = result.Items[len(result.Items)-1].Id
	}
	return result, nil
}
