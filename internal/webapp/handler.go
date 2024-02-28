package webapp

import (
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strconv"
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

	// If the path is not exactly "/"
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// list of template files to parse, must be in order of inheritence
	templateFilesToParse := []string{
		"internal/webapp/ui/html/base.tmpl.html",
		"internal/webapp/ui/html/partials/nav.tmpl.html",
		"internal/webapp/ui/html/pages/home.tmpl.html",
	}

	// Parse template
	parsedTemplates, err := template.ParseFS(h.embeddedFileSystem, templateFilesToParse...)
	if err != nil {
		log.Default().Println("Unable to parse referenced template", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Write template to response
	err = parsedTemplates.ExecuteTemplate(w, "base", nil)
	if err != nil {
		log.Default().Println("Unable to execute parsed template", err)
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
