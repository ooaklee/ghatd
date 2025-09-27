package server

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/NYTimes/gziphandler"
	"github.com/gorilla/mux"
	"github.com/spf13/cobra"
	"github.com/tdewolff/minify"
	"github.com/tdewolff/minify/html"
	"github.com/tdewolff/minify/js"
	"github.com/tdewolff/minify/json"
	"github.com/tdewolff/minify/svg"
	"go.uber.org/zap"

	cache "github.com/ooaklee/http-cache"
	"github.com/ooaklee/http-cache/adapter/memory"

	"github.com/ooaklee/ghatd/cmd/server/settings"
	"github.com/ooaklee/ghatd/external/logger"
	loggerMiddleware "github.com/ooaklee/ghatd/external/logger/middleware"
	"github.com/ooaklee/ghatd/external/middleware/contenttype"
	cors "github.com/ooaklee/ghatd/external/middleware/cors"
	"github.com/ooaklee/ghatd/external/response"
	"github.com/ooaklee/ghatd/external/router"
	"github.com/ooaklee/ghatd/external/toolbox"
	"github.com/ooaklee/ghatd/external/validator"
	//>ghatd {{ block "WebDetailImports" . }}
	//>ghatd {{ end }}
	//>ghatd {{ block "ApiDetailImports" . }}
	//>ghatd {{ end }}
)

// NewCommand returns a command for starting
// the webapp aspect of this service
func NewCommand(embeddedContent fs.FS, embeddedContentFilePathPrefix string) *cobra.Command {
	webAppCmd := &cobra.Command{
		Use:   "start-server",
		Short: "Start the server",
		Long:  "Start the server",
	}

	webAppCmd.Run = run(embeddedContent, embeddedContentFilePathPrefix)
	return webAppCmd
}

// run the function that is called when the command is ran
func run(embeddedContent fs.FS, embeddedContentFilePathPrefix string) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		if err := runServer(embeddedContent, embeddedContentFilePathPrefix); err != nil {
			log.SetFlags(0)
			log.Fatal(err.Error())
		}
	}
}

// runServer handles initialising and running the server
func runServer(embeddedContent fs.FS, embeddedContentFilePathPrefix string) error {

	// Initialise appplication settings
	appSettings, err := settings.NewSettings()
	if err != nil {
		return fmt.Errorf("server/unable-to-load-settings - %v", err)
	}

	// Define server timeouts
	// serverGracefulWait specify time to wait for server to
	// allow connections before gracefully shutting down
	var (
		serverGracefulWait time.Duration = time.Second * time.Duration(appSettings.GracefulServerTimeout)
	)

	// Initialise Logger
	appLogger, err := initialiseLogger(appSettings)
	if err != nil {
		return fmt.Errorf("server/application-logger-initialisation-failed: %v", err)
	}

	// Initialise validator
	appValidator := validator.NewValidator()
	// carry out random validation to stop unused error
	err = appValidator.Validate(appLogger)
	if err != nil {
		return fmt.Errorf("server/application-logger-validation-failed: %v", err)
	}

	// TODO: Initialise clients, if applicable
	minifierClient, err := initialiseThirdPartyClients(appSettings)
	if err != nil {
		log.Panicf("server/failed-3rd-party-clients-initialisation: %v", err)
	}

	// TODO: Initialise additional middlewares to pass into attached routes if desired
	routerMiddlewares, err := initialiseRouterMiddlewares(appSettings, appLogger, minifierClient)
	if err != nil {
		log.Panicf("server/failed-middleware-initialisation: %v", err)
	}

	// Initialise router
	httpRouter := router.NewRouter(response.GetResourceNotFoundError, response.GetDefault200Response, routerMiddlewares...)

	// Service information
	serviceExternalName := appSettings.BusinessEntityName
	serviceExternalWebsite := appSettings.BusinessEntityWebsite
	serviceExternalEmail := appSettings.BusinessEntityEmail
	serviceLegalBusinessName := appSettings.BusinessEntityNameLegal

	fmt.Println(toolbox.OutputBasicLogString("info", fmt.Sprintf("initiating service referencing the external name (%s), website (%s), email (%s) and with legal business name (%s)", serviceExternalName, serviceExternalWebsite, serviceExternalEmail, serviceLegalBusinessName)))

	//
	//  	 ___          ___                                ___          ___      ___
	//  	/__/\        /  /\        _____                 /  /\        /  /\    /  /\
	//     _\_ \:\      /  /:/_      /  /::\               /  /::\      /  /::\  /  /::\
	//    /__/\ \:\    /  /:/ /\    /  /:/\:\             /  /:/\:\    /  /:/\:\/  /:/\:\
	//   _\_ \:\ \:\  /  /:/ /:/_  /  /:/~/::\           /  /:/~/::\  /  /:/~/:/  /:/~/:/
	//  /__/\ \:\ \:\/__/:/ /:/ /\/__/:/ /:/\:|         /__/:/ /:/\:\/__/:/ /:/__/:/ /:/
	//  \  \:\ \:\/:/\  \:\/:/ /:/\  \:\/:/~/:/         \  \:\/:/__\/\  \:\/:/\  \:\/:/
	//   \  \:\ \::/  \  \::/ /:/  \  \::/ /:/           \  \::/      \  \::/  \  \::/
	//    \  \:\/:/    \  \:\/:/    \  \:\/:/             \  \:\       \  \:\   \  \:\
	//     \  \::/      \  \::/      \  \::/               \  \:\       \  \:\   \  \:\
	//  	\__\/        \__\/        \__\/                 \__\/        \__\/    \__\/

	//>ghatd {{ block "WebDetailInit" . }}
	//>ghatd {{ end }}

	//      	 ___           ___
	//      	/  /\         /  /\      ___
	//         /  /::\       /  /::\    /  /\
	//        /  /:/\:\     /  /:/\:\  /  /:/
	//       /  /:/~/::\   /  /:/~/:/ /__/::\
	//      /__/:/ /:/\:\ /__/:/ /:/  \__\/\:\__
	//      \  \:\/:/__\/ \  \:\/:/      \  \:\/\
	//       \  \::/       \  \::/        \__\::/
	//        \  \:\        \  \:\        /__/:/
	//         \  \:\        \  \:\       \__\/
	//      	\__\/         \__\/

	//>ghatd {{ block "ApiDetailInit" . }}
	//>ghatd {{ end }}

	//        	 ___          ___          ___                     ___          ___
	//      	/  /\        /  /\        /  /\        ___        /  /\        /  /\
	//         /  /:/_      /  /:/_      /  /::\      /__/\      /  /:/_      /  /::\
	//        /  /:/ /\    /  /:/ /\    /  /:/\:\     \  \:\    /  /:/ /\    /  /:/\:\
	//       /  /:/ /::\  /  /:/ /:/_  /  /:/~/:/      \  \:\  /  /:/ /:/_  /  /:/~/:/
	//      /__/:/ /:/\:\/__/:/ /:/ /\/__/:/ /:/______  \__\:\/__/:/ /:/ /\/__/:/ /:/___
	//      \  \:\/:/~/:/\  \:\/:/ /:/\  \:\/:::::/__/\ |  |:|\  \:\/:/ /:/\  \:\/:::::/
	//       \  \::/ /:/  \  \::/ /:/  \  \::/~~~~\  \:\|  |:| \  \::/ /:/  \  \::/~~~~
	//        \__\/ /:/    \  \:\/:/    \  \:\     \  \:\__|:|  \  \:\/:/    \  \:\
	//      	/__/:/      \  \::/      \  \:\     \__\::::/    \  \::/      \  \:\
	//      	\__\/        \__\/        \__\/         ~~~~      \__\/        \__\/

	// Define server
	srv := &http.Server{
		Addr:    fmt.Sprintf("%s:%s", appSettings.Host, appSettings.Port),
		Handler: httpRouter.GetRouter(),
	}

	// Run server as go routine
	go func() {
		fmt.Println(toolbox.OutputBasicLogString("info", fmt.Sprintf("Server is listening on port - %s", appSettings.Port)))

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.SetFlags(0)
			log.Fatal(toolbox.OutputBasicLogString("info", fmt.Sprintf("server/unable-to-start-server - %v", err)))
		}
	}()

	// Handle graceful server shutdowns
	done := make(chan os.Signal, 1)
	// Define when graceful shutdowns should take place
	// Note: SIGINT (Ctrl+C), SIGTERM (Ctrl+/)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	<-done

	// Create a deadline to wait for.
	serverWaitCtx, cancel := context.WithTimeout(context.Background(), serverGracefulWait)
	defer cancel()

	if err := srv.Shutdown(serverWaitCtx); err != nil {
		return fmt.Errorf("server/error-while-attempting-to-gracefully-stop-server - %v", err)
	}

	return nil

}

// initialiseLogger configures logger used throughout the application
func initialiseLogger(appSettings *settings.Settings) (*zap.Logger, error) {
	logger, err := logger.NewLogger(
		appSettings.LogLevel,
		appSettings.Environment,
		appSettings.Component,
	)
	if err != nil {
		return nil, fmt.Errorf("unable-to-create-logger: %w", err)
	}
	return logger, nil
}

// initialiseThirdPartyClients returns initialised third-party clients if successfully initialised,
// otherwise error will be returned if some fail initialisation.
func initialiseThirdPartyClients(appSettings *settings.Settings) (*minify.M, error) {
	var (
		err error
	)

	// //** Some Third Party Application **//
	// // Only attempt if license key is passed
	// if appSettings.SomeLicenseKey != "" {

	// 	return someThirdPartyClient, nil

	// 	if err != nil {
	// 		return nil, fmt.Errorf("unable-to-initialise-some-third-party: %v", err)
	// 	}
	// }

	// (todo: refine configuarion)
	minifierClient := minify.New()
	// minifierClient.AddFunc("text/css", css.Minify)
	minifierClient.AddFunc("text/html", html.Minify)
	minifierClient.AddFunc("image/svg+xml", svg.Minify)
	minifierClient.AddFuncRegexp(regexp.MustCompile("^(application|text)/(x-)?(java|ecma)script$"), js.Minify)
	minifierClient.AddFuncRegexp(regexp.MustCompile("[/+]json$"), json.Minify)

	// placeholder
	if appSettings.Environment != "local" {
		return nil, fmt.Errorf("third-party-client-initialisation-placeholder-is-erroring: non-local environment in app settings (detected: %s)", appSettings.Environment)
	}

	return minifierClient, err

}

// initialiseRouterMiddlewares handles added to core middleswares that will be used by the server
func initialiseRouterMiddlewares(appSettings *settings.Settings, appLogger *zap.Logger, minifierClient *minify.M) ([]mux.MiddlewareFunc, error) {
	var routerMiddlewares []mux.MiddlewareFunc

	// Set origins
	allowOrigins := []string{}
	if strings.Contains(appSettings.AllowOrigins, ",") {
		allowOrigins = strings.Split(appSettings.AllowOrigins, ",")
	}

	if !strings.Contains(appSettings.AllowOrigins, ",") {
		allowOrigins = append(allowOrigins, appSettings.AllowOrigins)
	}

	// Set cache management

	// set cache adpter (todo: migrate to redis for production)
	// More information can be found here: https://github.com/victorspringer/http-cache?tab=readme-ov-file#getting-started
	memcachedCacheAdapter, err := memory.NewAdapter(
		memory.AdapterWithAlgorithm(memory.LRU),
		memory.AdapterWithCapacity(10000000),
	)
	if err != nil {
		return []mux.MiddlewareFunc{}, fmt.Errorf("unable-to-initialise-cache-memory-adapter: %v", err)
	}

	apiPathRegex := regexp.MustCompile(appSettings.CacheSkipUriPathRegex)

	cacheClient, err := cache.NewClient(
		cache.ClientWithAdapter(memcachedCacheAdapter),
		cache.ClientWithTTL(time.Duration(appSettings.CacheTtl)*time.Minute),
		cache.ClientWithRefreshKey(appSettings.CacheRefreshParameterKey),
		cache.ClientWithExpiresHeader(),
		cache.ClientWithSkipCacheResponseHeader(appSettings.CacheSkipHttpHeader),
		cache.ClientWithSkipCacheUriPathRegex(apiPathRegex),
	)
	if err != nil {
		return []mux.MiddlewareFunc{}, fmt.Errorf("unable-to-initialise-cache-memory-middleware: %v", err)
	}

	// gzip responses - gziphandler
	// manage caching -
	routerMiddlewares = append(routerMiddlewares, contenttype.NewContentType, cors.NewCorsMiddleware(allowOrigins), loggerMiddleware.NewLogger(appLogger, []string{}).HTTPLogger, gziphandler.GzipHandler, minifierClient.Middleware, cacheClient.Middleware)

	return routerMiddlewares, nil

}
