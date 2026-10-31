package seo

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// ErrSitemapPublicationScanUnavailable means the configured adapter cannot
// provide a valid bounded inventory page. It contains no storage diagnostics.
var ErrSitemapPublicationScanUnavailable = errors.New("seo: publication scan unavailable")

// ListIndexNowSitemapItemsRequest selects an internal, read-only inventory page.
// AfterID is the last previously observed string ID, not a caller-supplied query.
// Limit must be from one to 100. It is not an HTTP request contract.
type ListIndexNowSitemapItemsRequest struct {
	AfterID string
	Limit   int
}

// ListIndexNowSitemapItemsResponse contains records in ascending ID order.
// NextID is empty only when the bounded read observed no further record; otherwise
// it equals the final returned ID. Pages are current reads, not a snapshot.
type ListIndexNowSitemapItemsResponse struct {
	Items  []SitemapItem
	NextID string
}

// SitemapPublicationSource is the optional bounded read capability used by
// publication reconciliation. Existing CRUD adapters need not implement it.
type SitemapPublicationSource interface {
	ListIndexNowSitemapItems(context.Context, *ListIndexNowSitemapItemsRequest) (*ListIndexNowSitemapItemsResponse, error)
}

// PublicationTime returns this entry's independent public inclusion gate using
// exactly the XML renderer's strict UTC parser. Empty means no gate; a malformed
// nonempty timestamp is an error and must never become immediately eligible.
func (s SitemapItem) PublicationTime() (time.Time, error) {
	return s.publishedAtInstant()
}

func validPublicationScanRequest(ctx context.Context, req *ListIndexNowSitemapItemsRequest) error {
	if ctx == nil || req == nil || req.Limit < 1 || req.Limit > 100 || strings.TrimSpace(req.AfterID) != req.AfterID || strings.ContainsAny(req.AfterID, "\x00\r\n\t") {
		return ErrSitemapPublicationScanUnavailable
	}
	return ctx.Err()
}

// ListIndexNowSitemapItems forwards only to an explicitly bounded repository
// capability. It never falls back to an unbounded GetAllSitemapItems read.
func (s *Service) ListIndexNowSitemapItems(ctx context.Context, req *ListIndexNowSitemapItemsRequest) (*ListIndexNowSitemapItemsResponse, error) {
	if err := validPublicationScanRequest(ctx, req); err != nil {
		return nil, err
	}
	if s == nil || s.SitemapRepository == nil {
		return nil, ErrSitemapPublicationScanUnavailable
	}
	source, ok := s.SitemapRepository.(SitemapPublicationSource)
	if !ok || nilPublicationDependency(source) {
		return nil, ErrSitemapPublicationScanUnavailable
	}
	copyRequest := *req
	return source.ListIndexNowSitemapItems(ctx, &copyRequest)
}

// ListIndexNowSitemapItems reads at most Limit+1 documents using the native
// string _id index. URI/publication values remain unchanged. Invalid/non-string
// IDs fail decoding or validation instead of silently disappearing from a scan.
func (r *Repository) ListIndexNowSitemapItems(ctx context.Context, req *ListIndexNowSitemapItemsRequest) (*ListIndexNowSitemapItemsResponse, error) {
	if err := validPublicationScanRequest(ctx, req); err != nil {
		return nil, err
	}
	if r == nil || nilPublicationDependency(r.Store) {
		return nil, ErrSitemapPublicationScanUnavailable
	}
	collection, err := r.GetSitemapItemsCollection(ctx)
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
		return nil, ErrSitemapPublicationScanUnavailable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = cursor.Close(cleanup)
	}()
	var items []SitemapItem
	if err := r.Store.MapAllInCursorToResult(ctx, cursor, &items, "publication sitemap items"); err != nil {
		return nil, err
	}
	previous := req.AfterID
	for _, item := range items {
		if item.ID == "" || item.ID <= previous || strings.TrimSpace(item.ID) != item.ID || strings.ContainsAny(item.ID, "\x00\r\n\t") {
			return nil, ErrSitemapPublicationScanUnavailable
		}
		previous = item.ID
	}
	if len(items) > req.Limit+1 {
		return nil, ErrSitemapPublicationScanUnavailable
	}
	result := &ListIndexNowSitemapItemsResponse{Items: items}
	if len(items) > req.Limit {
		result.Items = items[:req.Limit]
		result.NextID = result.Items[len(result.Items)-1].ID
	}
	return result, nil
}

func nilPublicationDependency(value any) bool {
	if value == nil {
		return true
	}
	switch reflect.ValueOf(value).Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return reflect.ValueOf(value).IsNil()
	default:
		return false
	}
}
