package starter

import (
	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/accessmanager"
	amiddleware "github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/ooaklee/ghatd/external/billingmanager"
	"github.com/ooaklee/ghatd/external/contentmanager"
	"github.com/ooaklee/ghatd/external/group"
	"github.com/ooaklee/ghatd/external/policy"
	"github.com/ooaklee/ghatd/external/pricer"
	"github.com/ooaklee/ghatd/external/router"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/usermanager"
	"github.com/ooaklee/ghatd/external/vision"
)

// RouteGroup identifies a set of standard API routes that can be skipped
// during AttachDefaultRoutes.
type RouteGroup string

const (
	// RouteGroupPricer identifies the /api/v1/pricing route group.
	RouteGroupPricer RouteGroup = "pricer"
	// RouteGroupPolicy identifies the /api/v1/policies route group.
	RouteGroupPolicy RouteGroup = "policy"
	// RouteGroupUser identifies the /api/v2/users route group.
	RouteGroupUser RouteGroup = "user"
	// RouteGroupGroup identifies the /api/v1/groups route group.
	RouteGroupGroup RouteGroup = "group"
	// RouteGroupAccessManager identifies the /api/v1/ams route group.
	RouteGroupAccessManager RouteGroup = "accessmanager"
	// RouteGroupUserManager identifies the /api/v1/ums route group.
	RouteGroupUserManager RouteGroup = "usermanager"
	// RouteGroupContentManager identifies the /api/v1/cms route group.
	RouteGroupContentManager RouteGroup = "contentmanager"
	// RouteGroupBillingManager identifies the /api/v1/bms route group.
	RouteGroupBillingManager RouteGroup = "billingmanager"
	// RouteGroupVision identifies the /api/v1/visions route group.
	RouteGroupVision RouteGroup = "vision"
)

// AttachDefaultRoutesRequest holds the router and starter Stack needed to
// attach every standard GHATD API route group. Groups listed in Skip are
// omitted; their handler may be nil.
type AttachDefaultRoutesRequest struct {
	Router *router.Router
	Stack  *Stack
	Skip   []RouteGroup
}

// AttachDefaultRoutes attaches standard GHATD API routes to the given router
// using the handlers and middleware from Stack. It validates that the request,
// router, stack, and handlers are non-nil. It also validates that every
// non-skipped route group has a non-nil handler and that any middleware needed
// by remaining groups is present.
//
// AttachDefaultRoutes only attaches API routes - it does not attach SPA
// routes, auth verify/CORS middleware, or router bootstrap. Those remain
// host-owned.
func AttachDefaultRoutes(r *AttachDefaultRoutesRequest) error {
	if r == nil {
		return ErrNilAttachDefaultRoutesRequest
	}
	if r.Router == nil {
		return ErrNilRouter
	}
	if r.Stack == nil {
		return ErrNilStack
	}
	if r.Stack.Handlers == nil {
		return ErrNilHandlers
	}

	skip, err := newRouteGroupSkipSet(r.Skip)
	if err != nil {
		return err
	}

	if err := validateAttachDefaultRouteHandlers(r.Stack.Handlers, skip); err != nil {
		return err
	}

	requirements := newRouteMiddlewareRequirements(skip)
	var mw *amiddleware.Suite
	if requirements.any() {
		if r.Stack.Middleware == nil {
			return ErrNilMiddleware
		}
		if r.Stack.Middleware.AccessManager == nil {
			return ErrNilMiddlewareSuite
		}
		mw = r.Stack.Middleware.AccessManager
		if err := validateAttachDefaultRouteMiddleware(requirements, mw); err != nil {
			return err
		}
	}

	if !skip[RouteGroupPricer] {
		pricer.AttachRoutes(&pricer.AttachRoutesRequest{
			Router:              r.Router,
			Handler:             r.Stack.Handlers.Pricer,
			AdminOnlyMiddleware: mw.AdminOnly,
		})
	}

	if !skip[RouteGroupPolicy] {
		policy.AttachRoutes(&policy.AttachRoutesRequest{
			Router:  r.Router,
			Handler: r.Stack.Handlers.Policy,
		})
	}

	if !skip[RouteGroupUser] {
		userv2.AttachRoutes(&userv2.AttachRoutesRequest{
			Router:              r.Router,
			Handler:             r.Stack.Handlers.User,
			AdminOnlyMiddleware: mw.AdminOnly,
		})
	}

	if !skip[RouteGroupGroup] {
		group.AttachRoutes(&group.AttachRoutesRequest{
			Router:              r.Router,
			Handler:             r.Stack.Handlers.Group,
			AdminOnlyMiddleware: mw.AdminOnly,
		})
	}

	if !skip[RouteGroupAccessManager] {
		accessmanager.AttachRoutes(&accessmanager.AttachRoutesRequest{
			Router:                             r.Router,
			Handler:                            r.Stack.Handlers.AccessManager,
			ActiveOnlyMiddleware:               mw.ActiveOnly,
			ActiveValidApiTokenOrJWTMiddleware: mw.ActiveValidApiTokenOrJWT,
			HardenedRateLimitMiddleware:        mw.HardenedRateLimit,
		})
	}

	if !skip[RouteGroupUserManager] {
		usermanager.AttachRoutes(&usermanager.AttachRoutesRequest{
			Router:                                       r.Router,
			Handler:                                      r.Stack.Handlers.UserManager,
			AuthenticatedMiddleware:                      mw.Authenticated,
			ActiveOnlyMiddleware:                         mw.ActiveOnly,
			AdminOnlyMiddleware:                          mw.AdminOnly,
			AdminApiTokenOrJWTMiddleware:                 mw.AdminApiTokenOrJWT,
			ActiveValidApiTokenOrJWTMiddleware:           mw.ActiveValidApiTokenOrJWT,
			ValidApiTokenOrJWTMiddleware:                 mw.ActiveValidApiTokenOrAuthenticated,
			RateLimitOrActiveMiddleware:                  mw.RateLimitOrActive,
			CustomMeEndpointValidApiTokenOrJWTMiddleware: mw.CustomMeEndpointValidApiTokenOrJWT,
		})
	}

	if !skip[RouteGroupContentManager] {
		contentmanager.AttachRoutes(&contentmanager.AttachRoutesRequest{
			Router:                                 r.Router,
			Handler:                                r.Stack.Handlers.ContentManager,
			MiddlewareAdminApiTokenOrJwtRequired:   mw.AdminApiTokenOrJWT,
			RateLimitOrActiveMiddleware:            mw.RateLimitOrActive,
			MiddlewareValidApiTokenOrJWTMiddleware: mw.ActiveValidApiTokenOrJWT,
		})
	}

	if !skip[RouteGroupBillingManager] {
		billingmanager.AttachRoutes(&billingmanager.AttachRoutesRequest{
			Router:                        r.Router,
			Handler:                       r.Stack.Handlers.BillingManager,
			MiddlewareAdminOnlyMiddleware: mw.AdminApiTokenOrJWT,
			MiddlewareActiveValidApiTokenOrJWTMiddleware: mw.ActiveValidApiTokenOrJWT,
		})
	}

	if !skip[RouteGroupVision] {
		vision.AttachRoutes(&vision.AttachRoutesRequest{
			Router:                  r.Router,
			Handler:                 r.Stack.Handlers.Vision,
			AdminOnlyMiddleware:     mw.AdminOnly,
			AuthenticatedMiddleware: mw.Authenticated,
		})
	}

	return nil
}

// newRouteGroupSkipSet deduplicates the skip list and validates each group is known.
func newRouteGroupSkipSet(groups []RouteGroup) (map[RouteGroup]bool, error) {
	skip := make(map[RouteGroup]bool, len(groups))
	for _, group := range groups {
		if !isKnownRouteGroup(group) {
			return nil, newErrUnknownRouteGroup(group)
		}
		skip[group] = true
	}

	return skip, nil
}

// isKnownRouteGroup reports whether the given group is a valid RouteGroup.
func isKnownRouteGroup(group RouteGroup) bool {
	switch group {
	case RouteGroupPricer,
		RouteGroupPolicy,
		RouteGroupUser,
		RouteGroupGroup,
		RouteGroupAccessManager,
		RouteGroupUserManager,
		RouteGroupContentManager,
		RouteGroupBillingManager,
		RouteGroupVision:
		return true
	default:
		return false
	}
}

// validateAttachDefaultRouteHandlers checks that non-skipped route groups have a non-nil handler.
func validateAttachDefaultRouteHandlers(handlers *Handlers, skip map[RouteGroup]bool) error {
	if !skip[RouteGroupPricer] && handlers.Pricer == nil {
		return ErrMissingPricerHandler
	}
	if !skip[RouteGroupPolicy] && handlers.Policy == nil {
		return ErrMissingPolicyHandler
	}
	if !skip[RouteGroupUser] && handlers.User == nil {
		return ErrMissingUserHandler
	}
	if !skip[RouteGroupGroup] && handlers.Group == nil {
		return ErrMissingGroupHandler
	}
	if !skip[RouteGroupAccessManager] && handlers.AccessManager == nil {
		return ErrMissingAccessManagerHandler
	}
	if !skip[RouteGroupUserManager] && handlers.UserManager == nil {
		return ErrMissingUserManagerHandler
	}
	if !skip[RouteGroupContentManager] && handlers.ContentManager == nil {
		return ErrMissingContentManagerHandler
	}
	if !skip[RouteGroupBillingManager] && handlers.BillingManager == nil {
		return ErrMissingBillingManagerHandler
	}
	if !skip[RouteGroupVision] && handlers.Vision == nil {
		return ErrMissingVisionHandler
	}

	return nil
}

type routeMiddlewareRequirements struct {
	adminOnly                          bool
	authenticated                      bool
	activeOnly                         bool
	activeValidApiTokenOrJWT           bool
	hardenedRateLimit                  bool
	adminApiTokenOrJWT                 bool
	activeValidApiTokenOrAuthenticated bool
	rateLimitOrActive                  bool
	customMeEndpointValidApiTokenOrJWT bool
}

// newRouteMiddlewareRequirements computes which middleware types are needed
// based on the set of skipped route groups.
func newRouteMiddlewareRequirements(skip map[RouteGroup]bool) routeMiddlewareRequirements {
	return routeMiddlewareRequirements{
		adminOnly:                          !skip[RouteGroupPricer] || !skip[RouteGroupUser] || !skip[RouteGroupGroup] || !skip[RouteGroupUserManager] || !skip[RouteGroupVision],
		authenticated:                      !skip[RouteGroupUserManager] || !skip[RouteGroupVision],
		activeOnly:                         !skip[RouteGroupAccessManager] || !skip[RouteGroupUserManager],
		activeValidApiTokenOrJWT:           !skip[RouteGroupAccessManager] || !skip[RouteGroupUserManager] || !skip[RouteGroupContentManager] || !skip[RouteGroupBillingManager],
		hardenedRateLimit:                  !skip[RouteGroupAccessManager],
		adminApiTokenOrJWT:                 !skip[RouteGroupUserManager] || !skip[RouteGroupContentManager],
		activeValidApiTokenOrAuthenticated: !skip[RouteGroupUserManager],
		rateLimitOrActive:                  !skip[RouteGroupUserManager] || !skip[RouteGroupContentManager],
		customMeEndpointValidApiTokenOrJWT: !skip[RouteGroupUserManager],
	}
}

// any reports whether at least one middleware type is required.
func (r routeMiddlewareRequirements) any() bool {
	return r.adminOnly ||
		r.authenticated ||
		r.activeOnly ||
		r.activeValidApiTokenOrJWT ||
		r.hardenedRateLimit ||
		r.adminApiTokenOrJWT ||
		r.activeValidApiTokenOrAuthenticated ||
		r.rateLimitOrActive ||
		r.customMeEndpointValidApiTokenOrJWT
}

// validateAttachDefaultRouteMiddleware ensures every required middleware is set on the suite.
func validateAttachDefaultRouteMiddleware(requirements routeMiddlewareRequirements, mw *amiddleware.Suite) error {
	checks := []struct {
		required bool
		name     string
		ptr      *mux.MiddlewareFunc
	}{
		{requirements.adminOnly, "AdminOnly", &mw.AdminOnly},
		{requirements.authenticated, "Authenticated", &mw.Authenticated},
		{requirements.activeOnly, "ActiveOnly", &mw.ActiveOnly},
		{requirements.activeValidApiTokenOrJWT, "ActiveValidApiTokenOrJWT", &mw.ActiveValidApiTokenOrJWT},
		{requirements.hardenedRateLimit, "HardenedRateLimit", &mw.HardenedRateLimit},
		{requirements.adminApiTokenOrJWT, "AdminApiTokenOrJWT", &mw.AdminApiTokenOrJWT},
		{requirements.activeValidApiTokenOrAuthenticated, "ActiveValidApiTokenOrAuthenticated", &mw.ActiveValidApiTokenOrAuthenticated},
		{requirements.rateLimitOrActive, "RateLimitOrActive", &mw.RateLimitOrActive},
		{requirements.customMeEndpointValidApiTokenOrJWT, "CustomMeEndpointValidApiTokenOrJWT", &mw.CustomMeEndpointValidApiTokenOrJWT},
	}

	for _, c := range checks {
		if c.required && *c.ptr == nil {
			return newErrMissingMiddleware(c.name)
		}
	}

	return nil
}
