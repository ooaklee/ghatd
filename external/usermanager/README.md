# User Manager

The `usermanager` package is a high-level, full-stack service designed to simplify managing users and their associated data. It acts as an orchestrator, integrating with packages such as `user/v2`, `group`, and `contacter` to provide a unified API for common user-centric operations.

This guide gives an overview of the `usermanager` architecture, its key features, and how to interact with its API.

## Architecture

The package follows a standard layered architecture, consistent with other services in this project. Its primary role is to orchestrate calls to other services rather than managing its own data directly.

1.  **Routes (`routes.go`)**: Defines the HTTP API endpoints under the `/api/v1/ums` prefix, and organises them across four security tiers: open (rate-limited), authenticated, admin-only, and active-only.
2.  **Handler (`handler.go`)**: Acts as the intermediary between the HTTP transport layer and the business logic. It's responsible for parsing requests, calling the service layer, and formatting responses.
3.  **Service (`service.go`, `service.group.go`, `service.group.admin.go`)**: Contains the core business logic. The service layer makes calls to other downstream services (e.g., `UserService`, `GroupService`, `ContacterService`) to gather and assemble the data needed to fulfil a request.
4.  **Request/Response (`request.go`, `response.go`)**: Defines the data structures for API communication, ensuring a clear and consistent contract for clients.
5.  **Fender (`fender.go`)**: Maps incoming HTTP requests to the appropriate request structs, and applies authorisation checks where needed.

Unlike other packages, the `usermanager` doesn't have its own repository or database collection — it exclusively orchestrates data from other services.

## Key Features

The `usermanager` is designed to streamline complex user-related workflows into single API calls.

-   **Expanded User Profiles**: Fetch a complete user profile, including enriched data from multiple sources. A single request can return a user's core details alongside group memberships, team information, and more.
-   **Group Memberships**: A dedicated `/me/memberships` endpoint lets the authenticated user retrieve their group memberships, with optional filtering by group type (e.g. `TEAM`, `ORGANISATION`, etc.), the ability to include descendant groups, and optional root-prefix naming (`prefix_name`).
-   **Group Management**: Active users can create groups, add/remove members, and update group ownership — all through the `usermanager` surface. The service layer applies its own authorisation logic (admin flag or group access) on top of the route-level middleware.
-   **Communication Management**: Integrates with the `contacter` service to manage communication preferences and history (admin-only).
-   **Reminder Management**: Optionally integrates with the `reminder` service so users can create and manage scheduled reminders, while admin/service views can inspect reminder volume, due reminders, and stats.

## API Endpoints

All endpoints are prefixed with `/api/v1/ums`. They are split across four security tiers:

### Open (rate-limited)
-   `POST /api/v1/ums/comms`: Submit a new comms entry (e.g. a contact form submission).

### Authenticated
These require a valid JWT or API token.

-   `GET /api/v1/ums/me`: Get the authenticated user's own profile.
-   `DELETE /api/v1/ums/me`: Permanently delete the authenticated user's account.
-   `GET /api/v1/ums/me/micro`: Get a lightweight micro-profile for the authenticated user.
-   `GET /api/v1/ums/me/enriched`: Get an enriched profile, optionally including all group memberships. Supports `include_all_groups` and `prefix_name`.
-   `GET /api/v1/ums/me/memberships`: Get the authenticated user's group memberships. Supports `group_type`, `include_descendants`, and `prefix_name`.
-   `GET /api/v1/ums/me/groups`: Get a paginated list of groups the authenticated user belongs to. Supports `prefix_name`.
-   `GET /api/v1/ums/me/reminders`: List reminders for the authenticated user. Supports `status`, `target_type`, `target_id`, `page`, and `per_page`.
-   `POST /api/v1/ums/me/reminders`: Create a reminder for the authenticated user.
-   `GET /api/v1/ums/me/reminders/{reminderID}`: Get one reminder owned by the authenticated user.
-   `PATCH /api/v1/ums/me/reminders/{reminderID}`: Update one reminder owned by the authenticated user.
-   `DELETE /api/v1/ums/me/reminders/{reminderID}`: Delete one reminder owned by the authenticated user.
-   `POST /api/v1/ums/me/reminders/{reminderID}/disable`: Disable one reminder owned by the authenticated user.
-   `GET /api/v1/ums/users/{userId}`: Get a user by their ID.
-   `GET /api/v1/ums/groups/{groupID}`: Get enriched detail for a specific group (members, owner, etc.). Supports `prefix_name`.
-   `GET /api/v1/ums/groups/{groupID}/stats`: Get statistics for a specific group. Supports `prefix_name`.

### Optional custom middleware for `GET /me`

`AttachRoutesRequest` supports an optional middleware field named
`CustomMeEndpointValidApiTokenOrJWTMiddleware`.

-   If provided, it is used only for `GET /api/v1/ums/me`.
-   If omitted (`nil`), route setup falls back to `ValidApiTokenOrJWTMiddleware` for that endpoint.

This is useful when `/me` needs endpoint-specific auth error response handling while the rest of authenticated routes continue to use the standard middleware.

For implementation details and usage examples, see [`external/accessmanager/middleware/custom_middleware.go`](../accessmanager/middleware/custom_middleware.go).

### Quick note on `prefix_name`

When `prefix_name=true`, child group names are returned in a root-prefixed format (for example `school/year-10`).

-   `name`: may be prefixed for readability.
-   `raw_name`: remains the original, non-prefixed value.

Think of it as breadcrumbs for group names, but without the crumbs in your keyboard.

### Admin-only
-   `GET /api/v1/ums/comms`: List all comms entries.
-   `GET /api/v1/ums/comms/stats`: Get comms statistics.
-   `PUT /api/v1/ums/comms/{id}`: Update a comms entry.
-   `POST /api/v1/ums/users/{userId}/notifications`: Send a notification to a user when notifier is wired.
-   `GET /api/v1/ums/reminders`: List reminders across users. Supports `user_id`, `status`, `target_type`, `target_id`, `page`, and `per_page`.
-   `GET /api/v1/ums/reminders/stats`: Get aggregate reminder stats for admin overview pages. Supports optional `user_id` and `user_ids`.
-   `GET /api/v1/ums/reminders/due`: Get reminders that are ready for scheduler processing. Supports optional `user_id`, `user_ids`, `due_before`, and `limit`; if neither `user_id` nor `user_ids` is provided, it retrieves due reminders for everyone.

### Reminder list authorisation

UMS uses one `ListReminders` service method for both `GET /me/reminders` and
`GET /reminders`. The service checks the requesting user through `UserService`.
If the requester is not an admin, the request is locked to their own `UserID`
even if a different `user_id` filter is supplied. Admin users may omit
`user_id` to list across users, or pass it to inspect one user's reminders.

### Active users only
These require the user to be both authenticated and active.

-   `PATCH /api/v1/ums/me`: Update the authenticated user's own profile.
-   `POST /api/v1/ums/groups`: Create a new group.
-   `PUT /api/v1/ums/groups/{groupID}/owner`: Update the owner of a group.
-   `POST /api/v1/ums/groups/{groupID}/members`: Add a member to a group.
-   `DELETE /api/v1/ums/groups/{groupID}/members/{memberID}`: Remove a member from a group.

## Configuration and Initialisation

To use the `usermanager` service, you must initialise it and attach its routes to a configured `ghatdRouter`. This is typically done during your application's startup process.

For a detailed guide on setting up the main application router, please see the [Router Package Getting Started documentation](../router/README.md).

If the application uses `external/starter/v0`, starter creates the reminder
service and attaches it to User Manager by default. The manual example below is
for projects composing `usermanager` directly.

**Example Initialisation:**

```go
import (
	"net/http"

	"github.com/ooaklee/ghatd/external/reminder"
	"github.com/ooaklee/ghatd/external/router"
	userv2 "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/ghatd/external/usermanager"
	"github.com/ooaklee/ghatd/external/validator"
	// ... import other required services (group, audit, etc.)
)

func main() {
	// ... assume ghatdRouter is already initialised as per the router documentation
	var ghatdRouter *router.Router

	// Initialise a validator
	validator := validator.New()

	// Initialise downstream services
userService := userv2.NewService(...)
	groupService := group.NewService(...)
	contacterService := contacter.NewService(...)
	auditService := audit.NewService(...)
	apiTokenService := apitoken.NewService(...)
	reminderService := reminder.NewService(...)

	// Create the usermanager service
	umsService := usermanager.NewService(&usermanager.NewServiceRequest{
		UserService:      userService,
		ApiTokenService:  apiTokenService,
		AuditService:     auditService,
		ContacterService: contacterService,
	})

	// Add optional services
	umsService.WithGroupService(groupService)
	umsService.WithReminderService(reminderService)

	// Create the usermanager handler
	umsHandler := usermanager.NewHandler(&usermanager.NewHandlerRequest{
		Service:   umsService,
		Validator: validator,
		// ... other options like ErrorMaps — build with errormanifest.Composer
		//     (see github.com/ooaklee/ghatd/external/errormanifest)
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
		// Optional: custom middleware for GET /api/v1/ums/me only.
		CustomMeEndpointValidApiTokenOrJWTMiddleware: mockMiddleware,
		RateLimitOrActiveMiddleware:        mockMiddleware,
	})

	// ... set up and start the HTTP server with ghatdRouter
}
```
