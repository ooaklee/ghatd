package access

import (
	"context"
	"reflect"
	"time"

	"github.com/go-redis/redis/v7"
	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/accessmanager"
	accessmiddleware "github.com/ooaklee/ghatd/external/accessmanager/middleware"
	"github.com/ooaklee/ghatd/external/accesspolicy"
	"github.com/ooaklee/ghatd/external/accesspolicy/adminaccess"
	"github.com/ooaklee/ghatd/external/accesspolicymanager"
	"github.com/ooaklee/ghatd/external/repository"
	"github.com/ooaklee/ghatd/external/router"
)

// RuntimeConfig selects one authority namespace and borrows the host Mongo
// repository. Configure it before attaching routes; System must not come from a
// request, JWT claim, component name or an environment-dependent fallback.
type RuntimeConfig struct {
	System string
	Core   *repository.MongoDbRepository
	Routes *router.Router
	// StartupTimeout bounds storage preparation; nonpositive values use 30s.
	StartupTimeout time.Duration
	// ResourceChecks are copied by the owning route guard.
	ResourceChecks map[string]accessmiddleware.ResourceCheck
}

// Runtime keeps enforcement and administration on one initialized policy store.
// It owns no connection, workers or cleanup. Hosts retain Mongo lifecycle and
// initialize the API-token inventory through its repository before serving.
type Runtime struct {
	system  string
	store   *accesspolicy.MongoStore
	service *accesspolicy.Service
}

// NewRuntime prepares transactional policy storage and installs the fail-closed
// route guard. It creates no grants, users or credentials. A failed constructor
// must abort startup; collection preparation can already have taken place.
func NewRuntime(ctx context.Context, cfg RuntimeConfig) (*Runtime, error) {
	if ctx == nil || cfg.Core == nil || cfg.Routes == nil {
		return nil, accesspolicy.ErrConfiguration
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	timeout := cfg.StartupTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	startup, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	store, err := accesspolicy.NewMongoStore(startup, cfg.Core)
	if err != nil {
		return nil, err
	}
	service, err := accesspolicy.NewService(store, nil)
	if err != nil {
		return nil, err
	}
	guard, err := accessmiddleware.NewRoutePolicyGuard(cfg.System, service, cfg.ResourceChecks)
	if err != nil {
		return nil, err
	}
	if err := store.Initialize(startup); err != nil {
		return nil, err
	}
	if err := guard.Install(cfg.Routes); err != nil {
		return nil, err
	}
	return &Runtime{system: cfg.System, store: store, service: service}, nil
}

// Service exposes enforcement on the same store used for management. A nil
// runtime returns nil, allowing optional composition without creating authority.
func (r *Runtime) Service() *accesspolicy.Service {
	if r == nil {
		return nil
	}
	return r.service
}

// ManagementConfig connects real session and identity owners to policy routes.
// This is trusted startup configuration, never transport-decoded request data.
type ManagementConfig struct {
	Sessions  *accessmanager.Service
	Users     accesspolicymanager.UserReader
	Inventory accesspolicymanager.InventoryPreparer
	// BearerSession must be the owning explicit-bearer middleware. Management
	// routes additionally verify its private marker on every request.
	BearerSession mux.MiddlewareFunc
	// Capabilities explicitly adds scope/permission administration; no grants
	// are inferred from enabling the routes or from administrator roles.
	Capabilities bool
	// AdminAccess nil leaves the browser proof bridge disabled.
	AdminAccess *AdminAccessConfig
}

// AdminAccessConfig opts into the proof-bound browser bridge with existing
// managed Redis and email owners. It never opens a new connection or sender.
type AdminAccessConfig struct {
	Origin, Environment, CookieName, RedisNamespace string
	// Window is an absolute proof lifetime between one and five minutes.
	Window time.Duration
	// DeliveryEnabled is required outside the exact "local" environment.
	DeliveryEnabled bool
	Redis           *redis.Client
	Email           adminaccess.EmailSender
}

// AttachManagement binds all enabled surfaces to the same current-session
// authorizer and initialized policy store. Configuration is validated before
// route registration. A registration failure requires aborting startup: router
// mutations are not transactional and must not be served after an error.
func (r *Runtime) AttachManagement(routes *router.Router, cfg ManagementConfig) error {
	if r == nil || r.store == nil || r.service == nil || routes == nil || cfg.Sessions == nil || nilPort(cfg.Users) || nilPort(cfg.Inventory) || cfg.BearerSession == nil {
		return accesspolicy.ErrConfiguration
	}
	authorize, err := cfg.Sessions.PolicyManagementAuthorizer(r.system)
	if err != nil {
		return err
	}
	manager, err := accesspolicymanager.NewService(accesspolicymanager.Config{
		System: r.system, Store: r.store, Authorize: authorize, Users: cfg.Users, Inventory: cfg.Inventory,
	})
	if err != nil {
		return err
	}
	handler, err := accesspolicymanager.NewHandler(manager)
	if err != nil {
		return err
	}
	var bridge *adminaccess.Bridge
	if c := cfg.AdminAccess; c != nil {
		if c.Window < time.Minute || c.Window > 5*time.Minute || (c.Environment != "local" && !c.DeliveryEnabled) || nilPort(c.Email) {
			return adminaccess.ErrUnavailable
		}
		store, err := adminaccess.NewRedisStore(c.Redis, c.RedisNamespace)
		if err != nil {
			return err
		}
		bridge, err = adminaccess.New(adminaccess.Config{
			Origin: c.Origin, System: r.system, Environment: c.Environment,
			CookieName: c.CookieName, Window: c.Window, Store: store,
			Manager: manager, Authorize: authorize, BearerSession: cfg.BearerSession, Email: c.Email,
		})
		if err != nil {
			return err
		}
	}
	if err := accesspolicymanager.AttachRoutes(routes, handler, cfg.BearerSession); err != nil {
		return err
	}
	if cfg.Capabilities {
		if err := accesspolicymanager.AttachCapabilityRoutes(routes, handler, cfg.BearerSession); err != nil {
			return err
		}
	}
	if bridge != nil {
		return bridge.Attach(routes)
	}
	return nil
}

// nilPort rejects typed-nil dependency pointers before they can cross owning
// interfaces and panic after a caller has already been authenticated.
func nilPort(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
