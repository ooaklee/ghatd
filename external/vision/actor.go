package vision

import (
	"context"
	"net/http"
	"reflect"
	"strings"

	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
)

// nilVisionDependency detects absent requests and typed-nil ports without invoking
// their methods. A nonnil adapter remains responsible for its own configuration.
func nilVisionDependency(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	}
	return false
}

// validateVisionActorRequest requires explicit authentication before decoding.
// Route admission and manager ownership checks still determine authorization.
func validateVisionActorRequest(request *http.Request) error {
	if request == nil || request.URL == nil {
		return ErrVisionInvalidPayload
	}
	if err := request.Context().Err(); err != nil {
		return err
	}
	if strings.TrimSpace(accesshelpers.AcquireAuthenticatedUserIDFrom(request.Context())) == "" {
		return ErrVisionUserIDIsRequired
	}
	return nil
}

// visionActorMatchesContext rejects contradictory published identity evidence.
// Bare-context calls are trusted in-process workflows: supplying an actor never
// grants permission, so the integrating manager must authorize before invoking.
func visionActorMatchesContext(ctx context.Context, actorID string) bool {
	if ctx == nil || actorID == "" || strings.TrimSpace(actorID) != actorID {
		return false
	}
	if ctx.Value(accesshelpers.RequestorUserKey) != nil {
		user := accesshelpers.AcquireUserFrom(ctx)
		if user == nil || user.ID != actorID || accesshelpers.AcquireAuthenticatedUserIDFrom(ctx) != actorID {
			return false
		}
	}
	if ctx.Value(accesshelpers.RequestorKey) != nil || ctx.Value(accesshelpers.RequestorAuthenticatedKey) != nil {
		return accesshelpers.AcquireAuthenticatedUserIDFrom(ctx) == actorID
	}
	return true
}

// validateEntry rejects unusable service inputs before logging or persistence.
// Native cancellation errors are preserved for shared response manifests.
func (s *Service) validateEntry(ctx context.Context, request any) error {
	if ctx == nil || nilVisionDependency(request) {
		return ErrVisionInvalidPayload
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || nilVisionDependency(s.VisionRepository) {
		return ErrVisionUnavailable
	}
	if s.Config == nil {
		return ErrVisionConfigNotSet
	}
	return nil
}
