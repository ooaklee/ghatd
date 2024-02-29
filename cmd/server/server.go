package server

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/cmd/server/settings"
	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/logger"
	loggerMiddleware "github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/logger/middleware"
	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/middleware/contenttype"
	cors "github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/middleware/cors"
	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/rememberer"
	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/repository"
	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/response"
	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/router"
	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/toolbox"
	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/validator"
	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/webapp"
)

// NewCommand returns a command for starting
// the webapp aspect of this service
func NewCommand(embeddedContent fs.FS) *cobra.Command {
	webAppCmd := &cobra.Command{
		Use:   "start-server",
		Short: "Start the server",
		Long:  "Start the server",
	}

	webAppCmd.Run = run(embeddedContent)
	return webAppCmd
}

// run the function that is called when the command is ran
func run(embeddedContent fs.FS) func(cmd *cobra.Command, args []string) {
	return func(cmd *cobra.Command, args []string) {
		if err := runServer(embeddedContent); err != nil {
			log.SetFlags(0)
			log.Fatal(err.Error())
		}
	}
}

// runServer handles initialising and running the server
func runServer(embeddedContent fs.FS) error {

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

	// TODO: Initialise clients, if applicable
	err = initialiseThirdPartyClients(appSettings)
	if err != nil {
		log.Panicf("server/failed-3rd-party-clients-initialisation: %v", err)
	}

	// TODO: Initialise additional middlewares to pass into attached routes if desired
	routerMiddlewares := initialiseRouterMiddlewares(appSettings, appLogger)

	// Initialise router
	httpRouter := router.NewRouter(response.GetResourceNotFoundError, response.GetDefault200Response, routerMiddlewares...)

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

	// Initialise handler for web app
	webAppHandler := webapp.NewWebAppHandler(embeddedContent)

	// Attach routes
	webapp.AttachRoutes(&webapp.AttachRoutesRequest{
		Router:           httpRouter,
		Handler:          webAppHandler,
		WebAppFileSystem: embeddedContent,
	})

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

	// TODO: Initialise repository, if applicable
	coreRepository := repository.NewInMememoryRepository()

	// TODO: Create Service(s)
	remembererService := rememberer.NewService(coreRepository)

	// TODO: Create Handler(s)
	remembererHandler := rememberer.NewHandler(remembererService, appValidator)

	// TODO: Attach package routes to router
	rememberer.AttachRoutes(&rememberer.AttachRoutesRequest{
		Router:  httpRouter,
		Handler: remembererHandler,
	})

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
func initialiseThirdPartyClients(appSettings *settings.Settings) error {
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

	// placeholder
	if appSettings.Environment == "local" {
		return fmt.Errorf("third-party-client-initialisation-placeholder-is-erroring: non-local environment in app settings (detected: %s)", appSettings.Environment)
	}

	return err

}

// initialiseRouterMiddlewares handles added to core middleswares that will be used by the server
func initialiseRouterMiddlewares(appSettings *settings.Settings, appLogger *zap.Logger) []mux.MiddlewareFunc {
	var routerMiddlewares []mux.MiddlewareFunc

	// Set origins
	allowOrigins := []string{}
	if strings.Contains(appSettings.AllowOrigins, ",") {
		allowOrigins = strings.Split(appSettings.AllowOrigins, ",")
	}

	if !strings.Contains(appSettings.AllowOrigins, ",") {
		allowOrigins = append(allowOrigins, appSettings.AllowOrigins)
	}

	routerMiddlewares = append(routerMiddlewares, contenttype.NewContentType, cors.NewCorsMiddleware(allowOrigins), loggerMiddleware.NewLogger(appLogger).HTTPLogger)

	return routerMiddlewares

}
