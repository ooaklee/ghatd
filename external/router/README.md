# Router

The `router` package provides a standardised, project-specific wrapper around `gorilla/mux`. It's designed to simplify setting up common application-level routing concerns, like default handlers, middleware, and authentication endpoints.

## Architecture

-   **`router.go`**: Contains the `Router` struct and the `NewRouter` constructor. It initialises a `mux.Router` and applies any provided default handlers or global middleware.
-   **`handler.go`**: Provides handlers for common, cross-cutting concerns. A key example is `NewAuthVerifyHandler`, which manages the redirection flow for email and login verification links.
-   **`auth_verify.go`**: Provides `AttachDefaultAuthVerifyRoute`, a host-application helper that registers GHATD's default verification route from backend and frontend base URLs. Use `NewAuthVerifyHandler` directly when the host application needs custom endpoint paths.
-   **`const.go`**: Defines constant URI paths for shared endpoints like health checks (`/v0/health/check`) and authentication verification (`/v0/auth/verify`).

## Getting Started

The following example shows how to initialise the `ghatdRouter`, configure it with default handlers and middleware, and attach a verification endpoint.

### Example Initialisation

This setup is typically done once in your application's `main` function or wherever you configure your HTTP server.

```go
package main

import (
	"fmt"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router"
	// ... other imports
)

func main() {
	// ... initialise logger and other dependencies

	// Define base URLs for the application (this will be the same if packaging your UI with the ghatd backend)
	backendBaseURL := "http://localhost:8080"
	frontendBaseURL := "http://localhost:3000"

	// 1. Define Default Handlers
	// These handlers are used by the router for unhandled routes or health checks.
	default404Handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "Not Found")
	})
	defaultHealthcheckHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "OK")
	})

	// 2. Define Global Middleware (e.g., CORS)
	// This is a mock CORS middleware for demonstration purposes.
	// In a real project, you would use a proper CORS library.
	corsMiddleware := func(allowedOrigins []string) mux.MiddlewareFunc {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Access-Control-Allow-Origin", "*") // Be more restrictive in production
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				if r.Method == "OPTIONS" {
					w.WriteHeader(http.StatusOK)
					return
				}
				next.ServeHTTP(w, r)
			})
		}
	}

	// 3. Create a New Router
	// Initialise the router with the default handlers and global middleware.
	ghatdRouter := router.NewRouter(
		default404Handler,
		defaultHealthcheckHandler,
		corsMiddleware([]string{frontendBaseURL}),
	)

	// 4. Add Application-Specific Handlers
	// The default auth verify route processes verification links from emails.
	if err := router.AttachDefaultAuthVerifyRoute(&router.AttachDefaultAuthVerifyRouteRequest{
		Router:          ghatdRouter,
		BackendBaseURL:  backendBaseURL,
		FrontendBaseURL: frontendBaseURL,
	}); err != nil {
		panic(err)
	}

	// 5. Attach Service-Specific Routes
	// At this point, you would attach the routes for each of your services.
	// For example:
	//
	// usermanager.AttachRoutes(&usermanager.AttachRoutesRequest{
	// 	Router:  ghatdRouter,
	// 	Handler: umsHandler,
	// 	// ... middleware
	// })

	// 6. Start the HTTP Server
	// http.ListenAndServe(":8080", ghatdRouter.GetRouter())
}
```

By following this pattern, you establish a consistent foundation for routing across your entire application, which can then be referenced by other "Getting Started" guides.
