# spa

Lightweight helpers for serving a single-page application (SPA) from embedded/static assets. It provides a 404 fallback handler and a router attach helper that rewrite unknown paths back to `/` so the SPA router can handle deep links.

## Quick start

1) Create the SPA handler used for 404 fallbacks:

```go
spaHandler := spa.NewSpaHandler(&spa.NewSpaHandlerRequest{
	EmbeddedContent:               embeddedContent,
	EmbeddedContentFilePathPrefix: embeddedContentFilePathPrefix,
	HandleUpdatePathToIndexFunc:   spa.NewHandleUpdatePathToIndex(),
})
```

2) Attach SPA routes to the app router so static assets are served and all other paths are rewritten to the SPA index:

```go
spa.AttachRoutes(&spa.AttachRoutesRequest{
	Router:                        httpRouter,
	SpaFileSystem:                 embeddedContent,
	EmbeddedContentFilePathPrefix: embeddedContentFilePathPrefix,
	HandleUpdatePathToIndexFunc:   spa.NewHandleUpdatePathToIndex(),
})
```

3) Control how paths are rewritten with `NewHandleUpdatePathToIndex`:

```go
// Default: bypass common asset extensions and rewrite everything else to "/".
handleUpdatePath := spa.NewHandleUpdatePathToIndex()

// Customise (examples):
// - Allow .webp through without rewriting
// - Force .txt to be rewritten to "/"
customHandleUpdatePath := spa.NewHandleUpdatePathToIndex(
	spa.BypassWithFileExtension(".webp"),
	spa.IgnoreFileExtension("txt"),
)
```

### How it works

- `NewHandleUpdatePathToIndex` builds a function that rewrites request paths to `/` unless the URL has a bypassed extension (defaults include `.js`, `.css`, images, fonts, etc.). This keeps SPA deep links working while letting static assets be served directly.
- `NewSpaHandler` uses that function inside `GetResourceNotFoundError` to serve the embedded `/dist/index.html` for non-API 404s.
- `AttachRoutes` wires the same updater into a catch-all route so browser requests without a known asset extension are rewritten and served by the SPA bundle.

