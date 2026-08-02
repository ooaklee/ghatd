<div align="center" style="padding-bottom: 8px;">
  <img alt="ghat" height="280px" src="./docs/assets/ghat-final-logo-with-background-shadow.png">
</div>

# GHAT(D)

GHAT(D) is an open-source, opinionated, and free full-stack web application foundation based on the Go programming language. Its name is an acronym that stands for Go, HTMX, Alpine.js, Tailwind, and DaisyUI, which originally formed the foundational stack. Over time, for improved usability, it has also been extended to support most Vite-compatible front-end stacks (tested with Vue). The aim is to make GHAT(D) a solid base for creating highly portable, scalable, and performant full-stack projects. Whether you need just a backend, a landing page, or a content-driven application, you can still utilise GHAT(D) without committing to a hidden application container.

We recognise that everyone has unique needs, and ideally their solutions should not start with a messy foundation that requires cleaning up before building. To reduce cognitive load and make preparation easier, we have introduced "building blocks" which we call `Details`. A `Detail` is an independent application that can function both within a GHAT(D) project and on its own. GHAT(D) supports `api`, `web`, and `web-vite` Detail types.

## Motivation

GHAT(D) is a hobby project I work on in my spare time. It is designed to provide a friendly starting point for people like me who are interested in Go, APIs, and web applications, and who want a consistent foundation and shared standards to build from. I hope GHAT(D) can serve as an ejectable base for many awesome projects and initiatives.


I also aim to use this project as a learning opportunity, to improve my understanding of and share my knowledge of lightweight frontend libraries, highly portable full-stack alternatives, and cost-effective infrastructure solutions for full-scale products. 

As we develop this project, I want to also create tutorials/guides for those who want to integrate it with other technologies, such as `rpc`,  `graphql`,  `websocket`, and others.

I am a platform engineer by trade, so I do not promise perfect code by any stretch of the imagination (especially with the front end - so please support and contribute). Instead, I am aspiring to create a standardised project base that helps those curious (about Go, APIs, hobby projects, and web app development) to turn their ideas/ hobbies into tangible product(s) that they can demo and even ship.

This will be an exciting experience, and I look forward to building out this project with you all and sharing my progress and knowledge as it matures.


## Core Packages

GHAT(D) offers modular packages that can be used both together and independently. Our goal is for each package to adhere to clean architecture principles, featuring comprehensive documentation and examples. We are committed to implementing these practices in both new and legacy packages, especially those that are less extensible for other projects.

### Authentication & Verification
A dual-channel verification system providing both magic link and human-readable code entry.

- **[Access Manager](./external/accessmanager/README.md)** - Complete authentication and authorisation with email-based verification, login, OAuth, and API token management
  - `accessmanager` - User creation, login, registration, email verification, OAuth, API token management
  - `accessmanager/middleware` - JWT, API token, rate-limiting, and hardened code-verification middleware
  - `accessmanager/helpers` - Context-transmission utilities and unique code generation
  - `auth` - JWT creation, validation, and metadata extraction
  - `apitoken` - API token lifecycle management

### Email System
A complete email solution split into three composable packages for maximum flexibility and testability.

- **[Email Manager](./external/emailmanager/README.md)** - Complete email system with templating, sending, and audit logging
  - `emailtemplater` - Generate HTML email templates with variable substitution
  - `emailprovider` - Abstract email sending across providers (SparkPost, logging, custom)
  - `emailmanager` - High-level orchestration with audit integration

### Billing System
A complete billing solution split into three composable packages for maximum flexibility and testability.

- **[Billing Manager](./external/billingmanager/README.md)** - Complete billing system with webhook processing, subscription management, and audit logging
  - `paymentprovider` - Abstract payment provider webhook verification and payload normalisation (Stripe, Lemon Squeezy, Ko-fi)
  - **[`billing`](./external/billing/README.md)** - Manage subscription and billing event data persistence with repository pattern
  - `billingmanager` - High-level orchestration with webhook processing and audit integration
- **[Pricer](./external/pricer/README.md)** - Source-of-truth pricing catalog with plans, feature entitlements, provider refs, Mongo migrations, and pricing-card E2E fixtures
  - `pricer` - Manage pricing plans, costs, features, and provider-linked catalog metadata

### Additional Packages
- **[Audit](./external/audit/)** - Handles audit logging for compliance and debugging
- **[Content Manager](./external/contentmanager/README.md)** - HTTP orchestration for CMS-style content
- **[Group](./external/group/README.md)** - User groups, memberships, and hierarchical organisations
- **[Logger](./external/logger/)** - Structured logging with middleware support
- **[MongoDB Migrator](./external/migrator/mongo/README.md)** - Shared migration command with host-owned registrations and templates
- **[Notifier](./external/notifier/README.md)** - Push notification registration, preferences, and delivery
- **[Post](./external/post/README.md)** - Reusable content models, persistence, and publication rules
- **[Reminder](./external/reminder/README.md)** - User-owned scheduled reminders with target-based lookups and execution tracking
- **[Router](./external/router/README.md)** - Shared HTTP routing and route attachment
- **[SEO](./external/seo/README.md)** - Sitemap generation and persistence
- **[SPA](./external/spa/README.md)** - Single-page application serving and fallback routing
- **[Streaker](./external/streaker/README.md)** - Generic idempotent streak completions, current/best stats, and history listing
- **[Repository](./external/repository/README.md)** - MongoDB repository patterns and utilities
- **[Server](./external/http/server/README.md)** - Ejectable HTTP server lifecycle helper with graceful shutdown
- **[Starter/v0](./external/starter/v0/README.md)** - Ejectable lazy composition layer for GHATD application wiring
- **[User v2](./external/user/v2/README.md)** - Configurable universal user model and persistence
- **[User Manager](./external/usermanager/README.md)** - User-facing orchestration across user, group, reminder, and related services
- **[Vision](./external/vision/README.md)** - Feedback and roadmap management
- **[Error Manifest](./external/errormanifest/)** - Cross-package error mapping and bundle composition
**Note on Core Packages:** This is a curated overview rather than an exhaustive package inventory. Each documented package keeps its canonical README alongside the package code.

## Dual-Channel Verification

GHAT(D) supports a dual-channel verification flow for login and email verification — users receive both a **magic link** (with a JWT token) and an **8-character alphanumeric code** in the same email.

### How It Works

1. **Email delivery**: Both login and verification emails contain a clickable magic link AND a human-readable 8-character code displayed in large monospaced font.
2. **Link flow** (`?t=<jwt-token>`): User clicks the magic link → token is validated → user is authenticated.
3. **Code flow** (`?c=ABCD1234`): User enters the code in the app or web interface ("I already have a session code") → code is resolved to its corresponding token via ephemeral storage → token is validated → user is authenticated.
4. **Code generation**: Each code is globally unique (A-Z, 0-9), crypto-random, stored in ephemeral storage with a TTL matching the token expiry, and regenerated on collision.

### Security Measures

| Layer | Mechanism |
|---|---|
| **Code entropy** | 8-character A-Z/0-9 = ~2.8 trillion combinations |
| **Collision resistance** | Ephemeral storage check with up to 5 retry attempts |
| **Brute-force protection** | `HardenedRateLimitProtection` middleware tracks attempts per IP and per code within a configurable window (default: 5/hr per IP, 5/hr per code) |
| **Auto-blocking** | IPs exceeding the threshold are temporarily blocked (default: 1 hour) |
| **One-time use** | Codes and tokens are invalidated after successful verification |
| **Time-bounded** | Login tokens default to 5 minutes; email-verification tokens default to 10 minutes |
| **Refresh rotation tolerance** | Near-concurrent duplicate refreshes can reuse the winning rotation result instead of consuming the same refresh token twice |
| **Login email cooldown** | Duplicate login email sends for the same active user/context are suppressed during a short cooldown window |
| **Audit logging** | All verification attempts (pass and fail) and rate-limit blocks are logged for monitoring |
| **Rate-limit response** | Blocked IPs receive HTTP 429 with `EPH0-002` — no information leakage |


## Starting locally

Before getting started please make sure you have the correct version of [Go installed](https://go.dev/doc/install) or you can use [asdf](https://github.com/asdf-vm/asdf) to install the pinned toolchain from `.tool-versions`. **Minimum required Go version: 1.26.4**.

```sh
# Add the plugin for Go
asdf plugin add golang

# Install the pinned version
asdf install
```

Use `asdf exec` when running maintenance commands so local validation uses the same Go toolchain as the repository:

```sh
asdf exec go test ./...
asdf exec go run golang.org/x/vuln/cmd/govulncheck@v1.3.0 ./...
asdf exec go mod tidy
```

### Using the CLI (experimental)

Run the source version of the CLI with the repository's pinned toolchain:

```sh
asdf exec go run cli/cli.go --help
```

Inspect a command before using it:

```sh
asdf exec go run cli/cli.go <desired-command> --help
```

The `new` command can assemble a host application from one or more Details, but the generator remains experimental and its defaults may target development branches or module versions. Review the generated application and run `asdf exec go mod tidy` before relying on it. Generated applications include a host-owned `cmd/mongo-migrator` adapter and `migrations/mongo/template.go`; the adapter keeps using GHATD's shared migration implementation while its host imports are rewritten to the new module.

Example local command:

```shell
asdf exec go run cli/cli.go new \
  -n "awesome-service" \
  -m "github.com/example/awesome-service" \
  -w "github.com/example/ghatd-detail-api"
```

See [About Details](./docs/about-details.md) and [Getting Started With A New Project](./docs/how-to/local-development/a-new-project-it-s-in-the-detail.md) for the supported Detail types and generated-app workflow. See [Managing MongoDB Migrations](./docs/how-to/manage-mongodb-migrations.md) before adding or applying migrations.

### Starting the server

To start the server you can use the code:

```sh
asdf exec go run main.go start-server
```

For a better development experience, install [`reflex`](https://github.com/cespare/reflex) to rerun the server command when files change:

```sh
reflex -r '\.(html|go|css|png|svg|ico|js|woff2|woff|ttf|eot)$' -s -- asdf exec go run main.go start-server
```

> More [information on hot-reloading can be found below](#hot-reloading)


## Good to know

### ASCII Art

All ASCII art in this template was created using [PatorJK](https://patorjk.com/software/taag/#p=display&h=2&f=Isometric3).

### Curl Examples

- Making a `GET` request: `curl -i -X GET "http://localhost:4000/v0/health/check"`

### How to stop file server showing directory listing?

Add a blank index.html file to the specific directory that you want to disable listings for. For example, the
code below will create an index file which will stop [the web app](http://localhost:4000/static/) from showing 
and listing page.

```sh
touch internal/web/ui/static/index.html
```

### Hot reloading

Install reflex

`asdf exec go install github.com/cespare/reflex@latest`

> You can find more information in the repo https://github.com/cespare/reflex

Once installed, run the server

```sh
reflex -r '\.(html|go|css|png|svg|ico|js|woff2|woff|ttf|eot)$' -s -- asdf exec go run main.go start-server
```

### How to build binaries

One of the benefits of using the GHATD stack is that it compiles everything into a single binary. This makes it highly portable and provides numerous deployment options. 

#### CLI 

To build a binary for the GHATDCLI for your desired system architecture, please follow the instructions below:


> All commands should be executed from the root directory.

##### Mac OS (ARM64)

```sh
export BINARY_NAME=ghatdcli
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 asdf exec go build -a -installsuffix cgo -ldflags="-w -s" -o ./$BINARY_NAME cli/cli.go
```

##### Mac OS (AMD64)

```sh
export BINARY_NAME=ghatdcli
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 asdf exec go build -a -installsuffix cgo -ldflags="-w -s" -o ./$BINARY_NAME cli/cli.go
```

##### Linux (ARM64)

```sh
export BINARY_NAME=ghatdcli
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 asdf exec go build -a -installsuffix cgo -ldflags="-w -s" -o ./$BINARY_NAME cli/cli.go
```

##### Linux (AMD64)

```sh
export BINARY_NAME=ghatdcli
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 asdf exec go build -a -installsuffix cgo -ldflags="-w -s" -o ./$BINARY_NAME cli/cli.go
```


#### Web App

To build a binary for web app to your desired system architecture, please follow the instructions below:

> All commands should be executed from the root directory.

##### Mac OS (ARM64)

```sh
export BINARY_NAME=ghatd
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 asdf exec go build -a -installsuffix cgo -ldflags="-w -s" -o ./$BINARY_NAME main.go
```

##### Mac OS (AMD64)

```sh
export BINARY_NAME=ghatd
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 asdf exec go build -a -installsuffix cgo -ldflags="-w -s" -o ./$BINARY_NAME main.go
```

##### Linux (ARM64)

```sh
export BINARY_NAME=ghatd
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 asdf exec go build -a -installsuffix cgo -ldflags="-w -s" -o ./$BINARY_NAME main.go
```

##### Linux (AMD64)

```sh
export BINARY_NAME=ghatd
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 asdf exec go build -a -installsuffix cgo -ldflags="-w -s" -o ./$BINARY_NAME main.go
```

## License
This project is licensed under the [MIT License](./LICENSE).
