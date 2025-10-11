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
	SearchUsersByExtension(w http.ResponseWriter, r *http.Request)
	BulkUpdateUsersStatus(w http.ResponseWriter, r *http.Request)
	GetUsersByRoles(w http.ResponseWriter, r *http.Request)
	GetUsersByStatus(w http.ResponseWriter, r *http.Request)
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

	// AuthenticatedMiddleware middleware used for authenticated users
	AuthenticatedMiddleware mux.MiddlewareFunc
}

// AttachRoutes attaches user handler to corresponding routes on router
func AttachRoutes(request *AttachRoutesRequest) {
	httpRouter := request.Router.GetRouter()

	// Admin-only routes for full user management
	usersAdminOnlyRoutes := httpRouter.PathPrefix(APIUsersV2Prefix).Subrouter()
	usersAdminOnlyRoutes.HandleFunc("", request.Handler.CreateUser).Methods(http.MethodPost, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("", request.Handler.GetUsers).Methods(http.MethodGet, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("/{userID}", request.Handler.GetUserByID).Methods(http.MethodGet, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("/{userID}", request.Handler.UpdateUser).Methods(http.MethodPatch, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("/{userID}", request.Handler.DeleteUser).Methods(http.MethodDelete, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("/nano/{nanoID}", request.Handler.GetUserByNanoID).Methods(http.MethodGet, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("/email/{email}", request.Handler.GetUserByEmail).Methods(http.MethodGet, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("/{userID}/profile", request.Handler.GetUserProfile).Methods(http.MethodGet, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("/{userID}/micro", request.Handler.GetUserMicroProfile).Methods(http.MethodGet, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("/{userID}/status", request.Handler.UpdateUserStatus).Methods(http.MethodPatch, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("/{userID}/roles", request.Handler.AddUserRole).Methods(http.MethodPost, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("/{userID}/roles", request.Handler.RemoveUserRole).Methods(http.MethodDelete, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("/{userID}/verify/email", request.Handler.VerifyUserEmail).Methods(http.MethodPost, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("/{userID}/unverify/email", request.Handler.UnverifyUserEmail).Methods(http.MethodPost, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("/{userID}/verify/phone", request.Handler.VerifyUserPhone).Methods(http.MethodPost, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("/{userID}/recordings/login", request.Handler.RecordUserLogin).Methods(http.MethodPost, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("/{userID}/extensions", request.Handler.SetUserExtension).Methods(http.MethodPost, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("/{userID}/extensions/{extensionKey}", request.Handler.GetUserExtension).Methods(http.MethodGet, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("/{userID}/personal-info", request.Handler.UpdateUserPersonalInfo).Methods(http.MethodPatch, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("/{userID}/validate", request.Handler.ValidateUser).Methods(http.MethodGet, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("/search/extensions", request.Handler.SearchUsersByExtension).Methods(http.MethodGet, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("/by-roles", request.Handler.GetUsersByRoles).Methods(http.MethodGet, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("/by-status", request.Handler.GetUsersByStatus).Methods(http.MethodGet, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("/bulk/status", request.Handler.BulkUpdateUsersStatus).Methods(http.MethodPost, http.MethodOptions)
	usersAdminOnlyRoutes.Use(request.AdminOnlyMiddleware)

	// Authenticated routes (if needed for self-service operations)
	// Uncomment and customise as needed:
	// usersAuthenticatedRoutes := httpRouter.PathPrefix(APIUsersV2Prefix).Subrouter()
	// usersAuthenticatedRoutes.HandleFunc("/me", request.Handler.GetCurrentUser).Methods(http.MethodGet, http.MethodOptions)
	// usersAuthenticatedRoutes.HandleFunc("/me", request.Handler.UpdateCurrentUser).Methods(http.MethodPatch, http.MethodOptions)
	// usersAuthenticatedRoutes.Use(request.AuthenticatedMiddleware)
}
