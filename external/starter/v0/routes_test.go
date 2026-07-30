package starter

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/gorilla/mux"
	accessmiddleware "github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/ooaklee/ghatd/external/router"
)

func TestAttachDefaultRoutes_BadRequest(t *testing.T) {
	tests := []struct {
		name    string
		req     *AttachDefaultRoutesRequest
		wantErr error
	}{
		{
			name:    "FAILURE - nil request",
			req:     nil,
			wantErr: ErrNilAttachDefaultRoutesRequest,
		},
		{
			name: "FAILURE - nil Router",
			req: &AttachDefaultRoutesRequest{
				Router: nil,
				Stack:  &Stack{},
			},
			wantErr: ErrNilRouter,
		},
		{
			name: "FAILURE - nil Stack",
			req: &AttachDefaultRoutesRequest{
				Router: router.NewRouter(nil, nil),
				Stack:  nil,
			},
			wantErr: ErrNilStack,
		},
		{
			name: "FAILURE - nil Handlers",
			req: &AttachDefaultRoutesRequest{
				Router: router.NewRouter(nil, nil),
				Stack:  &Stack{},
			},
			wantErr: ErrNilHandlers,
		},
		{
			name: "FAILURE - nil Middleware",
			req: &AttachDefaultRoutesRequest{
				Router: router.NewRouter(nil, nil),
				Stack: &Stack{
					Handlers: validHandlers(t),
				},
			},
			wantErr: ErrNilMiddleware,
		},
		{
			name: "FAILURE - nil Middleware.AccessManager suite",
			req: &AttachDefaultRoutesRequest{
				Router: router.NewRouter(nil, nil),
				Stack: &Stack{
					Handlers:   validHandlers(t),
					Middleware: &Middleware{},
				},
			},
			wantErr: ErrNilMiddlewareSuite,
		},
		{
			name: "FAILURE - unknown skipped route group",
			req: &AttachDefaultRoutesRequest{
				Router: router.NewRouter(nil, nil),
				Stack: &Stack{
					Handlers:   validHandlers(t),
					Middleware: validMiddleware(t),
				},
				Skip: []RouteGroup{"user-manager"},
			},
			wantErr: ErrUnknownRouteGroup,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := AttachDefaultRoutes(tt.req)
			if err == nil {
				t.Fatalf("expected %v, got nil", tt.wantErr)
			}
			if !errors.Is(err, tt.wantErr) && err.Error() != tt.wantErr.Error() {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestAttachDefaultRoutes_BadMissingHandler(t *testing.T) {
	httpRouter := router.NewRouter(nil, nil)

	tests := []struct {
		name     string
		wantErr  error
		mutate   func(*Handlers)
		skipRest []RouteGroup
	}{
		{
			name:    "FAILURE - missing pricer handler",
			wantErr: ErrMissingPricerHandler,
			mutate: func(h *Handlers) {
				h.Pricer = nil
			},
		},
		{
			name:    "FAILURE - missing policy handler",
			wantErr: ErrMissingPolicyHandler,
			mutate: func(h *Handlers) {
				h.Policy = nil
			},
			skipRest: []RouteGroup{RouteGroupPricer, RouteGroupUser, RouteGroupGroup, RouteGroupAccessManager, RouteGroupUserManager, RouteGroupContentManager, RouteGroupBillingManager},
		},
		{
			name:    "FAILURE - missing user handler",
			wantErr: ErrMissingUserHandler,
			mutate: func(h *Handlers) {
				h.User = nil
			},
			skipRest: []RouteGroup{RouteGroupPricer, RouteGroupPolicy, RouteGroupGroup, RouteGroupAccessManager, RouteGroupUserManager, RouteGroupContentManager, RouteGroupBillingManager},
		},
		{
			name:    "FAILURE - missing group handler",
			wantErr: ErrMissingGroupHandler,
			mutate: func(h *Handlers) {
				h.Group = nil
			},
			skipRest: []RouteGroup{RouteGroupPricer, RouteGroupPolicy, RouteGroupUser, RouteGroupAccessManager, RouteGroupUserManager, RouteGroupContentManager, RouteGroupBillingManager},
		},
		{
			name:    "FAILURE - missing accessmanager handler",
			wantErr: ErrMissingAccessManagerHandler,
			mutate: func(h *Handlers) {
				h.AccessManager = nil
			},
			skipRest: []RouteGroup{RouteGroupPricer, RouteGroupPolicy, RouteGroupUser, RouteGroupGroup, RouteGroupUserManager, RouteGroupContentManager, RouteGroupBillingManager},
		},
		{
			name:    "FAILURE - missing usermanager handler",
			wantErr: ErrMissingUserManagerHandler,
			mutate: func(h *Handlers) {
				h.UserManager = nil
			},
			skipRest: []RouteGroup{RouteGroupPricer, RouteGroupPolicy, RouteGroupUser, RouteGroupGroup, RouteGroupAccessManager, RouteGroupContentManager, RouteGroupBillingManager},
		},
		{
			name:    "FAILURE - missing contentmanager handler",
			wantErr: ErrMissingContentManagerHandler,
			mutate: func(h *Handlers) {
				h.ContentManager = nil
			},
			skipRest: []RouteGroup{RouteGroupPricer, RouteGroupPolicy, RouteGroupUser, RouteGroupGroup, RouteGroupAccessManager, RouteGroupUserManager, RouteGroupBillingManager},
		},
		{
			name:    "FAILURE - missing billingmanager handler",
			wantErr: ErrMissingBillingManagerHandler,
			mutate: func(h *Handlers) {
				h.BillingManager = nil
			},
			skipRest: []RouteGroup{RouteGroupPricer, RouteGroupPolicy, RouteGroupUser, RouteGroupGroup, RouteGroupAccessManager, RouteGroupUserManager, RouteGroupContentManager},
		},
		{
			name:    "FAILURE - missing vision handler",
			wantErr: ErrMissingVisionHandler,
			mutate: func(h *Handlers) {
				h.Vision = nil
			},
			skipRest: []RouteGroup{RouteGroupPricer, RouteGroupPolicy, RouteGroupUser, RouteGroupGroup, RouteGroupAccessManager, RouteGroupUserManager, RouteGroupContentManager, RouteGroupBillingManager},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlers := validHandlers(t)
			tt.mutate(handlers)

			err := AttachDefaultRoutes(&AttachDefaultRoutesRequest{
				Router: httpRouter,
				Stack: &Stack{
					Handlers:   handlers,
					Middleware: validMiddleware(t),
				},
				Skip: tt.skipRest,
			})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("expected %v, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestAttachDefaultRoutes_BadMissingMiddleware(t *testing.T) {
	httpRouter := router.NewRouter(nil, nil)

	mw, err := NewMiddleware(validMiddlewareRequest(t))
	if err != nil {
		t.Fatalf("creating middleware: %v", err)
	}
	suite := mw.AccessManager

	tests := []struct {
		name   string
		mutate func(*accessmiddleware.Suite)
		skip   []RouteGroup
	}{
		{
			name: "FAILURE - missing AdminOnly middleware",
			mutate: func(s *accessmiddleware.Suite) {
				s.AdminOnly = nil
			},
			skip: []RouteGroup{RouteGroupAccessManager, RouteGroupUserManager, RouteGroupContentManager, RouteGroupBillingManager},
		},
		{
			name: "FAILURE - missing Authenticated middleware",
			mutate: func(s *accessmiddleware.Suite) {
				s.Authenticated = nil
			},
			skip: []RouteGroup{RouteGroupPricer, RouteGroupPolicy, RouteGroupUser, RouteGroupAccessManager, RouteGroupBillingManager, RouteGroupContentManager},
		},
		{
			name: "FAILURE - missing ActiveOnly middleware",
			mutate: func(s *accessmiddleware.Suite) {
				s.ActiveOnly = nil
			},
			skip: []RouteGroup{RouteGroupPricer, RouteGroupPolicy, RouteGroupUser, RouteGroupGroup, RouteGroupUserManager, RouteGroupContentManager, RouteGroupBillingManager},
		},
		{
			name: "FAILURE - missing ActiveValidApiTokenOrJWT middleware",
			mutate: func(s *accessmiddleware.Suite) {
				s.ActiveValidApiTokenOrJWT = nil
			},
			skip: []RouteGroup{RouteGroupPricer, RouteGroupPolicy, RouteGroupUser, RouteGroupGroup, RouteGroupAccessManager, RouteGroupUserManager, RouteGroupContentManager},
		},
		{
			name: "FAILURE - missing HardenedRateLimit middleware",
			mutate: func(s *accessmiddleware.Suite) {
				s.HardenedRateLimit = nil
			},
			skip: []RouteGroup{RouteGroupPricer, RouteGroupPolicy, RouteGroupUser, RouteGroupGroup, RouteGroupUserManager, RouteGroupContentManager, RouteGroupBillingManager},
		},
		{
			name: "FAILURE - missing AdminApiTokenOrJWT middleware",
			mutate: func(s *accessmiddleware.Suite) {
				s.AdminApiTokenOrJWT = nil
			},
			skip: []RouteGroup{RouteGroupPricer, RouteGroupPolicy, RouteGroupUser, RouteGroupGroup, RouteGroupAccessManager, RouteGroupUserManager, RouteGroupBillingManager},
		},
		{
			name: "FAILURE - missing ActiveValidApiTokenOrAuthenticated middleware",
			mutate: func(s *accessmiddleware.Suite) {
				s.ActiveValidApiTokenOrAuthenticated = nil
			},
			skip: []RouteGroup{RouteGroupPricer, RouteGroupPolicy, RouteGroupUser, RouteGroupGroup, RouteGroupAccessManager, RouteGroupContentManager, RouteGroupBillingManager},
		},
		{
			name: "FAILURE - missing RateLimitOrActive middleware",
			mutate: func(s *accessmiddleware.Suite) {
				s.RateLimitOrActive = nil
			},
			skip: []RouteGroup{RouteGroupPricer, RouteGroupPolicy, RouteGroupUser, RouteGroupGroup, RouteGroupAccessManager, RouteGroupUserManager, RouteGroupBillingManager},
		},
		{
			name: "FAILURE - missing CustomMeEndpointValidApiTokenOrJWT middleware",
			mutate: func(s *accessmiddleware.Suite) {
				s.CustomMeEndpointValidApiTokenOrJWT = nil
			},
			skip: []RouteGroup{RouteGroupPricer, RouteGroupPolicy, RouteGroupUser, RouteGroupGroup, RouteGroupAccessManager, RouteGroupContentManager, RouteGroupBillingManager},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := *suite
			tt.mutate(&s)

			err := AttachDefaultRoutes(&AttachDefaultRoutesRequest{
				Router: httpRouter,
				Stack: &Stack{
					Handlers: validHandlers(t),
					Middleware: &Middleware{
						AccessManager: &s,
					},
				},
				Skip: tt.skip,
			})
			if err == nil {
				t.Fatal("expected error for missing middleware")
			}
			if !strings.Contains(err.Error(), "middleware") {
				t.Fatalf("expected middleware error, got %v", err)
			}
		})
	}
}

func TestAttachDefaultRoutes_GoodAllGroups(t *testing.T) {
	httpRouter := router.NewRouter(nil, nil)

	services, err := NewServices(validServicesRequest(t))
	if err != nil {
		t.Fatalf("creating services: %v", err)
	}
	handlers, err := NewHandlers(validHandlersRequest(t))
	if err != nil {
		t.Fatalf("creating handlers: %v", err)
	}
	middleware, err := NewMiddleware(validMiddlewareRequest(t))
	if err != nil {
		t.Fatalf("creating middleware: %v", err)
	}

	stack := &Stack{
		Handlers:   handlers,
		Middleware: middleware,
		Services:   services,
	}

	err = AttachDefaultRoutes(&AttachDefaultRoutesRequest{
		Router: httpRouter,
		Stack:  stack,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	routeCount := countRoutes(t, httpRouter.GetRouter())
	if routeCount == 0 {
		t.Fatal("expected routes to be registered")
	}
}

func TestAttachDefaultRoutes_GoodPolicyOnlyWithoutMiddleware(t *testing.T) {
	httpRouter := router.NewRouter(nil, nil)
	handlers := validHandlers(t)

	err := AttachDefaultRoutes(&AttachDefaultRoutesRequest{
		Router: httpRouter,
		Stack: &Stack{
			Handlers: &Handlers{
				Policy: handlers.Policy,
			},
		},
		Skip: []RouteGroup{
			RouteGroupPricer,
			RouteGroupUser,
			RouteGroupGroup,
			RouteGroupAccessManager,
			RouteGroupUserManager,
			RouteGroupContentManager,
			RouteGroupBillingManager,
			RouteGroupVision,
		},
	})
	if err != nil {
		t.Fatalf("expected no error when only policy routes are attached without middleware, got %v", err)
	}

	routeCount := countRoutes(t, httpRouter.GetRouter())
	if routeCount == 0 {
		t.Fatal("expected policy routes to be registered")
	}
}

func TestAttachDefaultRoutes_GoodGroupOnlyWithoutAuthenticatedMiddleware(t *testing.T) {
	httpRouter := router.NewRouter(nil, nil)

	middleware, err := NewMiddleware(validMiddlewareRequest(t))
	if err != nil {
		t.Fatalf("creating middleware: %v", err)
	}
	middleware.AccessManager.Authenticated = nil

	handlers := validHandlers(t)
	err = AttachDefaultRoutes(&AttachDefaultRoutesRequest{
		Router: httpRouter,
		Stack: &Stack{
			Handlers: &Handlers{
				Group: handlers.Group,
			},
			Middleware: middleware,
		},
		Skip: []RouteGroup{
			RouteGroupPricer,
			RouteGroupPolicy,
			RouteGroupUser,
			RouteGroupAccessManager,
			RouteGroupUserManager,
			RouteGroupContentManager,
			RouteGroupBillingManager,
			RouteGroupVision,
		},
	})
	if err != nil {
		t.Fatalf("expected no error when only group routes are attached without authenticated middleware, got %v", err)
	}

	routeCount := countRoutes(t, httpRouter.GetRouter())
	if routeCount == 0 {
		t.Fatal("expected group routes to be registered")
	}
}

func TestAttachDefaultRoutes_GoodSkipPricer(t *testing.T) {
	httpRouter := router.NewRouter(nil, nil)

	handlers, err := NewHandlers(validHandlersRequest(t))
	if err != nil {
		t.Fatalf("creating handlers: %v", err)
	}
	middleware, err := NewMiddleware(validMiddlewareRequest(t))
	if err != nil {
		t.Fatalf("creating middleware: %v", err)
	}

	handlerBackup := handlers.Pricer
	handlers.Pricer = nil

	err = AttachDefaultRoutes(&AttachDefaultRoutesRequest{
		Router: httpRouter,
		Stack: &Stack{
			Handlers:   handlers,
			Middleware: middleware,
		},
		Skip: []RouteGroup{RouteGroupPricer},
	})
	if err != nil {
		t.Fatalf("expected no error when skipping pricer with nil handler, got %v", err)
	}

	handlers.Pricer = handlerBackup
}

func TestAttachDefaultRoutes_GoodSkipUserManager(t *testing.T) {
	httpRouter := router.NewRouter(nil, nil)

	handlers, err := NewHandlers(validHandlersRequest(t))
	if err != nil {
		t.Fatalf("creating handlers: %v", err)
	}
	middleware, err := NewMiddleware(validMiddlewareRequest(t))
	if err != nil {
		t.Fatalf("creating middleware: %v", err)
	}

	handlerBackup := handlers.UserManager
	handlers.UserManager = nil

	err = AttachDefaultRoutes(&AttachDefaultRoutesRequest{
		Router: httpRouter,
		Stack: &Stack{
			Handlers:   handlers,
			Middleware: middleware,
		},
		Skip: []RouteGroup{RouteGroupUserManager},
	})
	if err != nil {
		t.Fatalf("expected no error when skipping usermanager with nil handler, got %v", err)
	}

	handlers.UserManager = handlerBackup
}

func TestAttachDefaultRoutes_GoodSkipAll(t *testing.T) {
	httpRouter := router.NewRouter(nil, nil)

	handlers, err := NewHandlers(validHandlersRequest(t))
	if err != nil {
		t.Fatalf("creating handlers: %v", err)
	}

	handlerBackups := *handlers
	*handlers = Handlers{}

	err = AttachDefaultRoutes(&AttachDefaultRoutesRequest{
		Router: httpRouter,
		Stack: &Stack{
			Handlers: handlers,
		},
		Skip: []RouteGroup{
			RouteGroupPricer,
			RouteGroupPolicy,
			RouteGroupUser,
			RouteGroupGroup,
			RouteGroupAccessManager,
			RouteGroupUserManager,
			RouteGroupContentManager,
			RouteGroupBillingManager,
			RouteGroupVision,
		},
	})
	if err != nil {
		t.Fatalf("expected no error when skipping all groups, got %v", err)
	}

	routeCount := countRoutes(t, httpRouter.GetRouter())
	if routeCount != 0 {
		t.Fatalf("expected 0 routes when all groups skipped, got %d", routeCount)
	}

	*handlers = handlerBackups
}

func TestAttachDefaultRoutes_GoodSkipWithoutCustomMeMiddleware(t *testing.T) {
	httpRouter := router.NewRouter(nil, nil)

	middleware, err := NewMiddleware(validMiddlewareRequest(t))
	if err != nil {
		t.Fatalf("creating middleware: %v", err)
	}
	middleware.AccessManager.CustomMeEndpointValidApiTokenOrJWT = nil

	handlers, err := NewHandlers(validHandlersRequest(t))
	if err != nil {
		t.Fatalf("creating handlers: %v", err)
	}

	err = AttachDefaultRoutes(&AttachDefaultRoutesRequest{
		Router: httpRouter,
		Stack: &Stack{
			Handlers:   handlers,
			Middleware: middleware,
		},
		Skip: []RouteGroup{RouteGroupUserManager},
	})
	if err != nil {
		t.Fatalf("expected no error when usermanager is skipped and custom me middleware is nil, got %v", err)
	}

	routeCount := countRoutes(t, httpRouter.GetRouter())
	if routeCount == 0 {
		t.Fatal("expected routes to be registered")
	}
}

func TestAttachDefaultRoutes_GoodSkippedGroupHandlerNotRequired(t *testing.T) {
	httpRouter := router.NewRouter(nil, nil)

	handlers, err := NewHandlers(validHandlersRequest(t))
	if err != nil {
		t.Fatalf("creating handlers: %v", err)
	}
	middleware, err := NewMiddleware(validMiddlewareRequest(t))
	if err != nil {
		t.Fatalf("creating middleware: %v", err)
	}

	nilHandlers := &Handlers{
		Pricer:         handlers.Pricer,
		Policy:         handlers.Policy,
		User:           handlers.User,
		Group:          handlers.Group,
		AccessManager:  handlers.AccessManager,
		ContentManager: handlers.ContentManager,
		BillingManager: handlers.BillingManager,
		Vision:         handlers.Vision,
	}

	err = AttachDefaultRoutes(&AttachDefaultRoutesRequest{
		Router: httpRouter,
		Stack: &Stack{
			Handlers:   nilHandlers,
			Middleware: middleware,
		},
		Skip: []RouteGroup{RouteGroupUserManager},
	})
	if err != nil {
		t.Fatalf("expected no error when usermanager is skipped and handler is nil, got %v", err)
	}
}

func countRoutes(t *testing.T, r *mux.Router) int {
	t.Helper()

	var count int
	walkErr := r.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
		path, err := route.GetPathTemplate()
		if err == nil && path != "" && path != "/v0/health/check" {
			count++
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk error: %v", walkErr)
	}
	return count
}

// validHandlers creates a Handlers container using the existing test helpers.
func validHandlers(t *testing.T) *Handlers {
	t.Helper()
	h, err := NewHandlers(validHandlersRequest(t))
	if err != nil {
		t.Fatalf("creating handlers: %v", err)
	}
	return h
}

// validMiddleware creates a Middleware container using the existing test helpers.
func validMiddleware(t *testing.T) *Middleware {
	t.Helper()
	m, err := NewMiddleware(validMiddlewareRequest(t))
	if err != nil {
		t.Fatalf("creating middleware: %v", err)
	}
	return m
}

// ensure concrete types satisfy interface contracts at compile time.
var _ http.HandlerFunc = http.NotFound
