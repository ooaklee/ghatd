package contentmanager

import (
	"context"
	"reflect"
	"strings"

	accessmanagerhelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/post"
	user "github.com/ooaklee/ghatd/external/user/v2"
)

// nilContentDependency detects missing ports, including typed-nil adapters.
func nilContentDependency(value any) bool {
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

// validateOperation rejects invalid entry state before invoking any dependency.
// Embedded request pointers are checked by each operation before promotion.
func (s *Service) validateOperation(ctx context.Context, request any) error {
	if ctx == nil || nilContentDependency(request) {
		return post.ErrPostBadRequest
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || nilContentDependency(s.postService) {
		return ErrContentManagerUnavailable
	}
	return nil
}

// contentActorMatchesContext keeps explicit actors from contradicting supplied
// authentication evidence. A context without identity evidence is permitted for
// trusted in-process composition; that caller must authenticate before assigning
// ActorID. An ID is never a credential, and an anonymous placeholder is not an actor.
func contentActorMatchesContext(ctx context.Context, actorID string) bool {
	if ctx == nil || strings.TrimSpace(actorID) == "" {
		return false
	}
	if ctx.Value(accessmanagerhelpers.RequestorKey) != nil || ctx.Value(accessmanagerhelpers.RequestorAuthenticatedKey) != nil {
		return accessmanagerhelpers.AcquireAuthenticatedUserIDFrom(ctx) == actorID
	}
	return true
}

// requireAdministrator resolves live authority for the explicit caller. Native
// lookup failures retain their error-map identity; malformed successful results
// are service failures, not evidence of permission or a missing target account.
func (s *Service) requireAdministrator(ctx context.Context, actorID string) error {
	if !contentActorMatchesContext(ctx, actorID) {
		return ErrUnauthorisedCMUser
	}
	if nilContentDependency(s.userService) {
		return ErrContentManagerUnavailable
	}
	response, err := s.userService.GetUserByID(ctx, &user.GetUserByIDRequest{ID: actorID})
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if response == nil || response.User == nil || response.User.ID != actorID {
		return ErrContentManagerUnavailable
	}
	if !response.User.IsAdmin() {
		return ErrUnauthorisedCMUser
	}
	return nil
}

// publicPostQuery restricts a value copy so public reads cannot change a caller's
// reusable query. Opposite visibility flags are cleared to avoid contradictions.
func publicPostQuery(source *post.GetPostsRequest, administrator bool) *post.GetPostsRequest {
	query := post.GetPostsRequest{}
	if source != nil {
		query = *source
	}
	if !administrator {
		query.IsPublished, query.IsNotDeleted = true, true
		query.IsNotPublished, query.IsDeleted = false, false
	}
	return &query
}
