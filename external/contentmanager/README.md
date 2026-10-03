# Content Manager

The `contentmanager` package is the HTTP orchestration layer for CMS-style content in this project. It sits on top of the `post` package and exposes endpoints for changelog entries, glossary items, FAQ items, articles, and latest-post overviews.

This guide focuses on a practical quick start, based on the current service wiring in this repository.

## Architecture

The package follows the standard route -> handler -> service pattern used across this codebase:

1. **Routes (`routes.go`)**: Defines the `/api/v1/cms` endpoints and middleware requirements.
2. **Handler (`handler.go`)**: Maps HTTP requests to service requests and writes structured responses.
3. **Fender (`fender.go`)**: Decodes body/query/path values and binds `ActorID` from verified middleware context, separately from post IDs and stored authors.
4. **Service (`service.go`)**: Orchestrates post retrieval/mutation and applies access rules (admin-only write, published-only public read).

## Quick Start

### 1. Wire Dependencies in the Server

Create the `post` and `contentmanager` services, then attach routes.

```go
import (
    "github.com/ooaklee/ghatd/external/contentmanager"
    "github.com/ooaklee/ghatd/external/post"
    userV2 "github.com/ooaklee/ghatd/external/user/v2"
)

// repository and user service already initialised earlier
postRepository := post.NewRepository(coreRepository)
postService := post.NewService(postRepository, post.DefaultValidPostTags)
contentManagerService := contentmanager.NewService(postService, userV2Service)

contentManagerHandler := contentmanager.NewHandler(
    contentManagerService,
    appValidator,
    post.PostErrorMap,
    toolbox.ToolboxErrorMap,
    userV2.UserErrorMap,
)

contentmanager.AttachRoutes(&contentmanager.AttachRoutesRequest{
    Router:                                 httpRouter,
    Handler:                                contentManagerHandler,
    MiddlewareAdminApiTokenOrJwtRequired:   middlewareAdminApiTokenOrJwtRequired,
    RateLimitOrActiveMiddleware:            middlewareRateLimitOrActive,
    MiddlewareValidApiTokenOrJWTMiddleware: middlewareActiveAValidApiTokenOrJwt,
})
```

### 2. Endpoint Groups and Access

- **Admin write endpoints**:
  - `POST /api/v1/cms/posts`
  - `PATCH /api/v1/cms/posts/{postId}`
  - `DELETE /api/v1/cms/posts/{postId}`
  - `PATCH /api/v1/cms/posts/{postId}/restore`
- **Open/read endpoints (rate-limited or active user)**:
  - `GET /api/v1/cms/changelog`
  - `GET /api/v1/cms/changelog/{urlFriendlyId}`
  - `GET /api/v1/cms/glossary`
  - `GET /api/v1/cms/faq`
  - `GET /api/v1/cms/articles`
  - `GET /api/v1/cms/articles/{urlFriendlyId}`
  - `GET /api/v1/cms/latest`

## Request Patterns

### Create a Post (Admin)

```bash
curl -X POST http://localhost:8080/api/v1/cms/posts \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <admin-token>" \
  -d '{
    "title": "Launch of the blog",
    "type": "article",
    "text": "# Hello\nThis is our first post",
    "text_format": "markdown",
    "header_image": "https://example.com/blog-cover.jpg",
    "tags": ["announcement"],
    "publish_now": true
  }'
```

### Query the Latest Notification Feed

Use the `kinds` and `limit` query parameters to retrieve a mixed feed. Supported
kinds include `post_article`, `post_changelog`, `post_faq`, and
`post_glossary`.

```bash
curl "http://localhost:8080/api/v1/cms/latest?kinds=post_article,post_changelog&limit=5"
```

### Paginated Content Queries

The changelog/glossary/faq/articles endpoints support common query params from `post.GetPostsRequest`, for example:

```bash
curl "http://localhost:8080/api/v1/cms/changelog?per_page=10&page=1&meta=true&with_tags=bug-fix&without_tags=announcement"
```

## Behaviour Rules to Know

### ActorID migration

This is a breaking Go source change: the eight reader request fields formerly
named `UserId` now use `ActorID`. The create, update, delete and restore wrappers
inherit the same rename from their embedded `post` requests. There are no legacy
caller aliases. Stored `CreatedByUserId`, `PublishedByUserId`, `UpdatedByUserId`
and `DeletedByUserId` fields are unchanged, as are post target IDs and routes.

Actor fields have `json:"-"` and no query or path tag. Private mutation mappers
require both a verified authentication flag and a nonempty context ID; public
read mappers use an empty actor for anonymous requests. An optional-auth
rate-limit placeholder never establishes viewer authority. Custom HTTP adapters
must publish verified state, not just a context ID. See the
[request identity guide](../../docs/how-to/request-identity.md#transport-binding).

Services use the explicit request actor. They no longer silently substitute a
different viewer from context. When identity evidence is present in context it
must match: conflicting or anonymous evidence denies mutations and reduces
reads to public-only content. Trusted in-process callers may use an ordinary
context, but must establish authority before assigning `ActorID`; IDs are not
credentials. Mutation authority is resolved against the current actor account,
never a post's author. Empty actors are forbidden; missing or inconsistent
dependency results map to `CM00-02` (503). Native dependency errors keep their
shared mappings and host overrides. Unavailable optional viewer authority falls
back to the public projection without logging private lookup diagnostics.

Public list restrictions operate on a request-local copy, clearing contradictory
visibility flags. Public single-item reads also exclude soft-deleted posts.
Latest-post filtering copies the adapter's overview slice. These changes do not
introduce a new publication scheduling policy. Sitemap jobs still request only
published, non-deleted content; direct jobs need no viewer, while the HTTP route
remains administrator-protected.

HTTP updates accept individual editable fields, not an embedded `post` full
replacement. Previously that replacement could override the URI-selected post
and server-owned attribution. Use the field-based update payload over HTTP.
Trusted internal replacements remain available in `post.UpdatePostRequest`,
but their ID must match `PostId` when both are supplied.

The common notification request's `UserID` remains a recipient field, not an
actor. This manager's post feed is global public content and does not use that
field to authorize access to a private notification inbox.

- Non-admin users are blocked from create/update/delete/restore actions.
- Public readers only receive published items.
- Single-item endpoints enforce URL prefix intent:
  - changelog IDs should start with `changelog-`
  - article IDs should start with `article-`
- If `published_as` is not explicitly set, the service resolves a fallback author display value.

## Frontend Integration Notes

If you are implementing a web client for this API:

- Use a dedicated CMS client with base URL `/api/v1/cms`.
- Keep dedicated methods for:
  - `getLatestPostsByType`
  - `getChangelogItems` and `getChangelogItemByUrlFriendlyId`
  - `getGlossaryItems`
  - `getFaqItems`
  - `getArticles` and `getArticleItemByUrlFriendlyId`
- For feed cards/home widgets, use `/latest` and consume post overviews.

## Related Package Docs

- [Post Getting Started](../post/README.md)
- [Router Getting Started](../router/README.md)
