# starter/v0 Host Application Setup

This guide shows the "lazy starter" wiring shape used by a GHATD host
application server command. It keeps GHATD modular: the host application still owns
runtime settings, third-party clients, router bootstrap, SPA assets, and HTTP
lifecycle, while `external/starter/v0` removes the repetitive repository,
service, handler, middleware, stack, and route wiring.

## Ownership Split

| Host application owns | GHATD lazy helpers can reduce |
|-----------------------|------------------------------|
| Settings and environment loading | Repository container construction |
| Logger, validator, and observability choices | Service container construction |
| Mongo, Redis, email, OAuth, and payment settings | Handler container construction |
| SPA asset source and router middleware ordering | Access middleware suite construction |
| Resource cleanup registration | Package-owned runtime bootstrap |
| HTTP server lifecycle choices | Standard GHATD route attachment |

This split is intentional. A new project can start lazy, then eject any layer
back into its own `cmd/server` package when custom behaviour becomes clearer.

## Flow

1. Load settings and initialise host-owned observability, logging, and validation.
2. Use package-owned bootstrap helpers for repeatable foundations such as SPA
   router setup, Mongo runtime setup, Redis runtime setup, SparkPost, and
   standard email manager wiring.
3. Register resource cleanups in `starter.CleanupGroup`.
4. Build app-specific integrations such as OAuth
   providers, notifier senders, and payment providers.
5. Build `starter.Repositories`, `starter.Services`, `starter.Handlers`, and
   `starter.Middleware`.
6. Group the layers with `starter.NewStack`.
7. Attach standard GHATD API routes with `starter.AttachDefaultRoutes`.
8. Attach the default auth verify route with
   `router.AttachDefaultAuthVerifyRoute`, or use `router.NewAuthVerifyHandler`
   directly when the host application needs custom endpoint paths.
9. Attach host-owned SPA routes.
10. Start the HTTP server with `external/http/server.StartServerWith`.

## Package-Owned Bootstrap Helpers

The lazy path now keeps repeated setup logic inside the package that owns the
concept:

| Helper | Package | Use it for |
|--------|---------|------------|
| `NewBootstrap` | `external/spa` | SPA fallback handler, router creation, and later SPA route attachment. |
| `NewMongoRuntime` | `external/repository` | Mongo URI generation, handler creation, warmup, cleanup, and core repository creation. |
| `NewRedisRuntime` | `external/ephemeral` | Redis client creation, optional hooks, ping, cleanup, and ephemeral store creation. |
| `NewSparkPostClient` | `external/emailprovider` | SparkPost client initialisation with optional transport instrumentation. |
| `NewStandardEmailManager` | `external/emailmanager` | Standard GHATD email templater plus email manager wiring. |
| `AttachDefaultAuthVerifyRoute` | `external/router` | Default auth verification route registration from backend/frontend base URLs. |

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

    spaBootstrap, err := spa.NewBootstrap(&spa.BootstrapRequest{
        EmbeddedContent:               embeddedContent,
        EmbeddedContentFilePathPrefix: embeddedContentFilePathPrefix,
        DefaultHealthcheckHandler:     response.GetDefault200Response,
        Middlewares:                   routerMiddlewares,
    })
    if err != nil {
        return fmt.Errorf("server/spa-bootstrap-initialisation-failed: %v", err)
    }
    httpRouter := spaBootstrap.Router

    var cleanupGroup starter.CleanupGroup
    defer func() {
        if err := cleanupGroup.Run(context.Background()); err != nil {
            log.Default().Printf("server/resource-cleanup-failed: %v\n", err)
        }
    }()

    mongoRuntime, err := repository.NewMongoRuntime(context.Background(), &repository.NewMongoRuntimeRequest{
        URIConfig: repositoryhelpers.MongoURIConfig{
            Username: appSettings.MongoDatabaseUsername,
            Password: appSettings.MongoDatabasePassword,
            Host:     appSettings.MongoDatabaseHost,
            AppName:  appSettings.MongoDatabaseAppName,
            Atlas:    appSettings.MongoDatabaseAtlas,
        },
        Database: appSettings.MongoDatabaseName,
        Options: []repositoryhelpers.ConfigOption{
            repositoryhelpers.WithConnectionPool(200, 10, 15*time.Minute),
            repositoryhelpers.WithTimeouts(5*time.Second, 3*time.Second, 120*time.Second),
            repositoryhelpers.WithRetryPolicy(true, true, 10*time.Second),
        },
    })
    if err != nil {
        return fmt.Errorf("server/mongo-runtime-initialisation-failed: %v", err)
    }
    cleanupGroup.Add(mongoRuntime.Close)

    redisRuntime, err := ephemeral.NewRedisRuntime(context.Background(), &ephemeral.NewRedisRuntimeRequest{
        Options: &redis.Options{
            Addr:     appSettings.RedisDSN,
            Password: appSettings.RedisPassword,
        },
        MaxUnauthedRequestAllowance: appSettings.MaxUnauthedRequestAllowance,
        Component:                   appSettings.Component,
        Environment:                 appSettings.Environment,
    })
    if err != nil {
        return fmt.Errorf("server/redis-runtime-initialisation-failed: %v", err)
    }
    cleanupGroup.Add(redisRuntime.Close)

    starterRepositories, err := starter.NewRepositories(&starter.NewRepositoriesRequest{
        Core: mongoRuntime.CoreRepository,
    })
    if err != nil {
        return fmt.Errorf("server/starter-repositories-initialisation-failed: %v", err)
    }

    auditService := audit.NewService(starterRepositories.Audit)

    sparkpostClient, err := emailprovider.NewSparkPostClient(&emailprovider.NewSparkPostClientRequest{
        BaseURL:    appSettings.SparkpostURLEndpoint,
        APIKey:     appSettings.SparkpostAccessToken,
        APIVersion: 1,
    })
    if err != nil {
        return fmt.Errorf("server/sparkpost-client-initialisation-failed: %v", err)
    }

    var emailProvider emailprovider.EmailProvider
    emailProvider = emailprovider.NewLoggingEmailProvider(&emailprovider.LoggingEmailProviderConfig{})
    if appSettings.Environment == "production" {
        emailProvider = emailprovider.NewSparkPostEmailProvider(sparkpostClient)
    }

    emailVerificationFullEndpoint := appSettings.BackendBaseURL + router.AuthVerifyEndpoint
    emailManager, err := emailmanager.NewStandardEmailManager(&emailmanager.NewStandardEmailManagerRequest{
        Provider:                      emailProvider,
        AuditService:                  auditService,
        FrontendBaseURL:               appSettings.FrontendBaseURL,
        EmailVerificationFullEndpoint: emailVerificationFullEndpoint,
        DashboardVerificationURIPath:  emailVerificationFullEndpoint,
        Environment:                   appSettings.Environment,
        BusinessEntityName:            appSettings.BusinessEntityName,
        BusinessEntityWebsite:         appSettings.BusinessEntityWebsite,
        WelcomeEmailSubject:           appSettings.EmailWelcomeSubject,
        LoginEmailSubject:             appSettings.EmailLoginSubject,
        FromEmailAddress:              appSettings.EmailFromAddr,
        NoReplyEmailAddress:           appSettings.EmailNoReplyAddr,
    })
    if err != nil {
        return fmt.Errorf("server/email-manager-initialisation-failed: %v", err)
    }

    starterServices, err := starter.NewServices(&starter.NewServicesRequest{
        Repositories:          starterRepositories,
        EphemeralStore:        redisRuntime.Store,
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

    if err := router.AttachDefaultAuthVerifyRoute(&router.AttachDefaultAuthVerifyRouteRequest{
        Router:          httpRouter,
        BackendBaseURL:  appSettings.BackendBaseURL,
        FrontendBaseURL: appSettings.FrontendBaseURL,
    }); err != nil {
        return fmt.Errorf("server/auth-verify-route-attachment-failed: %v", err)
    }

    if err := spaBootstrap.AttachRoutes(); err != nil {
        return fmt.Errorf("server/spa-routes-attachment-failed: %v", err)
    }

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

- Keep third-party settings explicit until the project has a stable opinion
  about Redis, email, OAuth, payment, observability, and storage lifecycle.
  Use the package-owned runtime helpers for the common path, and eject back to
  direct client construction when the host application needs custom behaviour.
- Prefer `starter.AttachDefaultRoutes` for a lazy first pass. Eject route
  attachment back into per-package calls when one route group needs custom
  middleware or ordering.
- Prefer `external/http/server.StartServerWith` for the common graceful
  shutdown flow. Eject it only when the project needs custom listeners,
  multiple servers, TLS management, or a different signal strategy.
- Use `starter.CleanupGroup` for host-owned resource cleanup. Starter does not
  own client lifetimes; it only gives the cleanup functions a shared home.
