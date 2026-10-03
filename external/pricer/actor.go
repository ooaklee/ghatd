package pricer

import (
	"context"
	"net/http"
	"reflect"
	"strings"

	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
)

// nilPriceDependency recognizes missing requests and typed-nil adapters without
// invoking them. It does not attempt to validate a configured adapter's internals.
func nilPriceDependency(value any) bool {
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

// validatePriceActorRequest requires explicit authentication before decoding a
// mutation. Route middleware still supplies authorization; an actor ID alone is
// not administrator authority. Anonymous bookkeeping IDs are never actors.
func validatePriceActorRequest(request *http.Request) error {
	if request == nil || request.URL == nil {
		return ErrInvalidPriceQueryParam
	}
	if err := request.Context().Err(); err != nil {
		return err
	}
	if strings.TrimSpace(accesshelpers.AcquireAuthenticatedUserIDFrom(request.Context())) == "" {
		return ErrPriceUserIDRequired
	}
	return nil
}

// priceActorMatchesContext rejects contradictory identity evidence. Trusted
// in-process callers may supply an actor without HTTP context, but are responsible
// for authenticating and authorizing it before invoking this lower-domain service.
func priceActorMatchesContext(ctx context.Context, actorID string) bool {
	if ctx == nil || strings.TrimSpace(actorID) == "" {
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

// validateMutation rejects unusable entry state before logging or persistence.
// Native cancellation errors retain their identity for shared reply error maps.
func (s *Service) validateMutation(ctx context.Context, request any, invalid error) error {
	if ctx == nil || nilPriceDependency(request) {
		return invalid
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || nilPriceDependency(s.PricerRepository) {
		return ErrPricerUnavailable
	}
	return nil
}

// copyMutablePlan detaches the model and cost entries that service normalization
// changes. Nested metadata is passed through unchanged, not deep-cloned: adapters
// must not mutate caller-owned nested maps or other shared payload values.
func copyMutablePlan(plan *PricePlan) *PricePlan {
	if plan == nil {
		return nil
	}
	copy := *plan
	copy.Costs = append([]PriceCost(nil), plan.Costs...)
	return &copy
}

// copyMutableFeature detaches scalar audit fields before service mutation.
// Arbitrary metadata remains read-only and is not recursively cloned.
func copyMutableFeature(feature *PriceFeature) *PriceFeature {
	if feature == nil {
		return nil
	}
	copy := *feature
	return &copy
}

// preserveFeatureAuditMetadata restores server-owned history on a trusted
// replacement. Current update attribution is assigned separately from ActorID.
func preserveFeatureAuditMetadata(feature, existing *PriceFeature) {
	feature.NanoID = existing.NanoID
	feature.CreatedAt, feature.CreatedByID = existing.CreatedAt, existing.CreatedByID
	feature.PublishedAt, feature.PublishedByID = existing.PublishedAt, existing.PublishedByID
	feature.DeletedAt, feature.DeletedByID = existing.DeletedAt, existing.DeletedByID
	feature.UpdatedAt = existing.UpdatedAt
}
