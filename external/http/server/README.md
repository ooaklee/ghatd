# external/http/server

Package `server` provides an ejectable HTTP server lifecycle helper that
reduces `ListenAndServe` / signal / shutdown boilerplate in host applications
while keeping router setup and SPA routes host-owned.

## Usage

```go
import server "github.com/ooaklee/ghatd/external/http/server"

err := server.StartServerWith(&server.StartServerWithRequest{
    Host:                    "localhost",
    Port:                    "8080",
    Handler:                 myRouter,
    GracefulShutdownTimeout: 15 * time.Second,
    ReadHeaderTimeout:       10 * time.Second,
    Log:                     myLogger,
})
```

`Handler`, address data, and `GracefulShutdownTimeout` are required. The helper
provides sensible defaults for signal handling (SIGINT, SIGTERM, and
os.Interrupt via signal.Notify), ListenAndServe (treats http.ErrServerClosed as
success), and Shutdown.

## API

### Observability lifecycle

Start one [observability runtime](../../observability/README.md#bootstrap) before
application dependencies and pass `otelhttp.Wrap(...)` as `Handler`. `Context`
controls this helper's lifecycle and logging; it does **not** populate
`http.Server.BaseContext` for requests. If handlers need runtime values from
their request contexts, configure a server directly as shown by the
[reference service](../../../examples/observability/main.go).

Drain requests and close dependencies before shutting telemetry down. If
`Shutdown` returns an error, this helper returns it without automatically
forcing `Server.Close`; a host requiring that fallback must supply a custom
shutdown function or own the server lifecycle. The reference demonstrates both
bounded draining and forced close before telemetry cleanup.

### `StartServerWith(req *StartServerWithRequest) error`

Creates an `*http.Server`, starts it in a background goroutine, blocks until a
signal or context cancellation is received, and calls graceful shutdown with
the configured timeout. Returns validation errors for nil requests, missing
handlers, missing address data, and non-positive timeouts.

### `ResolveAddr(host, port, addr string) string`

Returns `addr` when set, otherwise returns `fmt.Sprintf("%s:%s", host, port)`.

### `StartServerWithRequest`

| Field                      | Type                       | Required | Default                                                          |
|----------------------------|----------------------------|----------|------------------------------------------------------------------|
| `Host`                     | `string`                   | no       | `""`                                                             |
| `Port`                     | `string`                   | no       | `""`                                                             |
| `Addr`                     | `string`                   | no       | `""`                                                             |
| `Handler`                  | `http.Handler`             | yes      | —                                                                |
| `GracefulShutdownTimeout`  | `time.Duration`            | yes      | —                                                                |
| `ReadHeaderTimeout`        | `time.Duration`            | no       | `0` (no timeout)                                                 |
| `Log`                      | `func(level, message string)` | no    | no-op                                                            |
| `Signals`                  | `<-chan os.Signal`         | no       | `signal.Notify` with os.Interrupt, SIGINT, SIGTERM               |
| `NotifySignals`            | `[]os.Signal`              | no       | `[os.Interrupt, syscall.SIGINT, syscall.SIGTERM]`               |
| `ListenAndServe`           | `func(*http.Server) error` | no       | `srv.ListenAndServe()` (treats `http.ErrServerClosed` as success) |
| `Shutdown`                 | `func(*http.Server, context.Context) error` | no | `srv.Shutdown(ctx)`                              |
| `Context`                  | `context.Context`          | no       | `context.Background()`                                           |

### Exit behavior

- The background goroutine treats `http.ErrServerClosed` as a successful
  completion and will not trigger an error return.
- Any other error from `ListenAndServe` is returned as a wrapped
  `server/startup-failure`.
- Signal-triggered or context-triggered shutdown calls `Shutdown` with the
  configured `GracefulShutdownTimeout`.

### Error sentinels

| Sentinel                 | Meaning                          |
|--------------------------|----------------------------------|
| `ErrNilRequest`          | request is nil                   |
| `ErrMissingHandler`      | `.Handler` is nil                |
| `ErrMissingAddressData`  | `.Addr`, `.Host`, and `.Port` produce an empty, bare-colon, or trailing-colon address |
| `ErrNonPositiveTimeout`  | `.GracefulShutdownTimeout <= 0`  |
| `ErrStartupFailure`      | `ListenAndServe` returned a non-shutdown error |
| `ErrShutdownFailure`     | graceful `Shutdown` returned an error |

### Ejectability

If the default lifecycle behavior no longer fits, replace the single
`StartServerWith` call with an inlined `http.Server` creation, `signal.Notify`,
and `Shutdown` call without changing any other package.
