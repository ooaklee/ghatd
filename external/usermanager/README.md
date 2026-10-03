# User Manager

The `usermanager` package is a high-level, full-stack service designed to simplify managing users and their associated data. It acts as an orchestrator, integrating with packages such as `user/v2`, `group`, and `contacter` to provide a unified API for common user-centric operations.

This guide gives an overview of the `usermanager` architecture, its key features, and how to interact with its API.

## Architecture

The package follows a standard layered architecture, consistent with other services in this project. Its primary role is to orchestrate calls to other services rather than managing its own data directly.

1.  **Routes (`routes.go`)**: Defines the HTTP API endpoints under the `/api/v1/ums` prefix and organises them into public, special `/me`, authenticated, active-only, admin-only, and admin/service middleware groups.
2.  **Handler (`handler.go`)**: Acts as the intermediary between the HTTP transport layer and the business logic. It's responsible for parsing requests, calling the service layer, and formatting responses.
3.  **Service (`service.go`, `service.group.go`, `service.group.admin.go`)**: Contains the core business logic. The service layer makes calls to other downstream services (e.g., `UserService`, `GroupService`, `ContacterService`) to gather and assemble the data needed to fulfil a request.
4.  **Request/Response (`request.go`, `response.go`)**: Defines the data structures for API communication, ensuring a clear and consistent contract for clients.
5.  **Fender (`fender.go`)**: Maps incoming HTTP requests to the appropriate request structs, and applies authorisation checks where needed.

Unlike other packages, the `usermanager` doesn't have its own repository or database collection — it exclusively orchestrates data from other services.

## Mutation identity boundaries

### Administrative account status

The existing `user/v2` single and bulk status routes now delegate to this manager.
Standard starter composition wires them automatically. For manual composition,
install the access manager as the verifier and this service as the handler port:

```go
manager.WithAdministratorAuthorizer(accessManager)
userHandler.WithStatusManager(manager)
```

`AdministratorAuthorizer` includes both live authorization and its native error
manifests. Use the shared access manager implementation; simply extracting an ID
or trusting token administrator flags is insufficient. It rechecks the live
session, current account type/email revision, ACTIVE status and current roles.
API-only, mixed and anonymous contexts cannot authorize these operations. Route
middleware remains required and is not replaced by manager checks.

`UpdateUserStatus` binds `ActorID` from verified context and calls
`ChangeAccountStatus` with independent `ActorID` and `TargetUserID`. Trusted
in-process callers retain the same verified context; supplying an actor string
alone is not authority. Self-targeted administration is not newly prohibited.
The domain owns configured transition rules and the
[conditional field write](../user/v2/README.md#conditional-account-status).
One `user.status_updated` audit event follows each validated successful receipt,
with actor, target, requested transition and actual resolved destination. Audit
delivery is best effort: an outage emits a diagnostic-free warning and does not
turn an acknowledged write into a retry request. No domain audit is duplicated.

Bulk requests keep their ordered, non-atomic `{updated_count, failed_ids}` data
shape. Initial authority failure returns its mapped error before any write.
Each item then rechecks current authority; item failures, including cancellation
or revocation during the batch, join `failed_ids`. Already acknowledged successes
remain counted and audited. An uncertain failed item may have committed, so this
response is not a rollback guarantee or a reason to replay the entire batch.
No new per-item error schema, batch limit or all-or-nothing transaction is added.

The manager fails closed without the narrow domain capability or verifier.
Native failures flow through the domain/authority manifests and `reply`, with
request context, no-store and last-wins host overrides. Custom verifier error
maps must describe public responses, never embed private diagnostics. A live
authority check is point-in-time; it is not a transaction with the target write
or audit sink. General revocation generations and other user-management command
migrations remain separate work.

### Self-service profile names

**Breaking for custom adapters:** `UpdateUserProfile` now requires the optional
`UserProfileNamesService` capability on its user domain. Implement
`UpdateProfileNames` and the corresponding [conditional repository contract](../user/v2/README.md#conditional-profile-names);
there is no fallback to broad `UpdateUser` writes. In-process callers must retain
verified session or API context, not just provide an `ActorID`.

`PATCH /api/v1/ums/me` keeps its existing `ActiveSessionOrAPI` policy. The manager
binds the self-service actor, rejects mixed/anonymous/bare-ID context, reloads the
ACTIVE account and checks signed session type/email revision. API admission and
credential grants remain the authentication/policy middleware's responsibility;
the context helpers are trusted publishers, not credential verifiers. Account
snapshot comparisons in the domain protect the subsequent name write.

Only `first_name` and `last_name` are editable here. Legacy payload fields such as
`id`, email, status, type, roles and extensions are ignored, never forwarded as
authority or updates. Empty/equal names remain a 200 no-op. Success keeps the
existing user response shape; native failures use the shared manifest and host
overrides. All handler responses are `no-store` and carry request context through
reply. Missing capabilities and malformed receipts fail closed with 503.

After a successful operation, optional best-effort `user.updated` audit attributes
actor and target to the caller without recording names. Audit failure is a fixed
warning, not a rollback or raw diagnostic. No-op requests may also emit this
operation audit. The command does not claim client-version/ABA protection or
atomic API-token revocation; see the domain contract for concurrency limits.

### Other mutation payloads

Account deletion, contact creation/update, group creation/update, member addition,
member-role updates and ownership transfers separate trusted identity from editable
HTTP payloads:

- `DELETE /me` accepts a `reason`; both the actor and deleted account come from
  verified context. An administrator also deletes **their own** account here.
- Contact submissions derive attribution from verified context after decoding.
  Anonymous submissions have no user ID, including when their body supplies one.
  Contact updates bind the record ID from the URL and require a verified actor;
  the existing admin middleware remains mandatory.
- Group mutations obtain the actor from verified context and bind URL-selected
  group/member IDs after decoding. Body-selected `member_id` on member addition,
  `owner_id` on ownership transfer/creation, and `parent_group_id` on creation
  remain supported. The manager still checks admin or group access as applicable.
- Group updates accept `name`, `description`, `email`, `icon`, `visibility`,
  `status` and `extensions`. A body-supplied full `group` record is ignored; it
  cannot redirect an update through a nested ID. Use the dedicated membership
  and ownership endpoints for those changes.

**Compatibility:** the actor-bearing mappers in `fender.go` require both an authenticated
flag and nonempty caller ID. A context ID alone (including an anonymous placeholder)
is insufficient. GHATD authentication middleware publishes both; custom adapters
must publish a trusted authentication result through
[`ContextWithAuthentication`](../accessmanager/middleware/context.go) only after
credential verification. Do not populate authentication state from body/query data.
Unknown fields retain the existing ignore behavior; client-supplied actor IDs and
target overrides do not grant authority. HTTP clients that previously sent full
group records must switch to the editable fields above.

### ActorID migration

The 52 direct caller fields in `request.go` now use `ActorID`. This is a breaking
Go API change: update the former `UserId`/`UserID` actor fields, and
`GetGroupsByUserIDRequest.ID`, in struct literals and selectors. Embedded target
IDs, `FilterUserID(s)`, stored ownership and HTTP target parameters are unchanged.
`MyHandleRequest.ActorID` retains its existing session-only semantics.

See [Request identity and migration](../../docs/how-to/request-identity.md) for
the shared convention, examples, promoted-field hazards and custom middleware
requirements. Direct service calls remain trusted in-process commands, not HTTP
authentication boundaries. Services retain their existing domain authorization;
administrative route middleware is still required where authorization is owned
by the route. `ActorID` by itself does not grant permission.

`GetUserGroupMembershipsRequest` is a self-service request. Internal enrichment
uses target-only lower-domain queries instead of presenting a target as an actor.
The optional-auth contact creation request still embeds the lower contact-domain
attribution field; anonymous submissions remain unattributed. Vision requests
delegated to the lower domain and other manager packages are not renamed by
this migration.

## Notification recipient boundaries

`GET /api/v1/ums/me/notifications/latest` always reads the authenticated caller's
notifications. It accepts `kinds` and `limit`; supplied `user_id`, `user_email`
and admin-mode fields cannot redirect the feed, including for administrators.
The manager resolves the caller's current account email through `UserService`.

Explicit recipient selection belongs on the administrator-session routes:

- `GET /api/v1/ums/notifications/latest?user_id=<recipient-id>`
- `GET /api/v1/ums/notifications/{userId}/latest`
- `GET /api/v1/ums/notifications/latest?user_email=<invite-email>`

The path user ID takes precedence over the query user ID. An explicit email
selects pending invitations by that address, including for recipients without
an account; otherwise the selected account's current email is resolved. Omitted
selectors default to the administrator's own account. These routes continue to
require a session, not an API token. Both route policy and the manager enforce
administrator authority; the manager rechecks a matching live ACTIVE account.
Unavailable or inconsistent account lookups never become authorization grants.

**Go API migration:** trusted in-process callers selecting another recipient must
set `GetLatestNotificationOverviewsRequest.AdminView: true` and supply the verified
administrator's `ActorID`, not the recipient's ID. The zero value is self-service
and ignores embedded recipient selectors. `AdminView` is transport-excluded
intent, not proof of permission. Dependency errors keep their native mappings
through the shared reply/error-manifest writer.

## Self-service display handles

Handles are an optional user-domain capability, not another manager collection.
Apply the [handle index migration and domain configuration](../user/v2/README.md#display-handles)
first, then set `AttachRoutesRequest.EnableHandles: true`. Supply the existing
`ActiveOnlyMiddleware` (session-only, not the API-token-or-JWT alternative),
configure the [shared route-policy evaluator](../router/README.md), and reject
startup if `ValidateRoutePolicies` fails. Missing handlers, middleware or the
PATCH revision evaluator fail registration validation. Existing route inventories
are unchanged when this option is false. Starter exposes the same opt-in as
`AttachDefaultRoutesRequest.EnableUserHandles`.

All three endpoints require a verified live session and current ACTIVE account:

| Method | Path under `/api/v1/ums` | Result |
| --- | --- | --- |
| GET | `/me/handle` | Current name and private lifecycle metadata; strong ETag |
| POST | `/me/handle/validate` | Advisory availability and optional suggestion |
| PATCH | `/me/handle` | Exact-candidate update under mandatory `If-Match` |

The handler uses verified session context to construct `MyHandleRequest.ActorID`.
The manager delegates to optional `UserHandleService`, implemented by `user/v2`.
API tokens, mixed credentials, login/email proofs, anonymous placeholders and
mismatched account/type contexts cannot use this API. Legacy session claims are
accepted only through the existing verified-session compatibility path. A query
or body cannot select another account. This is not an admin rename endpoint.

POST/PATCH accept exactly one case-sensitive, non-null string field:

```json
{"handle":"calm-fox"}
```

Send one `Content-Type: application/json` header (optional UTF-8 charset), an
unencoded body of at most 1024 bytes, and no extra fields. Duplicate keys, target
IDs, malformed/trailing JSON and invalid UTF-8 are rejected. On PATCH, send the
quoted numeric ETag from GET, for example `If-Match: "0"` for an unset handle.
Weak tags, wildcards, lists, repeated headers, leading zeroes and overflow are
invalid. Same-value requests still require the current revision.

Success uses the standard reply `data` envelope. GET/PATCH return `ETag`; all
handler responses use `Cache-Control: no-store`. CORS configuration must expose
`ETag` and allow `If-Match` for cross-origin clients. Cookie CSRF/origin checks,
credential selection and authenticated rate limits remain host-owned; strict
JSON parsing is not a replacement for those protections.

The default dependency manifest includes user and router errors; explicit host
maps can override them. Syntax errors return 400, taken names 409, stale revisions
412 and missing `If-Match` 428. Missing storage capability/index readiness returns
503; unknown or mixed operational failures retain the safe 500 fallback. No raw
driver error or supplied name is included in public diagnostics. On an uncertain
write outcome, reread the current handle before retrying. The backend does not
add profile/settings UI, automatic backfill, aliases or identity lookup by handle.

## Key Features

The `usermanager` is designed to streamline complex user-related workflows into single API calls.

-   **Expanded User Profiles**: Fetch a complete user profile, including enriched data from multiple sources. A single request can return a user's core details alongside group memberships, team information, and more.
-   **Group Memberships**: A dedicated `/me/memberships` endpoint lets the authenticated user retrieve their group memberships, with optional filtering by group type (e.g. `TEAM`, `ORGANISATION`, etc.), the ability to include descendant groups, and optional root-prefix naming (`prefix_name`).
-   **Group Management**: Active users can create groups, add/remove members, and update group ownership — all through the `usermanager` surface. The service layer applies its own authorisation logic (admin flag or group access) on top of the route-level middleware.
-   **Communication Management**: Integrates with the `contacter` service to manage communication preferences and history (admin-only).
-   **Reminder Management**: Optionally integrates with the `reminder` service so users can create and manage scheduled reminders, while admin/service views can inspect reminder volume, due reminders, and stats.
-   **Streak Management**: Optionally integrates with `streaker` for recording and querying current, longest, total, and historical streaks.
-   **Notifications**: Optionally integrates with `notifier` for device registration, preferences, delivery summaries, and admin/service sends.
-   **Vision**: Optionally integrates with `vision` for feedback, roadmap items, votes, comments, and administrative status changes.

## API Endpoints

All endpoints are prefixed with `/api/v1/ums`.

### Open (rate-limited when configured)
-   `POST /api/v1/ums/comms`: Submit a new comms entry (e.g. a contact form submission).
-   `GET /api/v1/ums/visions`: List public vision items.
-   `GET /api/v1/ums/visions/config`: Get the public vision configuration.
-   `GET /api/v1/ums/visions/{visionNanoID}`: Get one public vision item.

### Authenticated
These require a valid JWT or API token.

-   `GET /api/v1/ums/me`: Get the authenticated user's own profile.
-   `DELETE /api/v1/ums/me`: Permanently delete the authenticated user's account.
-   `GET /api/v1/ums/me/micro`: Get a lightweight micro-profile for the authenticated user.
-   `GET /api/v1/ums/me/enriched`: Get an enriched profile, optionally including all group memberships. Supports `include_all_groups` and `prefix_name`.
-   `GET /api/v1/ums/me/memberships`: Get the authenticated user's group memberships. Supports `group_type`, `include_descendants`, and `prefix_name`.
-   `GET /api/v1/ums/me/groups`: Get a paginated list of groups the authenticated user belongs to. Supports `prefix_name`.
-   `GET /api/v1/ums/me/invitations`: List outstanding group invitations.
-   `POST /api/v1/ums/me/invitations/{groupID}/accept`: Accept a group invitation.
-   `POST /api/v1/ums/me/invitations/{groupID}/reject`: Reject a group invitation.
-   `GET /api/v1/ums/me/reminders`: List reminders for the authenticated user. Supports `status`, `target_type`, `target_id`, `page`, and `per_page`.
-   `POST /api/v1/ums/me/reminders`: Create a reminder for the authenticated user.
-   `GET /api/v1/ums/me/reminders/{reminderID}`: Get one reminder owned by the authenticated user.
-   `PATCH /api/v1/ums/me/reminders/{reminderID}`: Update one reminder owned by the authenticated user.
-   `DELETE /api/v1/ums/me/reminders/{reminderID}`: Delete one reminder owned by the authenticated user.
-   `POST /api/v1/ums/me/reminders/{reminderID}/disable`: Disable one reminder owned by the authenticated user.
-   `GET /api/v1/ums/me/streaks`: List the authenticated user's streak history.
-   `POST /api/v1/ums/me/streaks/record`: Record a streak event.
-   `GET /api/v1/ums/me/streaks/current`: Get the current streak count.
-   `GET /api/v1/ums/me/streaks/longest`: Get the longest streak.
-   `GET /api/v1/ums/me/streaks/count`: Count streak entries.
-   `GET /api/v1/ums/me/notifications/latest`: Get the latest notification overviews.
-   `GET /api/v1/ums/me/notifications/config`: Get client-safe notifier configuration.
-   `GET|POST /api/v1/ums/me/notifications/addresses`: List or register notification addresses.
-   `DELETE /api/v1/ums/me/notifications/addresses/{addressID}`: Delete one owned notification address.
-   `GET|PATCH /api/v1/ums/me/notifications/preferences`: Get or update notification preferences.
-   `GET /api/v1/ums/users`: List users.
-   `GET /api/v1/ums/users/{userId}`: Get a user by their ID.
-   `GET /api/v1/ums/users/{userId}/groups`: Get groups for a user.
-   `GET /api/v1/ums/groups/validate-name`: Validate a proposed group name.
-   `GET /api/v1/ums/groups/{groupID}`: Get enriched detail for a specific group (members, owner, etc.). Supports `prefix_name`.
-   `GET /api/v1/ums/groups/{groupID}/lineage`: Get the group's ancestor lineage.
-   `GET /api/v1/ums/groups/{groupID}/stats`: Get statistics for a specific group. Supports `prefix_name`.
-   `GET /api/v1/ums/groups/{groupID}/descendants`: Get descendant groups.
-   `POST /api/v1/ums/visions`: Create a vision item.
-   `PATCH|DELETE /api/v1/ums/visions/{visionNanoID}`: Update or delete an owned vision item.
-   `PUT|DELETE /api/v1/ums/visions/{visionNanoID}/votes`: Set or remove the requester's vote.
-   `POST /api/v1/ums/visions/{visionNanoID}/comments`: Add a comment.
-   `PUT|DELETE /api/v1/ums/visions/{visionNanoID}/comments/{commentID}/votes`: Set or remove a comment vote.

`GET /api/v1/ums/groups/config` uses
`ActiveValidApiTokenOrJWTMiddleware`, which is configured separately from the
general authenticated route group.

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
-   `GET /api/v1/ums/notifications/config`: Get notifier configuration.
-   `GET /api/v1/ums/notifications/latest`: Get latest notification overviews.
-   `GET /api/v1/ums/notifications/{userId}/latest`: Get a user's latest notification overviews.
-   `GET|POST /api/v1/ums/notifications/addresses`: List or register notification addresses.
-   `DELETE /api/v1/ums/notifications/{userId}/addresses/{addressID}`: Delete a user's address.
-   `GET|PATCH /api/v1/ums/notifications/{userId}/preferences`: Get or update a user's preferences.
-   `GET /api/v1/ums/users`: List users through the admin route group.
-   `PATCH /api/v1/ums/visions/{visionNanoID}/status`: Update a vision item's status.

### Admin or service token

These use `AdminApiTokenOrJWTMiddleware` when supplied, otherwise route setup
falls back to `AdminOnlyMiddleware`.

-   `POST /api/v1/ums/users/{userId}/notifications`: Send a notification to a user when notifier is wired.
-   `POST /api/v1/ums/notifications`: Send a notification to multiple users.
-   `GET /api/v1/ums/reminders`: List reminders across users. Supports `user_id`, `status`, `target_type`, `target_id`, `page`, and `per_page`.
-   `GET /api/v1/ums/reminders/stats`: Get aggregate reminder stats for admin overview pages. Supports optional `user_id` and `user_ids`.
-   `GET /api/v1/ums/reminders/due`: Get reminders that are ready for scheduler processing. Supports optional `user_id`, `user_ids`, `due_before`, and `limit`; if neither `user_id` nor `user_ids` is provided, it retrieves due reminders for everyone.
-   `GET /api/v1/ums/streaks`: List streak history across users.
-   `GET /api/v1/ums/streaks/current`: Get a current streak count.
-   `GET /api/v1/ums/streaks/longest`: Get a longest streak.
-   `GET /api/v1/ums/streaks/count`: Count streak entries.

### Reminder list authorisation

UMS uses one `ListReminders` service method for both `GET /me/reminders` and
`GET /reminders`. The service checks the requesting user through `UserService`.
If the requester is not an admin, the request is locked to their own `ActorID`
even if a different `user_id` filter is supplied. Admin users may omit
`user_id` to list across users, or pass it to inspect one user's reminders.

### Active users only
These require the user to be both authenticated and active.

-   `PATCH /api/v1/ums/me`: Update the authenticated user's own profile.
-   `POST /api/v1/ums/groups`: Create a new group.
-   `PATCH|DELETE /api/v1/ums/groups/{groupID}`: Update or delete a group.
-   `PUT /api/v1/ums/groups/{groupID}/owner`: Update the owner of a group.
-   `POST /api/v1/ums/groups/{groupID}/members`: Add a member to a group.
-   `DELETE /api/v1/ums/groups/{groupID}/members/{memberID}`: Remove a member from a group.
-   `PATCH /api/v1/ums/groups/{groupID}/members/{memberID}`: Update a member's role.

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
		ActiveOnlyMiddleware:               mockMiddleware,
		AdminOnlyMiddleware:                mockMiddleware,
		AdminApiTokenOrJWTMiddleware:       mockMiddleware,
		ActiveValidApiTokenOrJWTMiddleware: mockMiddleware,
		ValidApiTokenOrJWTMiddleware:       mockMiddleware,
		// Optional: custom middleware for GET /api/v1/ums/me only.
		CustomMeEndpointValidApiTokenOrJWTMiddleware: mockMiddleware,
		RateLimitOrActiveMiddleware:        mockMiddleware,
	})

	// ... set up and start the HTTP server with ghatdRouter
}
```
