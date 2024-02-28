package server

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ooaklee/courses/golang/template-golang-htmx-alpine-tailwind/internal/router"

	"github.com/gorilla/mux"
	"github.com/spf13/cobra"

	"github.com/ooaklee/courses/golang/template-golang-htmx-alpine-tailwind/cmd/server/settings"
	"github.com/ooaklee/courses/golang/template-golang-htmx-alpine-tailwind/internal/response"
	"github.com/ooaklee/courses/golang/template-golang-htmx-alpine-tailwind/internal/toolbox"
	"github.com/ooaklee/courses/golang/template-golang-htmx-alpine-tailwind/internal/webapp"
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
	var serverGracefulWait time.Duration = time.Second * time.Duration(appSettings.GracefulServerTimeout)

	// placeholders
	var placeHolderRouterMiddlewares []mux.MiddlewareFunc

	// Initialise router
	httpRouter := router.NewRouter(response.GetResourceNotFoundError, response.GetDefault200Response, placeHolderRouterMiddlewares...)

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

	// TODO: Set Up simple API

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
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL)

	<-done

	// Create a deadline to wait for.
	serverWaitCtx, cancel := context.WithTimeout(context.Background(), serverGracefulWait)
	defer cancel()

	if err := srv.Shutdown(serverWaitCtx); err != nil {
		return fmt.Errorf("server/error-while-attempting-to-gracefully-stop-server - %v", err)
	}

	return nil

}
