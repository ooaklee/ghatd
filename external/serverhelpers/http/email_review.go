package serverhttp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/ooaklee/ghatd/external/emailmanager"
	"github.com/ooaklee/ghatd/external/emailprovider"
	"github.com/ooaklee/ghatd/external/router"
)

// EmailReviewConfig enables a fixed-message loopback development harness. The
// manager must use local capture providers; the helper never accepts a browser
// recipient, message body, credential or provider override.
type EmailReviewConfig struct {
	Environment    string
	FrontendOrigin string
	FromAddress    string
	Manager        *emailmanager.EmailManager
	// AllowPrivateNetwork also permits directly connected private-address peers
	// for an explicitly configured local container/proxy workflow. Forwarded
	// headers never grant this permission; POST still needs a loopback origin.
	AllowPrivateNetwork bool
}

// AttachLocalEmailReview registers the development-only local inbox harness.
// Requests require an allowed local peer and POST requires a matching loopback HTTP
// origin. No forwarded header can establish local access. Borrowed providers
// retain lifecycle ownership and must be constructed in local capture mode.
func AttachLocalEmailReview(routes *router.Router, cfg EmailReviewConfig) error {
	if routes == nil || cfg.Manager == nil || cfg.Environment != "local" {
		return fmt.Errorf("local email routing review requires local configuration and manager")
	}
	for _, purpose := range []emailprovider.MailType{emailprovider.Transactional, emailprovider.Marketing} {
		local, ok := cfg.Manager.ProviderForMailType(purpose).(interface{ IsLocalOutputProvider() bool })
		if !ok || !local.IsLocalOutputProvider() {
			return fmt.Errorf("local email routing review requires captured delivery")
		}
	}
	m := cfg.Manager
	handler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Cache-Skip", "true")
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ip := net.ParseIP(host)
		if ip == nil || (!ip.IsLoopback() && !(cfg.AllowPrivateNetwork && ip.IsPrivate())) {
			http.Error(w, "local only", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			// A failed response write cannot be repaired with another HTTP response.
			_, _ = fmt.Fprint(w, `<!doctype html><html lang="en"><meta charset="utf-8"><title>Local email routing review</title><h1>Local email routing review</h1><p>Messages are captured locally. Provider labels show the selected route.</p><form method="post"><select name="process"><option value="login">Login</option><option value="verification">Verification</option><option value="transactional">Custom transactional</option><option value="marketing">Custom marketing</option><option value="rendered">Pre-rendered transactional</option></select><button>Capture test email</button></form><p><a href="/_ghatd/local/emails">Open local inbox</a></p></html>`)
			return
		}
		origin := r.Header.Get("Origin")
		parsed, err := url.Parse(origin)
		if err != nil || origin == "" || parsed.Scheme != "http" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1") || (parsed.Host != r.Host && origin != cfg.FrontendOrigin) {
			http.Error(w, "invalid origin", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 1024)
		if r.ParseForm() != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		var receipt *emailmanager.SendReceipt
		switch r.Form.Get("process") {
		case "login":
			receipt, err = m.SendLoginEmailWithResult(r.Context(), &emailmanager.SendLoginEmailRequest{Email: "routing-review@example.test", Token: "local-review-placeholder", Code: "12345678"})
		case "verification":
			receipt, err = m.SendVerificationEmailWithResult(r.Context(), &emailmanager.SendVerificationEmailRequest{Email: "routing-review@example.test", FirstName: "Local", LastName: "Review", Token: "local-review-placeholder", Code: "12345678"})
		case "transactional", "marketing":
			purpose := emailprovider.Transactional
			if r.Form.Get("process") == "marketing" {
				purpose = emailprovider.Marketing
			}
			receipt, err = m.SendCustomEmailWithResult(r.Context(), &emailmanager.SendCustomEmailRequest{MailType: purpose, EmailTo: "routing-review@example.test", EmailSubject: "Local routing review: " + string(purpose), EmailBody: "<p>This test message was captured locally.</p>", TextBody: "This test message was captured locally."})
		case "rendered":
			receipt, err = m.SendEmailWithResult(r.Context(), &emailmanager.SendEmailRequest{MailType: emailprovider.Transactional, To: "routing-review@example.test", From: cfg.FromAddress, Subject: "Local routing review: pre-rendered", TextBody: "This test message was captured locally."})
		default:
			http.Error(w, "unknown process", http.StatusBadRequest)
			return
		}
		if err != nil {
			http.Error(w, "email review failed", http.StatusServiceUnavailable)
			return
		}
		if strings.Contains(r.Header.Get("Accept"), "text/html") {
			http.Redirect(w, r, "/_ghatd/local/emails", http.StatusSeeOther)
			return
		}
		// Encode before committing the response so serialization failures remain errors.
		var body bytes.Buffer
		if err := json.NewEncoder(&body).Encode(receipt); err != nil {
			http.Error(w, "email review failed", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// A client disconnect must not retry the response or the captured email.
		_, _ = w.Write(body.Bytes())
	}
	routes.GetRouter().HandleFunc("/_ghatd/local/emails/review", handler).Methods(http.MethodGet, http.MethodPost)
	return nil
}
