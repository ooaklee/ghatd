# Post

The `post` package is the core content domain used by the CMS layer. It provides strongly-typed models, business rules, and MongoDB-backed persistence for post types such as changelog entries, glossary items, FAQs, and articles.

This guide covers a quick start for service setup, write/read flows, and migration-based seeding.

## Architecture

1. **Model (`model.go`)**: Defines post types, text formats, header image rules, URL-friendly IDs, and lightweight overviews.
2. **Service (`service.go`)**: Enforces business rules for creation, retrieval, filtering, publication, and deletion/restore workflows.
3. **Repository (`repository.go`)**: Handles MongoDB read/write operations for the `posts` collection.
4. **Contracts (`request.go`, `response.go`)**: Defines request/response shapes used by both direct service calls and higher-level packages.

## Supported Content Types

- `changelog`
- `article`
- `faq`
- `glossary`
- `other`

## Quick Start

### 1. Initialise Repository and Service

```go
import (
    "github.com/ooaklee/ghatd/external/post"
)

postRepository := post.NewRepository(coreRepository)
postService := post.NewService(postRepository, post.DefaultValidPostTags)
```

### 2. Create a Post

```go
created, err := postService.CreatePost(ctx, &post.CreatePostRequest{
    UserId:     "00000000-0000-0000-0000-000000000001",
    Title:      "Performance and reliability improvements",
    Type:       post.PostTypeChangelog,
    Text:       "Improved caching and route-level efficiency.",
    TextFormat: string(post.TextFormatMarkdown),
    Tags:       []string{"product-news"},
    PublishNow: true,
})
if err != nil {
    return err
}

_ = created.Post
```

### 3. Query Posts by Type

```go
resp, err := postService.GetPosts(ctx, &post.GetPostsRequest{
    WithTypes:    "article,changelog",
    PerPage:      10,
    Page:         1,
    IsNotDeleted: true,
    IsPublished:  true,
    Order:        "published_at_desc",
})
if err != nil {
    return err
}

for _, item := range resp.Posts {
    _ = item.UrlFriendlyId
}
```

## Key Business Rules

- `UserId` is required for create/update/delete/restore operations.
- `Title` and `Text` are required to create a post.
- `article` posts require `header_image`.
- non-article posts have `header_image` cleared.
- changelog posts must use valid tags (`post.DefaultValidPostTags`).
- URL-friendly ID generation is automatic and must remain unique.

## Mongo Collection

Posts are stored in MongoDB collection:

- `posts`

## Seeding Content with Migrations (Recommended)

If you plan to pre-load changelog/FAQ/glossary/article content, use migration files under `migrations/mongo` so content creation is versioned and repeatable.

### 1. Create a New Migration

```bash
go run main.go start-migrator new add-initial-faq-items
```

### 2. Add Up/Down Logic in Generated Migration

Use the generated file to insert or remove seed documents in `posts`. Keep IDs and URL-friendly IDs deterministic where practical.

```go
package migrations

import (
    "context"

    migrate "github.com/xakep666/mongo-migrate"
    "go.mongodb.org/mongo-driver/bson"
    "go.mongodb.org/mongo-driver/mongo"
)

func init() {
    migrate.Register(func(db *mongo.Database) error { // Up
        _, err := db.Collection("posts").InsertMany(context.Background(), []interface{}{
            bson.M{
                "_id":              "post-seed-1",
                "_nano_id":         "postseed1",
                "_url_friendly_id": "faq-what-is-ghatd",
                "type":             "faq",
                "title":            "What is ghatd?",
                "text_format":      "markdown",
                "text":             "A reusable backend framework.",
                "published_at":     "2026-01-01T00:00:00Z",
                "created_at":       "2026-01-01T00:00:00Z",
            },
        })
        return err
    }, func(db *mongo.Database) error { // Down
        _, err := db.Collection("posts").DeleteMany(context.Background(), bson.M{
            "_id": bson.M{"$in": []string{"post-seed-1"}},
        })
        return err
    })
}
```

### 3. Apply Migrations

```bash
go run main.go start-migrator up
```

## Relationship to Content Manager

For most HTTP/API use-cases, call the `contentmanager` package and let it orchestrate post operations plus access control. Use `post` directly for:

- internal tooling
- migration seeding
- lower-level service composition

See: [Content Manager Getting Started](../content-manager/README.md)
