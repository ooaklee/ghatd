package webapp

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strconv"

	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/logger"
	"go.uber.org/zap"
)

// NewWebAppHandler creates a new instance of a web app handler
func NewWebAppHandler(embeddedContent fs.FS) *Handler {
	return &Handler{
		embeddedFileSystem: embeddedContent,
	}
}

// Handler manages request for webapp
type Handler struct {
	embeddedFileSystem fs.FS
}

func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {

	logger := logger.AcquireFrom(r.Context())

	// If the path is not exactly "/"
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// list of template files to parse, must be in order of inheritence
	templateFilesToParse := []string{
		"internal/webapp/ui/html/base.tmpl.html",
		"internal/webapp/ui/html/pages/home.tmpl.html",
		"internal/webapp/ui/html/partials/tailwind-dash-script.tmpl.html",
		"internal/webapp/ui/html/partials/header.tmpl.html",
	}

	// Parse template
	parsedTemplates, err := template.ParseFS(h.embeddedFileSystem, templateFilesToParse...)
	if err != nil {
		logger.Error("Unable to parse referenced template", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Write template to response
	err = parsedTemplates.ExecuteTemplate(w, "base", nil)
	if err != nil {
		logger.Error("Unable to execute parsed template", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

}

func (h *Handler) Dash(w http.ResponseWriter, r *http.Request) {

	logger := logger.AcquireFrom(r.Context())

	// list of template files to parse, must be in order of inheritence
	templateFilesToParse := []string{
		"internal/webapp/ui/html/base.tmpl.html",
		"internal/webapp/ui/html/pages/dash.tmpl.html",
		"internal/webapp/ui/html/partials/tailwind-dash-script.tmpl.html",
		"internal/webapp/ui/html/partials/preloader.tmpl.html",
		"internal/webapp/ui/html/partials/sidebar.tmpl.html",
		"internal/webapp/ui/html/partials/dash-header.tmpl.html",
		"internal/webapp/ui/html/partials/chart-area.tmpl.html",
		"internal/webapp/ui/html/partials/chart-bar.tmpl.html",
		"internal/webapp/ui/html/partials/chart-donut.tmpl.html",
		"internal/webapp/ui/html/partials/map-01.tmpl.html",
		"internal/webapp/ui/html/partials/table-01.tmpl.html",
	}

	// Parse template
	parsedTemplates, err := template.ParseFS(h.embeddedFileSystem, templateFilesToParse...)
	if err != nil {
		logger.Error("Unable to parse referenced templates", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Write template to response
	err = parsedTemplates.ExecuteTemplate(w, "base", nil)
	if err != nil {
		logger.Error("Unable to execute parsed templates", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

}

// Add a SnippetView handler function.
func (h *Handler) SnippetView(w http.ResponseWriter, r *http.Request) {

	queryParamId := r.URL.Query().Get("id")

	parsedId, err := strconv.Atoi(queryParamId)
	if err != nil || parsedId < 1 {
		http.Error(w, "View Not Found", http.StatusNotFound)
		return
	}

	fmt.Fprintf(w, "Displaying snippet: %d", parsedId)
}

// Add a SnippetCreate handler function.
func (h *Handler) SnippetCreate(w http.ResponseWriter, r *http.Request) {

	if r.Method != http.MethodPost {
		w.Header().Add("Allow", http.MethodPost)

		// To suppress a head I can used the following
		w.Header()["Date"] = nil

		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Write([]byte("Create a new snippet..."))
}
