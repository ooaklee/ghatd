package webapp

import (
	"io/fs"
	"log"
	"net/http"

	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/router"
)

// webAppHandler expected methods for valid web app handler
type webAppHandler interface {
	SnippetView(w http.ResponseWriter, r *http.Request)
	SnippetCreate(w http.ResponseWriter, r *http.Request)
	Home(w http.ResponseWriter, r *http.Request)
	Terms(w http.ResponseWriter, r *http.Request)
	Privacy(w http.ResponseWriter, r *http.Request)
	Cookie(w http.ResponseWriter, r *http.Request)
	AuthLogin(w http.ResponseWriter, r *http.Request)
	AuthSignup(w http.ResponseWriter, r *http.Request)
	AuthResetPassword(w http.ResponseWriter, r *http.Request)

	Dash(w http.ResponseWriter, r *http.Request)
}

const (
	// WebAppBase the start of the web apps URI
	WebAppBase = "/"

	// WebAppDashRoute base URI prefix for the web app dashboard routes
	WebAppDashRoute = WebAppBase + "dash"

	// WebAppPolicyTermsRoute base URI prefix for the web app terms policy page
	WebAppPolicyTermsRoute = WebAppBase + "terms"

	// WebAppPolicyPrivacyRoute base URI prefix for the web app privacy policy page
	WebAppPolicyPrivacyRoute = WebAppBase + "privacy-policy"

	// WebAppPolicyCookieRoute base URI prefix for the web app cookie policy page
	WebAppPolicyCookieRoute = WebAppBase + "cookie-policy"

	// WebAppAuthRoute base URI prefix for the web app auth route for user authentication
	WebAppAuthRoute = WebAppBase + "auth/"

	// WebAppAuthLoginRoute the URI for the web app login page
	WebAppAuthLoginRoute = WebAppAuthRoute + "login"

	// WebAppAuthSignupRoute the URI for the web app signup page
	WebAppAuthSignupRoute = WebAppAuthRoute + "signup"

	// WebAppAuthResetPasswordRoute the URI for the web app reset password page
	WebAppAuthResetPasswordRoute = WebAppAuthRoute + "reset-password"

	// WebAppStaticRoute base URI prefix for the web app static route for assets
	WebAppStaticRoute = WebAppBase + "static/"

	// WebAppSnippetRoute base URI prefix for the web app snippet routes
	WebAppSnippetRoute = WebAppBase + "snippet/"

	// WebAppSnippetViewRoute the URI for the web app snippet routes responsible for viewing snippet(s)
	WebAppSnippetViewRoute = WebAppSnippetRoute + "view"

	// WebAppSnippetCreateRoute the URI for the web app snippet routes responsible for creating a snippet
	WebAppSnippetCreateRoute = WebAppSnippetRoute + "create"
)

// AttachRoutesRequest holds everything needed to attach web app
// routes to router
type AttachRoutesRequest struct {
	// Router main router being served by server
	Router *router.Router

	// Handler valid web app handler
	Handler webAppHandler

	// WebAppFileSystem the file system that holds files utilised
	// by the web app
	WebAppFileSystem fs.FS
}

// AttachRoutes attaches webApp handler to corresponding
// routes on router
func AttachRoutes(request *AttachRoutesRequest) {

	// Create filesystem only holding static assets
	staticSubFS, err := fs.Sub(request.WebAppFileSystem, "internal/webapp/ui/static")
	if err != nil {
		log.Default().Panicln("unable-to-create-file-system-for-static-assets", err)
		return
	}

	httpRouter := request.Router.GetRouter()

	// Create path for handling static assets
	httpRouter.PathPrefix(WebAppStaticRoute).Handler(http.StripPrefix(WebAppStaticRoute, http.FileServer(http.FS(staticSubFS))))

	httpRouter.HandleFunc(WebAppBase, request.Handler.Home)
	httpRouter.HandleFunc(WebAppPolicyTermsRoute, request.Handler.Terms)
	httpRouter.HandleFunc(WebAppPolicyPrivacyRoute, request.Handler.Privacy)
	httpRouter.HandleFunc(WebAppPolicyCookieRoute, request.Handler.Cookie)
	httpRouter.HandleFunc(WebAppAuthLoginRoute, request.Handler.AuthLogin)
	httpRouter.HandleFunc(WebAppAuthSignupRoute, request.Handler.AuthSignup)
	httpRouter.HandleFunc(WebAppAuthResetPasswordRoute, request.Handler.AuthResetPassword)
	httpRouter.HandleFunc(WebAppDashRoute, request.Handler.Dash)
	httpRouter.HandleFunc(WebAppSnippetViewRoute, request.Handler.SnippetView)
	httpRouter.HandleFunc(WebAppSnippetCreateRoute, request.Handler.SnippetCreate)

}
