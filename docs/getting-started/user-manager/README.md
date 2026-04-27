# User Manager

The `usermanager` package is a high-level, full-stack service designed to simplify managing users and their associated data. It acts as an orchestrator, integrating with various other packages like `user`, `group`, and `contacter` to provide a unified API for common user-centric operations.

This guide gives an overview of the `usermanager` architecture, its key features, and how to interact with its API.

## Architecture

The package follows a standard layered architecture, consistent with other services in this project. Its primary role is to orchestrate calls to other services rather than managing its own data directly.

1.  **Routes (`routes.go`)**: Defines the HTTP API endpoints for user management, like `/api/v1/ums/users/{userId}/profile`, mapping incoming requests to the appropriate handlers.
2.  **Handler (`handler.go`)**: Acts as the intermediary between the HTTP transport layer and the business logic. It's responsible for parsing requests, calling the service layer, and formatting responses.
3.  **Service (`service.go`, `service.group.go`)**: Contains the core business logic. The service layer makes calls to other downstream services (e.g., `UserService`, `GroupService`, `ContacterService`) to gather and assemble the data needed to fulfil a request.
4.  **Request/Response (`request.go`, `response.go`)**: Defines the data structures for API communication, ensuring a clear and consistent contract for clients.
5.  **Fender (`fender.go`)**: An authorisation layer that can be used to secure endpoints, ensuring the authenticated user has the correct permissions for the requested action.

Unlike other packages, the `usermanager` doesn't have its own repository or database collection, as it exclusively deals with orchestrating data from other services.

## Key Features

The `usermanager` is designed to streamline complex user-related workflows into single API calls.

-   **Expanded User Profiles**: Fetch a complete user profile, expanded with data from multiple sources. For example, a single request can return a user's core details alongside a list of all the groups they are a member of.
-   **Simplified Group Management**: Provides intuitive endpoints for managing a user's membership in groups. This includes adding a user to a group, removing them, and listing their current groups, without needing to interact directly with the `group` service.
-   **Communication Management**: Integrates with the `contacter` service to manage a user's communication preferences and history.
-   **Group Management**: Offers secure endpoints for creating groups and managing members/ownership, with service-level authorisation checks.

## API Endpoints

The following are the primary API endpoints provided by the `usermanager` service:

-   `GET /api/v1/ums/users/{userId}/profile`: Retrieves a user's expanded profile, including their group memberships.
-   `GET /api/v1/ums/users/{userId}/groups`: Lists all groups that the specified user is a member of.
-   `POST /api/v1/ums/users/{userId}/groups/{groupId}`: Adds a user to a specified group.
-   `DELETE /api/v1/ums/users/{userId}/groups/{groupId}`: Removes a user from a specified group.
-   `POST /api/v1/ums/groups`: Creates a new group for an authorised requester.

## Configuration and Initialisation

To use the `usermanager` service, you must initialise it and attach its routes to a configured `ghatdRouter`. This is typically done during your application's startup process.

For a detailed guide on setting up the main application router, please see the [Router Package Getting Started documentation](../router/README.md).

**Example Initialisation:**

```go
import (
	"net/http"

	"github.com/ooaklee/ghatd/external/router"
	"github.com/ooaklee/ghatd/external/usermanager"
	"github.com/ooaklee/ghatd/external/validator"
	// ... import other required services (user, group, etc.)
)

func main() {
	// ... assume ghatdRouter is already initialised as per the router documentation
	var ghatdRouter *router.Router

	// Initialise a validator
	validator := validator.New()

	// Initialise downstream services
	userService := user.NewService(...)
	groupService := group.NewService(...)
	contacterService := contacter.NewService(...)
	auditService := audit.NewService(...)
	apiTokenService := apitoken.NewService(...)

	// Create the usermanager service
	umsService := usermanager.NewService(&usermanager.NewServiceRequest{
		UserService:      userService,
		ApiTokenService:  apiTokenService,
		AuditService:     auditService,
		ContacterService: contacterService,
	})

	// Add optional services
	umsService.WithGroupService(groupService)

	// Create the usermanager handler
	umsHandler := usermanager.NewHandler(&usermanager.NewHandlerRequest{
		Service:   umsService,
		Validator: validator,
		// ... other options like ErrorMaps, Environment, etc.
	})

	// Mock middleware for the example
	mockMiddleware := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r)
		})
	}

	// Attach the usermanager routes
	usermanager.AttachRoutes(&usermanager.AttachRoutesRequest{
		Router:                             ghatdRouter,
		Handler:                            umsHandler,
		AuthenticatedMiddleware:            mockMiddleware,
		ActiveOnlyMiddleware:               mockMiddleware,
		AdminOnlyMiddleware:                mockMiddleware,
		ActiveValidApiTokenOrJWTMiddleware: mockMiddleware,
		ValidApiTokenOrJWTMiddleware:       mockMiddleware,
		RateLimitOrActiveMiddleware:        mockMiddleware,
	})

	// ... set up and start the HTTP server with ghatdRouter
}
```
