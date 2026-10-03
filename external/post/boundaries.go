package post

import (
	"context"
	"errors"
	"reflect"
	"slices"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

// nilPostDependency detects typed-nil ports before invoking adapter methods.
func nilPostDependency(value any) bool {
	if value == nil {
		return true
	}
	switch v := reflect.ValueOf(value); v.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return v.IsNil()
	}
	return false
}

// postAbsent accepts only an actual absence sentinel in a bounded single-cause
// chain. Messages, custom Is aliases and joined failures are not proof that a
// resource is absent. In particular, an outage must never permit a create.
func postAbsent(err error) bool {
	for depth := 0; err != nil && depth < 64; depth++ {
		if err == ErrResourceNotFound || err == mongo.ErrNoDocuments {
			return true
		}
		if nilPostDependency(err) {
			return false
		}
		err = errors.Unwrap(err)
	}
	return false
}

// validateOperation rejects invalid input and wiring before logging or I/O.
// Authorization belongs to the manager; this domain validates attribution only.
func (s *Service) validateOperation(ctx context.Context, req any) error {
	if ctx == nil || nilPostDependency(req) {
		return ErrPostBadRequest
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || nilPostDependency(s.contenterRepository) {
		return ErrPostUnavailable
	}
	return nil
}

// copyPost isolates mutable fields from caller- or repository-owned snapshots.
// Adapters may mutate their input without changing the original read result.
func copyPost(post *Post) *Post {
	if post == nil {
		return nil
	}
	copy := *post
	copy.Tags = slices.Clone(post.Tags)
	copy.Visibility = slices.Clone(post.Visibility)
	return &copy
}

// loadPost preserves operational causes and verifies the adapter selected the
// requested resource. The missing sentinel is operation-specific for compatibility.
func (s *Service) loadPost(ctx context.Context, id string, missing error) (*Post, error) {
	post, err := s.contenterRepository.GetPostById(ctx, id)
	if err != nil {
		if postAbsent(err) {
			return nil, missing
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if post == nil || post.Id != id {
		return nil, ErrPostUnavailable
	}
	return copyPost(post), nil
}

// checkSlug is an advisory uniqueness read. Only confirmed absence permits a
// write; a matching selected owner is valid on update. Atomic uniqueness still
// requires a database constraint. No driver error is retried or reconstructed.
func (s *Service) checkSlug(ctx context.Context, slug, selectedID string) error {
	post, err := s.contenterRepository.GetPostByUrlFriendlyId(ctx, slug)
	if err != nil && !postAbsent(err) {
		return err
	}
	if canceled := ctx.Err(); canceled != nil {
		return canceled
	}
	if err != nil {
		return nil
	}
	if post == nil || post.Id == "" || post.UrlFriendlyId != slug {
		return ErrPostUnavailable
	}
	if selectedID != "" && post.Id == selectedID {
		return nil
	}
	return ErrPostAlreadyExistsWithGivenUrlFriendlyId
}
