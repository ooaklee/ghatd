package user

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router"
)

// UserHandler interface defines expected methods for valid user handler
type UserHandler interface {
	// CreateUser serves the user creation HTTP endpoint: implementations decode and
	// validate the request, invoke user creation, and write the created user or an
	// error response.
	CreateUser(w http.ResponseWriter, r *http.Request)
	// GetUserByID serves the user-by-ID retrieval HTTP endpoint: implementations
	// decode the request, load the matching user, and write it or an error
	// response.
	GetUserByID(w http.ResponseWriter, r *http.Request)
	// GetUserByNanoID serves the user-by-nano-ID retrieval HTTP endpoint:
	// implementations decode the request, load the matching user, and write it or
	// an error response.
	GetUserByNanoID(w http.ResponseWriter, r *http.Request)
	// GetUserByEmail serves the user-by-email retrieval HTTP endpoint:
	// implementations decode the request, resolve the account by email, and write
	// it or an error response.
	GetUserByEmail(w http.ResponseWriter, r *http.Request)
	// UpdateUser serves the user update HTTP endpoint: implementations decode and
	// validate the request, apply the update, and write the updated user or an
	// error response.
	UpdateUser(w http.ResponseWriter, r *http.Request)
	// DeleteUser serves the user deletion HTTP endpoint: implementations decode the
	// request, remove the account, and write a no-content success or an error
	// response.
	DeleteUser(w http.ResponseWriter, r *http.Request)
	// GetUsers serves the HTTP endpoint that retrieves multiple users. It maps the
	// request into filters and pagination, delegates to the service, and writes the
	// matching users, with pagination metadata when requested, via w.
	GetUsers(w http.ResponseWriter, r *http.Request)
	// UpdateUserStatus serves the HTTP endpoint for user status transitions. It
	// validates the request, delegates to the status manager, and writes the
	// updated user via w; responses are marked no-store.
	UpdateUserStatus(w http.ResponseWriter, r *http.Request)
	// AddUserRole serves the HTTP endpoint that adds a role to a user. It validates
	// the request, delegates to the role manager, and writes the updated user via
	// w; responses are marked no-store.
	AddUserRole(w http.ResponseWriter, r *http.Request)
	// RemoveUserRole serves the HTTP endpoint that removes a role from a user. It
	// validates the request, delegates to the role manager, and writes the updated
	// user via w; responses are marked no-store.
	RemoveUserRole(w http.ResponseWriter, r *http.Request)
	// VerifyUserEmail serves the HTTP endpoint that marks a user's email as
	// verified. It validates the request, delegates to the service, and writes the
	// updated user via w.
	VerifyUserEmail(w http.ResponseWriter, r *http.Request)
	// UnverifyUserEmail serves the HTTP endpoint that marks a user's email as
	// unverified. It validates the request, delegates to the service, and writes
	// the updated user via w.
	UnverifyUserEmail(w http.ResponseWriter, r *http.Request)
	// VerifyUserPhone serves the HTTP endpoint that marks a user's phone as
	// verified. It validates the request, delegates to the service, and writes the
	// updated user via w.
	VerifyUserPhone(w http.ResponseWriter, r *http.Request)
	// RecordUserLogin serves the HTTP endpoint that records a user login event,
	// updating the user's last-login timestamp. It validates the request, delegates
	// to the service, and writes the updated user via w.
	RecordUserLogin(w http.ResponseWriter, r *http.Request)
	// GetUserProfile serves the HTTP endpoint that retrieves a user's full profile
	// representation. It validates the request, delegates to the service, and
	// writes the profile via w.
	GetUserProfile(w http.ResponseWriter, r *http.Request)
	// GetUserMicroProfile serves the HTTP endpoint that retrieves a user's reduced
	// micro profile representation. It validates the request, delegates to the
	// service, and writes the micro profile via w.
	GetUserMicroProfile(w http.ResponseWriter, r *http.Request)
	// SetUserExtension serves the HTTP endpoint that sets a user extension field
	// value. It validates the request carrying the user ID, key and value,
	// delegates to the service, and writes the updated user via w.
	SetUserExtension(w http.ResponseWriter, r *http.Request)
	// GetUserExtension serves the HTTP endpoint that retrieves a user extension
	// field value. It validates the request carrying the user ID and key, delegates
	// to the service, and writes the key/value response via w.
	GetUserExtension(w http.ResponseWriter, r *http.Request)
	// UpdateUserPersonalInfo serves the HTTP endpoint that updates a user's
	// personal information fields. It validates the request, delegates to the
	// service, and writes the updated user via w.
	UpdateUserPersonalInfo(w http.ResponseWriter, r *http.Request)
	// ValidateUser serves the HTTP endpoint that validates a user by ID, reporting
	// whether the account is valid and any validation errors. It delegates to the
	// service and writes the validation result via w.
	ValidateUser(w http.ResponseWriter, r *http.Request)
	// BulkUpdateUsersStatus serves the HTTP endpoint that applies a desired status
	// to multiple user IDs. It validates the request, delegates to the status
	// manager, and writes the per-batch outcome via w; responses are marked
	// no-store.
	BulkUpdateUsersStatus(w http.ResponseWriter, r *http.Request)
	// GetUserStats serves the HTTP endpoint that retrieves aggregated platform user
	// statistics. It validates the request, delegates to the service, and writes
	// the stats response via w.
	GetUserStats(w http.ResponseWriter, r *http.Request)
	// GetUserConfigs serves the HTTP endpoint that retrieves supported user config
	// presets and capabilities, including the default config type. It delegates to
	// the service and writes the response via w.
	GetUserConfigs(w http.ResponseWriter, r *http.Request)
}

// APIUsersV2Prefix base URI prefix for all v2 users routes
const APIUsersV2Prefix = "/api/v2/users"

// AttachRoutesRequest holds everything needed to attach user routes to router
type AttachRoutesRequest struct {
	// Router main router being served by API
	Router *router.Router

	// Handler valid user handler
	Handler UserHandler

	// AdminOnlyMiddleware middleware used to lock endpoints down to admin only
	AdminOnlyMiddleware mux.MiddlewareFunc
}

// AttachRoutes registers administrator-session user management routes.
// AdminOnlyMiddleware is required; validate the registry before serving.
func AttachRoutes(request *AttachRoutesRequest) {

	// Admin-only routes for full user management
	usersAdminOnlyRoutes := request.Router.NewRouteGroup(APIUsersV2Prefix, router.AdminSession, request.AdminOnlyMiddleware)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "", Operation: "user.CreateUser", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.CreateUser)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "", Operation: "user.GetUsers", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetUsers)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/stats", Operation: "user.GetUserStats", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetUserStats)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/configs", Operation: "user.GetUserConfigs", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetUserConfigs)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{userID}", Operation: "user.GetUserByID", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetUserByID)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{userID}", Operation: "user.UpdateUser", Methods: []string{http.MethodPatch, http.MethodOptions}}, request.Handler.UpdateUser)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{userID}", Operation: "user.DeleteUser", Methods: []string{http.MethodDelete, http.MethodOptions}}, request.Handler.DeleteUser)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/nano/{nanoID}", Operation: "user.GetUserByNanoID", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetUserByNanoID)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/email/{email}", Operation: "user.GetUserByEmail", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetUserByEmail)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/bulk/status", Operation: "user.BulkUpdateUsersStatus", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.BulkUpdateUsersStatus)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{userID}/profile", Operation: "user.GetUserProfile", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetUserProfile)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{userID}/micro", Operation: "user.GetUserMicroProfile", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetUserMicroProfile)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{userID}/status", Operation: "user.UpdateUserStatus", Methods: []string{http.MethodPatch, http.MethodOptions}}, request.Handler.UpdateUserStatus)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{userID}/roles", Operation: "user.AddUserRole", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.AddUserRole)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{userID}/roles", Operation: "user.RemoveUserRole", Methods: []string{http.MethodDelete, http.MethodOptions}}, request.Handler.RemoveUserRole)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{userID}/verify/email", Operation: "user.VerifyUserEmail", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.VerifyUserEmail)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{userID}/unverify/email", Operation: "user.UnverifyUserEmail", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.UnverifyUserEmail)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{userID}/verify/phone", Operation: "user.VerifyUserPhone", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.VerifyUserPhone)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{userID}/recordings/login", Operation: "user.RecordUserLogin", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.RecordUserLogin)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{userID}/extensions", Operation: "user.SetUserExtension", Methods: []string{http.MethodPost, http.MethodOptions}}, request.Handler.SetUserExtension)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{userID}/extensions/{extensionKey}", Operation: "user.GetUserExtension", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.GetUserExtension)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{userID}/personal-info", Operation: "user.UpdateUserPersonalInfo", Methods: []string{http.MethodPatch, http.MethodOptions}}, request.Handler.UpdateUserPersonalInfo)
	usersAdminOnlyRoutes.Handle(router.RouteDefinition{Path: "/{userID}/validate", Operation: "user.ValidateUser", Methods: []string{http.MethodGet, http.MethodOptions}}, request.Handler.ValidateUser)

}
