package emailprovider

import (
	"encoding/json"
	"fmt"
	stdhtml "html"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/router"
)

const DefaultLocalInboxRoutePrefix = "/_ghatd/local/emails"

const localInboxRawHTMLContentSecurityPolicy = "sandbox allow-popups allow-popups-to-escape-sandbox allow-top-navigation-by-user-activation"

var emailLinkPattern = regexp.MustCompile(`(?i)href\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)

// AttachLocalInboxRoutesRequest holds local email inbox route configuration.
type AttachLocalInboxRoutesRequest struct {
	// Router is the GHATD router that will receive the local inbox routes.
	Router *router.Router

	// Provider is the local logging email provider whose captured emails will be rendered.
	Provider *LoggingEmailProvider

	// Prefix optionally overrides the local inbox route prefix.
	// It defaults to DefaultLocalInboxRoutePrefix when left empty.
	Prefix string

	// AllowRemote permits non-loopback clients to access the inbox.
	// Keep this false unless another trusted local proxy protects the route.
	AllowRemote bool
}

// AttachLocalInboxRoutes attaches opt-in local email inbox routes.
// These routes render raw local email bodies and should only be enabled in
// trusted local development environments.
func AttachLocalInboxRoutes(request *AttachLocalInboxRoutesRequest) error {
	if request == nil {
		return fmt.Errorf("emailprovider/local-inbox-routes-nil-request")
	}
	if request.Router == nil {
		return fmt.Errorf("emailprovider/local-inbox-routes-missing-router")
	}
	if request.Provider == nil {
		return fmt.Errorf("emailprovider/local-inbox-routes-missing-provider")
	}

	prefix := normaliseLocalInboxPrefix(request.Prefix)
	httpRouter := request.Router.GetRouter()
	if httpRouter == nil {
		return fmt.Errorf("emailprovider/local-inbox-routes-missing-http-router")
	}

	inboxRouter := httpRouter.PathPrefix(prefix).Subrouter()
	handlers := &localInboxHandlers{
		provider: request.Provider,
		prefix:   prefix,
	}
	inboxRouter.HandleFunc("", handlers.index).Methods(http.MethodGet)
	inboxRouter.HandleFunc("/", handlers.index).Methods(http.MethodGet)
	inboxRouter.HandleFunc("/clear", handlers.clear).Methods(http.MethodPost)
	inboxRouter.HandleFunc("/api/emails", handlers.apiList).Methods(http.MethodGet)
	inboxRouter.HandleFunc("/{messageID}", handlers.detail).Methods(http.MethodGet)
	inboxRouter.HandleFunc("/{messageID}/html", handlers.rawHTML).Methods(http.MethodGet)
	inboxRouter.Use(localInboxNoStoreMiddleware)
	if !request.AllowRemote {
		inboxRouter.Use(localInboxLocalOnlyMiddleware)
	}

	return nil
}

type localInboxHandlers struct {
	provider *LoggingEmailProvider
	prefix   string
}

type localInboxIndexView struct {
	Prefix string
	Emails []localInboxEmailSummary
}

type localInboxDetailView struct {
	Prefix      string
	Email       LocalEmail
	CreatedAt   string
	RawHTMLPath string
	Links       []string
}

type localInboxEmailSummary struct {
	MessageID  string   `json:"messageId"`
	To         string   `json:"to"`
	From       string   `json:"from"`
	Subject    string   `json:"subject"`
	CreatedAt  string   `json:"createdAt"`
	DetailPath string   `json:"detailPath"`
	Links      []string `json:"links"`
}

// index renders the local email inbox list page.
func (h *localInboxHandlers) index(w http.ResponseWriter, r *http.Request) {
	emails := h.emailSummaries()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = localInboxIndexTemplate.Execute(w, localInboxIndexView{
		Prefix: h.prefix,
		Emails: emails,
	})
}

// detail renders a captured email detail page with metadata, preview, and links.
func (h *localInboxHandlers) detail(w http.ResponseWriter, r *http.Request) {
	email, ok := h.emailByRequest(w, r)
	if !ok {
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = localInboxDetailTemplate.Execute(w, localInboxDetailView{
		Prefix:      h.prefix,
		Email:       email,
		CreatedAt:   formatLocalEmailTime(email.CreatedAt),
		RawHTMLPath: h.rawHTMLPath(email.MessageID),
		Links:       extractEmailLinks(email.HTMLBody),
	})
}

// rawHTML renders the captured email HTML body for previewing in a browser.
func (h *localInboxHandlers) rawHTML(w http.ResponseWriter, r *http.Request) {
	email, ok := h.emailByRequest(w, r)
	if !ok {
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", localInboxRawHTMLContentSecurityPolicy)
	if strings.TrimSpace(email.HTMLBody) != "" {
		_, _ = w.Write([]byte(email.HTMLBody))
		return
	}

	_, _ = fmt.Fprintf(w, "<!doctype html><html><body><pre>%s</pre></body></html>", template.HTMLEscapeString(email.TextBody))
}

// clear removes all captured local emails and redirects back to the inbox list.
func (h *localInboxHandlers) clear(w http.ResponseWriter, r *http.Request) {
	h.provider.Inbox().Clear()
	http.Redirect(w, r, h.prefix, http.StatusSeeOther)
}

// apiList writes JSON summaries for the captured local emails.
func (h *localInboxHandlers) apiList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(h.emailSummaries())
}

// emailByRequest resolves the route message ID to a captured local email.
func (h *localInboxHandlers) emailByRequest(w http.ResponseWriter, r *http.Request) (LocalEmail, bool) {
	messageID := mux.Vars(r)["messageID"]
	email, ok := h.provider.Inbox().Get(messageID)
	if !ok {
		http.NotFound(w, r)
		return LocalEmail{}, false
	}
	return email, true
}

// emailSummaries converts captured local emails into list and API summaries.
func (h *localInboxHandlers) emailSummaries() []localInboxEmailSummary {
	emails := h.provider.Inbox().List()
	summaries := make([]localInboxEmailSummary, 0, len(emails))
	for _, email := range emails {
		summaries = append(summaries, localInboxEmailSummary{
			MessageID:  email.MessageID,
			To:         email.To,
			From:       email.From,
			Subject:    email.Subject,
			CreatedAt:  formatLocalEmailTime(email.CreatedAt),
			DetailPath: h.detailPath(email.MessageID),
			Links:      extractEmailLinks(email.HTMLBody),
		})
	}
	return summaries
}

// detailPath returns the inbox detail path for a captured email message ID.
func (h *localInboxHandlers) detailPath(messageID string) string {
	return h.prefix + "/" + url.PathEscape(messageID)
}

// rawHTMLPath returns the raw rendered-email path for a captured email message ID.
func (h *localInboxHandlers) rawHTMLPath(messageID string) string {
	return h.detailPath(messageID) + "/html"
}

// normaliseLocalInboxPrefix normalises a configured route prefix for mux routing.
func normaliseLocalInboxPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = DefaultLocalInboxRoutePrefix
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	prefix = strings.TrimRight(prefix, "/")
	if prefix == "" {
		return "/"
	}
	return prefix
}

// localInboxNoStoreMiddleware marks local inbox responses as uncacheable.
func localInboxNoStoreMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setLocalInboxNoStoreHeaders(w)
		next.ServeHTTP(w, r)
	})
}

// localInboxLocalOnlyMiddleware rejects requests that do not originate from loopback.
func localInboxLocalOnlyMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLocalInboxRequest(r) {
			http.Error(w, "local email inbox is only available from localhost", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// setLocalInboxNoStoreHeaders prevents stale local inbox pages and summaries.
func setLocalInboxNoStoreHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

// isLocalInboxRequest reports whether a request remote address is localhost or loopback.
func isLocalInboxRequest(r *http.Request) bool {
	host := r.RemoteAddr
	if splitHost, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = splitHost
	}
	host = strings.TrimSpace(host)
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// formatLocalEmailTime formats a captured email timestamp for inbox display.
func formatLocalEmailTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02 15:04:05 MST")
}

// extractEmailLinks extracts unique web links from a captured HTML email body.
func extractEmailLinks(htmlBody string) []string {
	matches := emailLinkPattern.FindAllStringSubmatch(htmlBody, -1)
	links := make([]string, 0, len(matches))
	seen := map[string]bool{}
	for _, match := range matches {
		link := ""
		for i := 1; i < len(match); i++ {
			if match[i] != "" {
				link = match[i]
				break
			}
		}
		link = strings.TrimSpace(stdhtml.UnescapeString(link))
		if link == "" || !isLocalInboxWebLink(link) || seen[link] {
			continue
		}
		seen[link] = true
		links = append(links, link)
	}
	return links
}

// isLocalInboxWebLink reports whether a link is safe to expose as a web action.
func isLocalInboxWebLink(link string) bool {
	parsed, err := url.Parse(link)
	if err != nil {
		return false
	}
	if parsed.Scheme == "" {
		return strings.HasPrefix(link, "/") && !strings.HasPrefix(link, "//")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return true
	default:
		return false
	}
}

var localInboxIndexTemplate = template.Must(template.New("local-email-inbox-index").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>GHATD Local Email Inbox</title>
  <style>
    :root { color-scheme: light dark; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    body { margin: 0; background: #f7f7f4; color: #1d2528; }
    main { max-width: 1040px; margin: 0 auto; padding: 28px 20px 48px; }
    header { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 20px; }
    h1 { font-size: 24px; line-height: 1.2; margin: 0; font-weight: 720; }
    p { margin: 4px 0 0; color: #5d666b; }
    form { margin: 0; }
    button, .button { border: 1px solid #cfd7d8; background: #fff; color: #1d2528; border-radius: 6px; padding: 8px 11px; text-decoration: none; font: inherit; cursor: pointer; }
    button:hover, .button:hover { border-color: #94a3a6; }
    table { width: 100%; border-collapse: collapse; background: #fff; border: 1px solid #d9e0e1; }
    th, td { border-bottom: 1px solid #e5eaeb; padding: 12px; text-align: left; vertical-align: top; font-size: 14px; }
    th { background: #eef3f3; font-size: 12px; text-transform: uppercase; color: #546064; letter-spacing: .06em; }
    tr:last-child td { border-bottom: 0; }
    .muted { color: #687477; }
    .subject { font-weight: 650; }
    .links { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 6px; }
    .links a { font-size: 12px; }
    .empty { background: #fff; border: 1px solid #d9e0e1; padding: 28px; border-radius: 6px; }
  </style>
</head>
<body>
  <main>
    <header>
      <div>
        <h1>GHATD Local Email Inbox</h1>
        <p>Captured local emails are kept in memory and reset when the process restarts.</p>
      </div>
      <form method="post" action="{{.Prefix}}/clear"><button type="submit">Clear</button></form>
    </header>
    {{if .Emails}}
    <table>
      <thead><tr><th>Sent</th><th>Subject</th><th>Recipient</th><th>Actions</th></tr></thead>
      <tbody>
      {{range .Emails}}
        <tr>
          <td class="muted">{{.CreatedAt}}</td>
          <td><div class="subject">{{.Subject}}</div><div class="muted">{{.From}}</div></td>
          <td>{{.To}}</td>
          <td>
            <a class="button" href="{{.DetailPath}}">Open</a>
            {{if .Links}}<div class="links">{{range .Links}}<a class="button" href="{{.}}" target="_blank" rel="noreferrer">Link</a>{{end}}</div>{{end}}
          </td>
        </tr>
      {{end}}
      </tbody>
    </table>
    {{else}}
    <div class="empty">No local emails captured yet.</div>
    {{end}}
  </main>
</body>
</html>`))

var localInboxDetailTemplate = template.Must(template.New("local-email-inbox-detail").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Email.Subject}} - GHATD Local Email Inbox</title>
  <style>
    :root { color-scheme: light dark; font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    body { margin: 0; background: #f7f7f4; color: #1d2528; }
    main { max-width: 1180px; margin: 0 auto; padding: 24px 20px 48px; }
    nav { margin-bottom: 18px; }
    a { color: #0f6664; }
    h1 { font-size: 22px; line-height: 1.25; margin: 0 0 12px; }
    dl { display: grid; grid-template-columns: 110px 1fr; gap: 8px 14px; background: #fff; border: 1px solid #d9e0e1; padding: 14px; margin: 0 0 16px; }
    dt { color: #607074; font-weight: 650; }
    dd { margin: 0; overflow-wrap: anywhere; }
    .actions { display: flex; gap: 8px; flex-wrap: wrap; margin: 14px 0 18px; }
    .button { border: 1px solid #cfd7d8; background: #fff; color: #1d2528; border-radius: 6px; padding: 8px 11px; text-decoration: none; font: inherit; }
    .button:hover { border-color: #94a3a6; }
    iframe { width: 100%; min-height: 620px; border: 1px solid #cfd7d8; background: #fff; }
    textarea, pre { width: 100%; box-sizing: border-box; min-height: 180px; border: 1px solid #cfd7d8; background: #fff; color: #1d2528; padding: 12px; overflow: auto; }
    .links { display: flex; flex-wrap: wrap; gap: 8px; }
  </style>
</head>
<body>
  <main>
    <nav><a href="{{.Prefix}}">Back to inbox</a></nav>
    <h1>{{.Email.Subject}}</h1>
    <dl>
      <dt>To</dt><dd>{{.Email.To}}</dd>
      <dt>From</dt><dd>{{.Email.From}}</dd>
      <dt>Reply To</dt><dd>{{.Email.ReplyTo}}</dd>
      <dt>Sent</dt><dd>{{.CreatedAt}}</dd>
      <dt>Message ID</dt><dd>{{.Email.MessageID}}</dd>
    </dl>
    <div class="actions">
      <a class="button" href="{{.RawHTMLPath}}" target="_blank" rel="noreferrer">Open rendered email</a>
      {{range .Links}}<a class="button" href="{{.}}" target="_blank" rel="noreferrer">Open link</a>{{end}}
    </div>
    <iframe title="Rendered email" src="{{.RawHTMLPath}}" sandbox=""></iframe>
    <h2>HTML</h2>
    <textarea readonly>{{.Email.HTMLBody}}</textarea>
    {{if .Email.TextBody}}<h2>Text</h2><pre>{{.Email.TextBody}}</pre>{{end}}
  </main>
</body>
</html>`))
