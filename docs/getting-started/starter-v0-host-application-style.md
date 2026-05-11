# starter/v0 Host Application Setup

This guide shows the "lazy starter" wiring shape used by a GHATD host
application server command. It keeps GHATD modular: the host application still owns
runtime settings, third-party clients, router bootstrap, SPA assets, and HTTP
lifecycle, while `external/starter/v0` removes the repetitive repository,
service, handler, middleware, stack, and route wiring.

## Ownership Split

| Host application owns | `starter/v0` helps with |
|-----------------------|-------------------------|
| Settings and environment loading | Repository container construction |
| Logger, validator, and observability clients | Service container construction |
| Mongo, Redis, email, OAuth, and payment clients | Handler container construction |
| SPA file serving and router bootstrap | Access middleware suite construction |
| Resource cleanup registration | Stack validation and grouping |
| HTTP server lifecycle | Standard GHATD route attachment |

This split is intentional. A new project can start lazy, then eject any layer
back into its own `cmd/server` package when custom behaviour becomes clearer.

## Flow

1. Load settings and initialise host-owned clients.
2. Build the Mongo URI and core repository.
3. Register resource cleanups in `starter.CleanupGroup`.
4. Build app-specific integrations such as Redis stores, email managers, OAuth
   providers, notifier senders, and payment providers.
5. Build `starter.Repositories`, `starter.Services`, `starter.Handlers`, and
   `starter.Middleware`.
6. Group the layers with `starter.NewStack`.
7. Attach standard GHATD API routes with `starter.AttachDefaultRoutes`.
8. Attach host-owned SPA routes.
9. Start the HTTP server with `external/http/server.StartServerWith`.

## Trimmed Example

The example below is intentionally trimmed. It shows where starter/v0 fits in
the server command without hiding the app-owned decisions. Host-owned helper
signatures are abbreviated so the example can stay focused on the starter
handoff points.

```go
func runServer(embeddedContent fs.FS, embeddedContentFilePathPrefix string) error {
    appSettings, err := serverSettings.NewSettings()
    if err != nil {
        return fmt.Errorf("server/unable-to-load-settings: %v", err)
    }

    appLogger, err := initialiseLogger(appSettings)
    if err != nil {
        return err
    }

    appValidator := validator.NewValidator()
    if err := appValidator.Validate(appLogger); err != nil {
        return err
    }

    routerMiddlewares, err := initialiseRouterMiddlewares(appSettings, appLogger)
    if err != nil {
        return err
    }

    spaHandler := spa.NewSpaHandler(&spa.NewSpaHandlerRequest{
        EmbeddedContent:               embeddedContent,
        EmbeddedContentFilePathPrefix: embeddedContentFilePathPrefix,
        HandleUpdatePathToIndexFunc:   spa.NewHandleUpdatePathToIndex(),
    })

    httpRouter := router.NewRouter(
        spaHandler.GetResourceNotFoundError,
        response.GetDefault200Response,
        routerMiddlewares...,
    )

    var cleanupGroup starter.CleanupGroup
    defer func() {
        if err := cleanupGroup.Run(context.Background()); err != nil {
            log.Default().Printf("server/resource-cleanup-failed: %v\n", err)
        }
    }()

    mongoURI, err := repositoryhelpers.GenerateMongoURI(repositoryhelpers.MongoURIConfig{
        Username: appSettings.MongoDatabaseUsername,
        Password: appSettings.MongoDatabasePassword,
        Host:     appSettings.MongoDatabaseHost,
        AppName:  appSettings.MongoDatabaseAppName,
        Atlas:    appSettings.MongoDatabaseAtlas,
    })
    if err != nil {
        return fmt.Errorf("server/failed-to-generate-mongo-uri: %v", err)
    }

    mongoHandler, err := repositoryhelpers.NewHandlerWithOptions(
        mongoURI,
        appSettings.MongoDatabaseName,
        repositoryhelpers.WithConnectionPool(200, 10, 15*time.Minute),
        repositoryhelpers.WithTimeouts(5*time.Second, 3*time.Second, 120*time.Second),
        repositoryhelpers.WithRetryPolicy(true, true, 10*time.Second),
    )
    if err != nil {
        return err
    }
    cleanupGroup.Add(mongoHandler.Close)

    coreRepository := repository.NewMongoDbRepositoryWithDefaults(
        mongoHandler,
        appSettings.MongoDatabaseName,
    )

    starterRepositories, err := starter.NewRepositories(&starter.NewRepositoriesRequest{
        Core: coreRepository,
    })
    if err != nil {
        return fmt.Errorf("server/starter-repositories-initialisation-failed: %v", err)
    }

    auditService := audit.NewService(starterRepositories.Audit)
    ephemeralStore := ephemeral.NewRedisStore(redisClient, maxAttempts, component, environment)
    emailManager := emailmanager.NewEmailManager(emailTemplater, emailProvider, auditService, emailConfig)

    starterServices, err := starter.NewServices(&starter.NewServicesRequest{
        Repositories:          starterRepositories,
        EphemeralStore:        ephemeralStore,
        EmailManager:          emailManager,
        AccessTokenSecret:     appSettings.AccessTokenSecret,
        RefreshTokenSecret:    appSettings.RefreshTokenSecret,
        StaticPlaceholderUUID: appSettings.StaticPlaceholderUUID,
        AuditService:          auditService,
        OAuthServices: []accessmanager.OauthService{
            oauthGoogleProvider,
        },
        AutoAdminEmailAddressRegex: appSettings.AdminGroupEmailRegex,
        UserConfig:                 user.DefaultUserConfig(),
        UserConfigs: []*user.UserConfig{
            user.APIServiceUserConfig(),
        },
        GroupConfig:     group.DefaultGroupConfig(),
        ValidPostTags:   post.DefaultValidPostTags,
        NotifierSenders: notifierSenders,
        PolicyConfig: &starter.PolicyConfig{
            BusinessEntityName:      appSettings.BusinessEntityName,
            BusinessEntityEmail:     appSettings.BusinessEntityEmail,
            BusinessEntityWebsite:   appSettings.BusinessEntityWebsite,
            LegalBusinessEntityName: appSettings.BusinessEntityNameLegal,
            GenerateStaticPolicies:  true,
        },
        PaymentProviders: []paymentprovider.Provider{
            kofiPaymentProvider,
        },
    })
    if err != nil {
        return fmt.Errorf("server/starter-services-initialisation-failed: %v", err)
    }

    starterHandlers, err := starter.NewHandlers(&starter.NewHandlersRequest{
        Services:                 starterServices,
        Validator:                appValidator,
        Environment:              appSettings.Environment,
        CookiePrefixAuthToken:    appSettings.CookiePrefixAuthToken,
        CookiePrefixRefreshToken: appSettings.CookiePrefixRefreshToken,
        CookieDomain:             appSettings.CookieDomain,
    })
    if err != nil {
        return fmt.Errorf("server/starter-handlers-initialisation-failed: %v", err)
    }

    starterMiddleware, err := starter.NewMiddleware(&starter.NewMiddlewareRequest{
        Services:                 starterServices,
        Environment:              appSettings.Environment,
        CookiePrefixAuthToken:    appSettings.CookiePrefixAuthToken,
        CookiePrefixRefreshToken: appSettings.CookiePrefixRefreshToken,
        CookieDomain:             appSettings.CookieDomain,
    })
    if err != nil {
        return fmt.Errorf("server/starter-middleware-initialisation-failed: %v", err)
    }

    serverPort, err := strconv.Atoi(appSettings.Port)
    if err != nil {
        return fmt.Errorf("server/invalid-port: %v", err)
    }

    starterStack, err := starter.NewStack(&starter.NewStackRequest{
        Config: starter.Config{
            Port:        serverPort,
            Environment: appSettings.Environment,
            LogLevel:    appSettings.LogLevel,
        },
        Repositories: starterRepositories,
        Services:     starterServices,
        Handlers:     starterHandlers,
        Middleware:   starterMiddleware,
        Cleanup:      cleanupGroup.Run,
    })
    if err != nil {
        return fmt.Errorf("server/starter-stack-initialisation-failed: %v", err)
    }

    if err := starter.AttachDefaultRoutes(&starter.AttachDefaultRoutesRequest{
        Router: httpRouter,
        Stack:  starterStack,
    }); err != nil {
        return fmt.Errorf("server/starter-routes-attachment-failed: %v", err)
    }

    spa.AttachRoutes(&spa.AttachRoutesRequest{
        Router:                        httpRouter,
        SpaFileSystem:                 embeddedContent,
        EmbeddedContentFilePathPrefix: embeddedContentFilePathPrefix,
        HandleUpdatePathToIndexFunc:   spa.NewHandleUpdatePathToIndex(),
    })

    return httpserver.StartServerWith(&httpserver.StartServerWithRequest{
        Host:                    appSettings.Host,
        Port:                    appSettings.Port,
        Handler:                 httpRouter.GetRouter(),
        GracefulShutdownTimeout: time.Second * time.Duration(appSettings.GracefulServerTimeout),
        ReadHeaderTimeout:       10 * time.Second,
        Log: func(level, message string) {
            fmt.Println(toolbox.OutputBasicLogString(level, message))
        },
    })
}
```

## Migration Notes

- Keep third-party clients explicit until the project has a stable opinion
  about Redis, email, OAuth, payment, observability, and storage lifecycle.
- Prefer `starter.AttachDefaultRoutes` for a lazy first pass. Eject route
  attachment back into per-package calls when one route group needs custom
  middleware or ordering.
- Prefer `external/http/server.StartServerWith` for the common graceful
  shutdown flow. Eject it only when the project needs custom listeners,
  multiple servers, TLS management, or a different signal strategy.
- Use `starter.CleanupGroup` for host-owned resource cleanup. Starter does not
  own client lifetimes; it only gives the cleanup functions a shared home.
