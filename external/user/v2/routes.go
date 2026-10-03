package user

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router"
)

// UserHandler interface defines expected methods for valid user handler
type UserHandler interface {
	CreateUser(w http.ResponseWriter, r *http.Request)
	GetUserByID(w http.ResponseWriter, r *http.Request)
	GetUserByNanoID(w http.ResponseWriter, r *http.Request)
	GetUserByEmail(w http.ResponseWriter, r *http.Request)
	UpdateUser(w http.ResponseWriter, r *http.Request)
	DeleteUser(w http.ResponseWriter, r *http.Request)
	GetUsers(w http.ResponseWriter, r *http.Request)
	UpdateUserStatus(w http.ResponseWriter, r *http.Request)
	AddUserRole(w http.ResponseWriter, r *http.Request)
	RemoveUserRole(w http.ResponseWriter, r *http.Request)
	VerifyUserEmail(w http.ResponseWriter, r *http.Request)
	UnverifyUserEmail(w http.ResponseWriter, r *http.Request)
	VerifyUserPhone(w http.ResponseWriter, r *http.Request)
	RecordUserLogin(w http.ResponseWriter, r *http.Request)
	GetUserProfile(w http.ResponseWriter, r *http.Request)
	GetUserMicroProfile(w http.ResponseWriter, r *http.Request)
	SetUserExtension(w http.ResponseWriter, r *http.Request)
	GetUserExtension(w http.ResponseWriter, r *http.Request)
	UpdateUserPersonalInfo(w http.ResponseWriter, r *http.Request)
	ValidateUser(w http.ResponseWriter, r *http.Request)
	BulkUpdateUsersStatus(w http.ResponseWriter, r *http.Request)
	GetUserStats(w http.ResponseWriter, r *http.Request)
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
