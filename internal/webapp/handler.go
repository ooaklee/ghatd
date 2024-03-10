package webapp

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"strconv"

	"go.uber.org/zap"

	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/common"
	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/logger"
	webapphelpers "github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/webapp/helpers"
	"github.com/ooaklee/template-golang-htmx-alpine-tailwind/internal/webapp/policy"
)

// NewWebAppHandlerRequest is the request needed to create a web app handler
type NewWebAppHandlerRequest struct {
	EmbeddedContent      fs.FS
	TermsOfServicePolicy *policy.WebAppPolicy
	PrivacyPolicy        *policy.WebAppPolicy
	CookiePolicy         *policy.WebAppPolicy
}

// NewWebAppHandler creates a new instance of a web app handler
func NewWebAppHandler(r *NewWebAppHandlerRequest) *Handler {
	return &Handler{
		embeddedFileSystem:   r.EmbeddedContent,
		termsOfServicePolicy: r.TermsOfServicePolicy,
		privacyPolicy:        r.PrivacyPolicy,
		cookiePolicy:         r.CookiePolicy,
	}
}

// Handler manages request for webapp
type Handler struct {
	embeddedFileSystem   fs.FS
	termsOfServicePolicy *policy.WebAppPolicy
	privacyPolicy        *policy.WebAppPolicy
	cookiePolicy         *policy.WebAppPolicy
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
		"internal/webapp/ui/html/partials/shared/tailwind-dash-script.tmpl.html",
		"internal/webapp/ui/html/partials/shared/header.tmpl.html",
		"internal/webapp/ui/html/partials/shared/footer.tmpl.html",
		"internal/webapp/ui/html/partials/shared/social-links.tmpl.html",
		"internal/webapp/ui/html/partials/shared/sidebar.tmpl.html",
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

func (h *Handler) Terms(w http.ResponseWriter, r *http.Request) {

	logger := logger.AcquireFrom(r.Context())

	// list of template files to parse, must be in order of inheritence
	templateFilesToParse := []string{
		"internal/webapp/ui/html/base.tmpl.html",
		"internal/webapp/ui/html/pages/base-policy.tmpl.html",
		"internal/webapp/ui/html/partials/shared/tailwind-dash-script.tmpl.html",
		"internal/webapp/ui/html/partials/shared/header.tmpl.html",
		"internal/webapp/ui/html/partials/shared/footer.tmpl.html",
		"internal/webapp/ui/html/partials/shared/social-links.tmpl.html",
		"internal/webapp/ui/html/partials/shared/sidebar.tmpl.html",
		"internal/webapp/ui/html/partials/policy/policy-holder.tmpl.html",
	}

	// Parse template
	parsedTemplates, err := template.ParseFS(h.embeddedFileSystem, templateFilesToParse...)
	if err != nil {
		logger.Error("Unable to parse referenced template", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Write template to response
	err = parsedTemplates.ExecuteTemplate(w, "base", h.termsOfServicePolicy)
	if err != nil {
		logger.Error("Unable to execute parsed template", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
}

func (h *Handler) Privacy(w http.ResponseWriter, r *http.Request) {

	logger := logger.AcquireFrom(r.Context())

	// list of template files to parse, must be in order of inheritence
	templateFilesToParse := []string{
		"internal/webapp/ui/html/base.tmpl.html",
		"internal/webapp/ui/html/pages/base-policy.tmpl.html",
		"internal/webapp/ui/html/partials/shared/tailwind-dash-script.tmpl.html",
		"internal/webapp/ui/html/partials/shared/header.tmpl.html",
		"internal/webapp/ui/html/partials/shared/footer.tmpl.html",
		"internal/webapp/ui/html/partials/shared/social-links.tmpl.html",
		"internal/webapp/ui/html/partials/shared/sidebar.tmpl.html",
		"internal/webapp/ui/html/partials/policy/policy-holder.tmpl.html",
	}

	// Parse template
	parsedTemplates, err := template.ParseFS(h.embeddedFileSystem, templateFilesToParse...)
	if err != nil {
		logger.Error("Unable to parse referenced template", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Write template to response
	err = parsedTemplates.ExecuteTemplate(w, "base", h.privacyPolicy)
	if err != nil {
		logger.Error("Unable to execute parsed template", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

}

func (h *Handler) Cookie(w http.ResponseWriter, r *http.Request) {

	logger := logger.AcquireFrom(r.Context())

	// list of template files to parse, must be in order of inheritence
	templateFilesToParse := []string{
		"internal/webapp/ui/html/base.tmpl.html",
		"internal/webapp/ui/html/pages/base-policy.tmpl.html",
		"internal/webapp/ui/html/partials/shared/tailwind-dash-script.tmpl.html",
		"internal/webapp/ui/html/partials/shared/header.tmpl.html",
		"internal/webapp/ui/html/partials/shared/footer.tmpl.html",
		"internal/webapp/ui/html/partials/shared/social-links.tmpl.html",
		"internal/webapp/ui/html/partials/shared/sidebar.tmpl.html",
		"internal/webapp/ui/html/partials/policy/policy-holder.tmpl.html",
	}

	// Parse template
	parsedTemplates, err := template.ParseFS(h.embeddedFileSystem, templateFilesToParse...)
	if err != nil {
		logger.Error("Unable to parse referenced template", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Write template to response
	err = parsedTemplates.ExecuteTemplate(w, "base", h.cookiePolicy)
	if err != nil {
		logger.Error("Unable to execute parsed template", zap.Error(err))
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

}

func (h *Handler) AuthLogin(w http.ResponseWriter, r *http.Request) {

	logger := logger.AcquireFrom(r.Context())

	// list of template files to parse, must be in order of inheritence
	templateFilesToParse := []string{
		"internal/webapp/ui/html/base.tmpl.html",
		"internal/webapp/ui/html/pages/base-auth.tmpl.html",
		"internal/webapp/ui/html/partials/auth/auth-login.tmpl.html",
		"internal/webapp/ui/html/partials/shared/tailwind-dash-script.tmpl.html",
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

func (h *Handler) AuthSignup(w http.ResponseWriter, r *http.Request) {

	logger := logger.AcquireFrom(r.Context())

	// list of template files to parse, must be in order of inheritence
	templateFilesToParse := []string{
		"internal/webapp/ui/html/base.tmpl.html",
		"internal/webapp/ui/html/pages/base-auth.tmpl.html",
		"internal/webapp/ui/html/partials/auth/auth-signup.tmpl.html",
		"internal/webapp/ui/html/partials/shared/tailwind-dash-script.tmpl.html",
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

func (h *Handler) AuthResetPassword(w http.ResponseWriter, r *http.Request) {

	logger := logger.AcquireFrom(r.Context())

	// list of template files to parse, must be in order of inheritence
	templateFilesToParse := []string{
		"internal/webapp/ui/html/base.tmpl.html",
		"internal/webapp/ui/html/pages/base-auth.tmpl.html",
		"internal/webapp/ui/html/partials/auth/auth-reset-password.tmpl.html",
		"internal/webapp/ui/html/partials/shared/tailwind-dash-script.tmpl.html",
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

func (h *Handler) ComingSoon(w http.ResponseWriter, r *http.Request) {

	logger := logger.AcquireFrom(r.Context())

	// list of template files to parse, must be in order of inheritence
	templateFilesToParse := []string{
		"internal/webapp/ui/html/base.tmpl.html",
		"internal/webapp/ui/html/pages/coming-soon.tmpl.html",
		"internal/webapp/ui/html/partials/shared/tailwind-dash-script.tmpl.html",
		"internal/webapp/ui/html/partials/shared/preloader.tmpl.html",
		"internal/webapp/ui/html/partials/coming-soon/countdown-timer.tmpl.html",
		"internal/webapp/ui/html/partials/shared/social-links.tmpl.html",
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

func (h *Handler) Dash(w http.ResponseWriter, r *http.Request) {

	logger := logger.AcquireFrom(r.Context())

	if r.Header.Get(common.WebPartialHttpRequestHeader) == "true" {

		w.Header().Add(common.CacheSkipHttpResponseHeader, "true")

		// list of template files to parse, must be in order of inheritence
		templateFilesToParse := []string{
			"internal/webapp/ui/html/partials/dash/dash-ecommerce.tmpl.html",
			"internal/webapp/ui/html/partials/dash/chart-area.tmpl.html",
			"internal/webapp/ui/html/partials/dash/chart-bar.tmpl.html",
			"internal/webapp/ui/html/partials/dash/chart-donut.tmpl.html",
			"internal/webapp/ui/html/partials/dash/map-01.tmpl.html",
			"internal/webapp/ui/html/partials/dash/table-01.tmpl.html",
		}

		// Parse template
		parsedTemplates, err := template.ParseFS(h.embeddedFileSystem, templateFilesToParse...)
		if err != nil {
			logger.Error("Unable to parse referenced template", zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Write template to response
		err = parsedTemplates.ExecuteTemplate(w, "dash-main", webapphelpers.UpdateSiteTitleHelper{
			EnableTitleUpdate: true,
			NewTitle:          "E Commerce",
		})
		if err != nil {
			logger.Error("Unable to execute parsed templates", zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		return
	}

	// list of template files to parse, must be in order of inheritence
	templateFilesToParse := []string{
		"internal/webapp/ui/html/base.tmpl.html",
		"internal/webapp/ui/html/pages/dash.tmpl.html",
		"internal/webapp/ui/html/partials/dash/dash-ecommerce.tmpl.html",
		"internal/webapp/ui/html/partials/shared/tailwind-dash-script.tmpl.html",
		"internal/webapp/ui/html/partials/shared/preloader.tmpl.html",
		"internal/webapp/ui/html/partials/dash/dash-sidebar.tmpl.html",
		"internal/webapp/ui/html/partials/dash/dash-header.tmpl.html",
		"internal/webapp/ui/html/partials/dash/chart-area.tmpl.html",
		"internal/webapp/ui/html/partials/dash/chart-bar.tmpl.html",
		"internal/webapp/ui/html/partials/dash/chart-donut.tmpl.html",
		"internal/webapp/ui/html/partials/dash/map-01.tmpl.html",
		"internal/webapp/ui/html/partials/dash/table-01.tmpl.html",
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

func (h *Handler) DashCalendar(w http.ResponseWriter, r *http.Request) {

	logger := logger.AcquireFrom(r.Context())

	if r.Header.Get(common.WebPartialHttpRequestHeader) == "true" {

		w.Header().Add(common.CacheSkipHttpResponseHeader, "true")

		// Parse template
		parsedTemplates, err := template.ParseFS(h.embeddedFileSystem, "internal/webapp/ui/html/partials/dash/dash-calendar.tmpl.html")
		if err != nil {
			logger.Error("Unable to parse referenced template", zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Write template to response
		err = parsedTemplates.ExecuteTemplate(w, "dash-main", webapphelpers.UpdateSiteTitleHelper{
			EnableTitleUpdate: true,
			NewTitle:          "Calendar",
		})
		if err != nil {
			logger.Error("Unable to execute parsed templates", zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		return
	}

	// list of template files to parse, must be in order of inheritence
	templateFilesToParse := []string{
		"internal/webapp/ui/html/base.tmpl.html",
		"internal/webapp/ui/html/pages/dash.tmpl.html",
		"internal/webapp/ui/html/partials/dash/dash-sidebar.tmpl.html",
		"internal/webapp/ui/html/partials/dash/dash-calendar.tmpl.html",
		"internal/webapp/ui/html/partials/shared/tailwind-dash-script.tmpl.html",
		"internal/webapp/ui/html/partials/shared/preloader.tmpl.html",
		"internal/webapp/ui/html/partials/dash/dash-header.tmpl.html",
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

func (h *Handler) DashProfile(w http.ResponseWriter, r *http.Request) {

	logger := logger.AcquireFrom(r.Context())

	if r.Header.Get(common.WebPartialHttpRequestHeader) == "true" {

		w.Header().Add(common.CacheSkipHttpResponseHeader, "true")

		// Parse template
		parsedTemplates, err := template.ParseFS(h.embeddedFileSystem, "internal/webapp/ui/html/partials/dash/dash-profile.tmpl.html")
		if err != nil {
			logger.Error("Unable to parse referenced template", zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Write template to response
		err = parsedTemplates.ExecuteTemplate(w, "dash-main", webapphelpers.UpdateSiteTitleHelper{
			EnableTitleUpdate: true,
			NewTitle:          "Profile",
		})
		if err != nil {
			logger.Error("Unable to execute parsed templates", zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		return
	}

	// list of template files to parse, must be in order of inheritence
	templateFilesToParse := []string{
		"internal/webapp/ui/html/base.tmpl.html",
		"internal/webapp/ui/html/pages/dash.tmpl.html",
		"internal/webapp/ui/html/partials/dash/dash-sidebar.tmpl.html",
		"internal/webapp/ui/html/partials/dash/dash-profile.tmpl.html",
		"internal/webapp/ui/html/partials/shared/tailwind-dash-script.tmpl.html",
		"internal/webapp/ui/html/partials/shared/preloader.tmpl.html",
		"internal/webapp/ui/html/partials/dash/dash-header.tmpl.html",
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

func (h *Handler) DashBlank(w http.ResponseWriter, r *http.Request) {

	logger := logger.AcquireFrom(r.Context())

	if r.Header.Get(common.WebPartialHttpRequestHeader) == "true" {

		w.Header().Add(common.CacheSkipHttpResponseHeader, "true")

		// Parse template
		parsedTemplates, err := template.ParseFS(h.embeddedFileSystem, "internal/webapp/ui/html/partials/dash/dash-blank.tmpl.html")
		if err != nil {
			logger.Error("Unable to parse referenced template", zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Write template to response
		err = parsedTemplates.ExecuteTemplate(w, "dash-main", webapphelpers.UpdateSiteTitleHelper{
			EnableTitleUpdate: true,
			NewTitle:          "Blank Page",
		})
		if err != nil {
			logger.Error("Unable to execute parsed templates", zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		return
	}

	// list of template files to parse, must be in order of inheritence
	templateFilesToParse := []string{
		"internal/webapp/ui/html/base.tmpl.html",
		"internal/webapp/ui/html/pages/dash.tmpl.html",
		"internal/webapp/ui/html/partials/dash/dash-sidebar.tmpl.html",
		"internal/webapp/ui/html/partials/dash/dash-blank.tmpl.html",
		"internal/webapp/ui/html/partials/shared/tailwind-dash-script.tmpl.html",
		"internal/webapp/ui/html/partials/shared/preloader.tmpl.html",
		"internal/webapp/ui/html/partials/dash/dash-header.tmpl.html",
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

func (h *Handler) DashFormElements(w http.ResponseWriter, r *http.Request) {

	logger := logger.AcquireFrom(r.Context())

	if r.Header.Get(common.WebPartialHttpRequestHeader) == "true" {

		w.Header().Add(common.CacheSkipHttpResponseHeader, "true")

		// list of template files to parse, must be in order of inheritence
		templateFilesToParse := []string{
			"internal/webapp/ui/html/partials/dash/dash-form-elements.tmpl.html",
			"internal/webapp/ui/html/partials/dash/form-elements-checkbox-radio.tmpl.html",
			"internal/webapp/ui/html/partials/dash/form-elements-file-upload.tmpl.html",
			"internal/webapp/ui/html/partials/dash/form-elements-input-fields.tmpl.html",
			"internal/webapp/ui/html/partials/dash/form-elements-select-input.tmpl.html",
			"internal/webapp/ui/html/partials/dash/form-elements-switch-input.tmpl.html",
			"internal/webapp/ui/html/partials/dash/form-elements-textarea-fields.tmpl.html",
			"internal/webapp/ui/html/partials/dash/form-elements-time-date.tmpl.html",
		}
		// Parse template
		parsedTemplates, err := template.ParseFS(h.embeddedFileSystem, templateFilesToParse...)
		if err != nil {
			logger.Error("Unable to parse referenced template", zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Write template to response
		err = parsedTemplates.ExecuteTemplate(w, "dash-main", webapphelpers.UpdateSiteTitleHelper{
			EnableTitleUpdate: true,
			NewTitle:          "Form Elements",
		})
		if err != nil {
			logger.Error("Unable to execute parsed templates", zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		return
	}

	// list of template files to parse, must be in order of inheritence
	templateFilesToParse := []string{
		"internal/webapp/ui/html/base.tmpl.html",
		"internal/webapp/ui/html/pages/dash.tmpl.html",
		"internal/webapp/ui/html/partials/dash/dash-sidebar.tmpl.html",
		"internal/webapp/ui/html/partials/dash/dash-form-elements.tmpl.html",
		"internal/webapp/ui/html/partials/dash/form-elements-checkbox-radio.tmpl.html",
		"internal/webapp/ui/html/partials/dash/form-elements-file-upload.tmpl.html",
		"internal/webapp/ui/html/partials/dash/form-elements-input-fields.tmpl.html",
		"internal/webapp/ui/html/partials/dash/form-elements-select-input.tmpl.html",
		"internal/webapp/ui/html/partials/dash/form-elements-switch-input.tmpl.html",
		"internal/webapp/ui/html/partials/dash/form-elements-textarea-fields.tmpl.html",
		"internal/webapp/ui/html/partials/dash/form-elements-time-date.tmpl.html",
		"internal/webapp/ui/html/partials/shared/tailwind-dash-script.tmpl.html",
		"internal/webapp/ui/html/partials/shared/preloader.tmpl.html",
		"internal/webapp/ui/html/partials/dash/dash-header.tmpl.html",
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

func (h *Handler) DashFormLayout(w http.ResponseWriter, r *http.Request) {

	logger := logger.AcquireFrom(r.Context())

	if r.Header.Get(common.WebPartialHttpRequestHeader) == "true" {

		w.Header().Add(common.CacheSkipHttpResponseHeader, "true")

		// list of template files to parse, must be in order of inheritence
		templateFilesToParse := []string{
			"internal/webapp/ui/html/partials/dash/dash-form-layout.tmpl.html",
			"internal/webapp/ui/html/partials/dash/form-layout-contact-form.tmpl.html",
			"internal/webapp/ui/html/partials/dash/form-layout-sign-up-form.tmpl.html",
			"internal/webapp/ui/html/partials/dash/form-layout-sign-in-form.tmpl.html",
		}
		// Parse template
		parsedTemplates, err := template.ParseFS(h.embeddedFileSystem, templateFilesToParse...)
		if err != nil {
			logger.Error("Unable to parse referenced template", zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		// Write template to response
		err = parsedTemplates.ExecuteTemplate(w, "dash-main", webapphelpers.UpdateSiteTitleHelper{
			EnableTitleUpdate: true,
			NewTitle:          "Form Layout",
		})
		if err != nil {
			logger.Error("Unable to execute parsed templates", zap.Error(err))
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}

		return
	}

	// list of template files to parse, must be in order of inheritence
	templateFilesToParse := []string{
		"internal/webapp/ui/html/base.tmpl.html",
		"internal/webapp/ui/html/pages/dash.tmpl.html",
		"internal/webapp/ui/html/partials/dash/dash-sidebar.tmpl.html",
		"internal/webapp/ui/html/partials/dash/dash-form-layout.tmpl.html",
		"internal/webapp/ui/html/partials/dash/form-layout-contact-form.tmpl.html",
		"internal/webapp/ui/html/partials/dash/form-layout-sign-up-form.tmpl.html",
		"internal/webapp/ui/html/partials/dash/form-layout-sign-in-form.tmpl.html",
		"internal/webapp/ui/html/partials/shared/tailwind-dash-script.tmpl.html",
		"internal/webapp/ui/html/partials/shared/preloader.tmpl.html",
		"internal/webapp/ui/html/partials/dash/dash-header.tmpl.html",
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
