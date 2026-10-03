package blueprint

import (
	"context"
	"net/http"
	"reflect"
	"strings"

	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
)

// nilBlueprintDependency recognizes typed-nil ports without invoking their methods.
func nilBlueprintDependency(value any) bool {
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

// blueprintActorMatchesContext rejects padded IDs and contradictory published
// identity. Bare-context calls are trusted internal composition: an actor string
// never grants permission, so the integrating manager must authorize the action.
func blueprintActorMatchesContext(ctx context.Context, actorID string) bool {
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

// validateBlueprintHTTPRequest guards mapping before context access or decoding.
// List authorization belongs to route middleware; actor-bearing mappers also
// require explicit verified authentication rather than an anonymous placeholder.
func validateBlueprintHTTPRequest(request *http.Request, actorRequired bool) error {
	if request == nil || request.URL == nil {
		return ErrBlueprintInvalidPayload
	}
	if err := request.Context().Err(); err != nil {
		return err
	}
	if actorRequired && !blueprintActorMatchesContext(request.Context(), accesshelpers.AcquireAuthenticatedUserIDFrom(request.Context())) {
		return ErrBlueprintUserIDIsRequired
	}
	return nil
}

// validateEntry rejects absent inputs and wiring before logging or side effects.
// Cancellation retains its native identity for the shared response manifest.
func (s *Service) validateEntry(ctx context.Context, request any) error {
	if ctx == nil || nilBlueprintDependency(request) {
		return ErrBlueprintInvalidPayload
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || nilBlueprintDependency(s.BlueprintRepository) {
		return ErrBlueprintUnavailable
	}
	return nil
}

// blueprintResult verifies a selected record without treating a malformed
// dependency result as authoritative absence. It cannot prove write durability.
func blueprintResult(ctx context.Context, value *Blueprint, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if value == nil || id == "" || value.ID != id {
		return ErrBlueprintUnavailable
	}
	return nil
}
