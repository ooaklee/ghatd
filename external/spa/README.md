# spa

Lightweight helpers for serving a single-page application (SPA) from embedded/static assets. It provides a 404 fallback handler and a router attach helper that rewrite unknown paths back to `/` so the SPA router can handle deep links.

## Quick start

For the common host-application path, use `NewBootstrap` to create the SPA
fallback handler and router together:

```go
spaBootstrap, err := spa.NewBootstrap(&spa.BootstrapRequest{
	EmbeddedContent:               embeddedContent,
	EmbeddedContentFilePathPrefix: embeddedContentFilePathPrefix,
	DefaultHealthcheckHandler:     response.GetDefault200Response,
	Middlewares:                   routerMiddlewares,
})
if err != nil {
	return err
}

httpRouter := spaBootstrap.Router

// Attach API routes before this call so the SPA catch-all stays last.
if err := spaBootstrap.AttachRoutes(); err != nil {
	return err
}
```

For lower-level composition:

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
if err := spa.AttachRoutes(&spa.AttachRoutesRequest{
	Router:                        httpRouter,
	SpaFileSystem:                 embeddedContent,
	EmbeddedContentFilePathPrefix: embeddedContentFilePathPrefix,
	HandleUpdatePathToIndexFunc:   spa.NewHandleUpdatePathToIndex(),
}); err != nil {
	return err
}
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

// Bypass specific files by name. The match is against the path filename segment, not the full route.
// This is independent of extension bypass rules.
beaconHandleUpdatePath := spa.NewHandleUpdatePathToIndex(
	spa.BypassWithFileName("beacon-loader.js"),
	spa.BypassWithFileName("beacon-example.html"),
)
```

### How it works

- `NewHandleUpdatePathToIndex` builds a function that rewrites request paths to `/` unless the URL has a bypassed extension (defaults include `.js`, `.css`, images, fonts, etc.) or a bypassed filename. This keeps SPA deep links working while letting static assets be served directly.
- `NewSpaHandler` uses that function inside `GetResourceNotFoundError` to serve the embedded `/dist/index.html` for non-API 404s.
- `AttachRoutes` wires the same updater into a catch-all route so browser requests without a known asset extension are rewritten and served by the SPA bundle.
