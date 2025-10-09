package user

import (
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/common"
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

const (
	// APIUsersV2Prefix base URI prefix for all v2 users routes
	APIUsersV2Prefix = common.ApiV1UriPrefix + "/v2/users"

	// APIUsersV2IDVariable URI variable used to get user ID out of URI
	APIUsersV2IDVariable = "/{userID}"

	// APIUsersV2NanoIDVariable URI variable used to get nano ID out of URI
	APIUsersV2NanoIDVariable = "/{nanoID}"

	// APIUsersV2ExtensionKeyVariable URI variable for extension key
	APIUsersV2ExtensionKeyVariable = "/{extensionKey}"
)

var (
	// APIUsersV2Profile URI prefix for user profile calls
	APIUsersV2Profile = APIUsersV2IDVariable + "/profile"

	// APIUsersV2MicroProfile URI prefix for user micro profile calls
	APIUsersV2MicroProfile = APIUsersV2IDVariable + "/micro"

	// APIUsersV2Status URI prefix for user status update calls
	APIUsersV2Status = APIUsersV2IDVariable + "/status"

	// APIUsersV2Roles URI prefix for user role management calls
	APIUsersV2Roles = APIUsersV2IDVariable + "/roles"

	// APIUsersV2VerifyEmail URI prefix for email verification calls
	APIUsersV2VerifyEmail = APIUsersV2IDVariable + "/verify/email"

	// APIUsersV2UnverifyEmail URI prefix for email unverification calls
	APIUsersV2UnverifyEmail = APIUsersV2IDVariable + "/unverify/email"

	// APIUsersV2VerifyPhone URI prefix for phone verification calls
	APIUsersV2VerifyPhone = APIUsersV2IDVariable + "/verify/phone"

	// APIUsersV2Login URI prefix for recording login calls
	APIUsersV2Login = APIUsersV2IDVariable + "/login"

	// APIUsersV2Extensions URI prefix for extension management calls
	APIUsersV2Extensions = APIUsersV2IDVariable + "/extensions"

	// APIUsersV2ExtensionValue URI prefix for specific extension value calls
	APIUsersV2ExtensionValue = APIUsersV2Extensions + APIUsersV2ExtensionKeyVariable

	// APIUsersV2PersonalInfo URI prefix for personal info update calls
	APIUsersV2PersonalInfo = APIUsersV2IDVariable + "/personal-info"

	// APIUsersV2Validate URI prefix for user validation calls
	APIUsersV2Validate = APIUsersV2IDVariable + "/validate"

	// APIUsersV2SearchByExtension URI prefix for searching by extension
	APIUsersV2SearchByExtension = "/search/extensions"

	// APIUsersV2BulkStatus URI prefix for bulk status updates
	APIUsersV2BulkStatus = "/bulk/status"

	// APIUsersV2ByRoles URI prefix for getting users by roles
	APIUsersV2ByRoles = "/by-roles"

	// APIUsersV2ByStatus URI prefix for getting users by status
	APIUsersV2ByStatus = "/by-status"

	// APIUsersV2ByNanoID URI prefix for getting user by nano ID
	APIUsersV2ByNanoID = "/nano" + APIUsersV2NanoIDVariable

	// APIUsersV2ByEmail URI prefix for getting user by email
	APIUsersV2ByEmail = "/by-email"
)

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

	// Basic CRUD operations
	usersAdminOnlyRoutes.HandleFunc("", request.Handler.CreateUser).Methods(http.MethodPost, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc("", request.Handler.GetUsers).Methods(http.MethodGet, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc(APIUsersV2IDVariable, request.Handler.GetUserByID).Methods(http.MethodGet, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc(APIUsersV2IDVariable, request.Handler.UpdateUser).Methods(http.MethodPatch, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc(APIUsersV2IDVariable, request.Handler.DeleteUser).Methods(http.MethodDelete, http.MethodOptions)

	// Alternative ID lookups
	usersAdminOnlyRoutes.HandleFunc(APIUsersV2ByNanoID, request.Handler.GetUserByNanoID).Methods(http.MethodGet, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc(APIUsersV2ByEmail, request.Handler.GetUserByEmail).Methods(http.MethodGet, http.MethodOptions)

	// Profile endpoints
	usersAdminOnlyRoutes.HandleFunc(APIUsersV2Profile, request.Handler.GetUserProfile).Methods(http.MethodGet, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc(APIUsersV2MicroProfile, request.Handler.GetUserMicroProfile).Methods(http.MethodGet, http.MethodOptions)

	// Status management
	usersAdminOnlyRoutes.HandleFunc(APIUsersV2Status, request.Handler.UpdateUserStatus).Methods(http.MethodPatch, http.MethodOptions)

	// Role management
	usersAdminOnlyRoutes.HandleFunc(APIUsersV2Roles, request.Handler.AddUserRole).Methods(http.MethodPost, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc(APIUsersV2Roles, request.Handler.RemoveUserRole).Methods(http.MethodDelete, http.MethodOptions)

	// Verification management
	usersAdminOnlyRoutes.HandleFunc(APIUsersV2VerifyEmail, request.Handler.VerifyUserEmail).Methods(http.MethodPost, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc(APIUsersV2UnverifyEmail, request.Handler.UnverifyUserEmail).Methods(http.MethodPost, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc(APIUsersV2VerifyPhone, request.Handler.VerifyUserPhone).Methods(http.MethodPost, http.MethodOptions)

	// Login recording
	usersAdminOnlyRoutes.HandleFunc(APIUsersV2Login, request.Handler.RecordUserLogin).Methods(http.MethodPost, http.MethodOptions)

	// Extension management
	usersAdminOnlyRoutes.HandleFunc(APIUsersV2Extensions, request.Handler.SetUserExtension).Methods(http.MethodPost, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc(APIUsersV2ExtensionValue, request.Handler.GetUserExtension).Methods(http.MethodGet, http.MethodOptions)

	// Personal info update
	usersAdminOnlyRoutes.HandleFunc(APIUsersV2PersonalInfo, request.Handler.UpdateUserPersonalInfo).Methods(http.MethodPatch, http.MethodOptions)

	// Validation
	usersAdminOnlyRoutes.HandleFunc(APIUsersV2Validate, request.Handler.ValidateUser).Methods(http.MethodGet, http.MethodOptions)

	// Search and filtering
	usersAdminOnlyRoutes.HandleFunc(APIUsersV2SearchByExtension, request.Handler.SearchUsersByExtension).Methods(http.MethodGet, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc(APIUsersV2ByRoles, request.Handler.GetUsersByRoles).Methods(http.MethodGet, http.MethodOptions)
	usersAdminOnlyRoutes.HandleFunc(APIUsersV2ByStatus, request.Handler.GetUsersByStatus).Methods(http.MethodGet, http.MethodOptions)

	// Bulk operations
	usersAdminOnlyRoutes.HandleFunc(APIUsersV2BulkStatus, request.Handler.BulkUpdateUsersStatus).Methods(http.MethodPost, http.MethodOptions)

	// Apply admin-only middleware
	usersAdminOnlyRoutes.Use(request.AdminOnlyMiddleware)

	// Authenticated routes (if needed for self-service operations)
	// Uncomment and customise as needed:
	// usersAuthenticatedRoutes := httpRouter.PathPrefix(APIUsersV2Prefix).Subrouter()
	// usersAuthenticatedRoutes.HandleFunc("/me", request.Handler.GetCurrentUser).Methods(http.MethodGet, http.MethodOptions)
	// usersAuthenticatedRoutes.HandleFunc("/me", request.Handler.UpdateCurrentUser).Methods(http.MethodPatch, http.MethodOptions)
	// usersAuthenticatedRoutes.Use(request.AuthenticatedMiddleware)
}
