package waitlist

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"

	"errors"
	"github.com/gorilla/mux"
	grouter "github.com/ooaklee/ghatd/external/router"
)

// RouteConfig supplies display-only host configuration for public/admin routes.
type RouteConfig struct {
	// Columns overrides the CSV projection; nil retains the standard six columns.
	Columns []CSVColumn
	// ExportFilename is a plain ASCII .csv basename, not a path or header value.
	// Empty uses "prerelease-waitlist.csv". Invalid names fail route registration.
	ExportFilename string
}

// CSVColumn projects one administrative value. The shared writer escapes formula
// prefixes for every header/value, including values produced by host callbacks.
type CSVColumn struct {
	Header string
	Value  func(Entry) string
}

var exportFilename = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,120}\.csv$`)

// AttachRoutes requires the application's public rate limiter and admin guard.
// Registration is public; the audience is never exposed to an anonymous user.
func AttachRoutes(router *grouter.Router, store Store, rateLimit, adminOnly mux.MiddlewareFunc) error {
	return AttachRoutesWithConfig(router, store, rateLimit, adminOnly, RouteConfig{})
}

// AttachRoutesWithConfig preserves signup admission while allowing a host
// CSV filename and projection. Both guards are mandatory; no route is added on invalid input.
func AttachRoutesWithConfig(router *grouter.Router, store Store, rateLimit, adminOnly mux.MiddlewareFunc, config RouteConfig) error {
	if router == nil || store == nil || rateLimit == nil || adminOnly == nil {
		return errors.New("waitlist routes require storage, rate limiting and admin authentication")
	}
	if config.ExportFilename == "" {
		config.ExportFilename = "prerelease-waitlist.csv"
	}
	if !exportFilename.MatchString(config.ExportFilename) {
		return errors.New("waitlist export requires a plain CSV filename")
	}
	if config.Columns != nil {
		if len(config.Columns) == 0 || len(config.Columns) > 32 {
			return errors.New("invalid waitlist CSV columns")
		}
		seen := map[string]bool{}
		for _, column := range config.Columns {
			if !configIdentifier.MatchString(column.Header) || column.Value == nil || seen[column.Header] {
				return errors.New("invalid waitlist CSV column")
			}
			seen[column.Header] = true
		}
	}
	h := &handler{store: store, exportFilename: config.ExportFilename, columns: slices.Clone(config.Columns)}
	public := router.NewRouteGroup("/api/v1/waitlist", grouter.OptionalActive, rateLimit)
	public.Handle(grouter.RouteDefinition{Path: "", Operation: "waitlist.Join", Methods: []string{http.MethodPost}}, h.join)
	preflight := router.NewRouteGroup("/api/v1/waitlist", grouter.Public, nil)
	preflight.Handle(grouter.RouteDefinition{Path: "", Operation: "waitlist.Preflight", Methods: []string{http.MethodOptions}}, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	admin := router.NewRouteGroup("/api/v1/waitlist", grouter.AdminSession, adminOnly)
	admin.Handle(grouter.RouteDefinition{Path: "/export", Operation: "waitlist.Export", Methods: []string{http.MethodGet, http.MethodOptions}}, h.export)
	return nil
}

type handler struct {
	columns        []CSVColumn
	store          Store
	exportFilename string
}

func canonicalEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if len(email) > 254 || strings.ContainsAny(email, "\r\n\t ") {
		return "", false
	}
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || address.Name != "" {
		return "", false
	}
	parts := strings.Split(email, "@")
	if len(parts) != 2 || len(parts[0]) > 64 || !strings.Contains(parts[1], ".") {
		return "", false
	}
	for _, label := range strings.Split(parts[1], ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return "", false
			}
		}
	}
	return email, true
}

func (h *handler) join(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "Please submit JSON."})
		return
	}
	var input struct {
		Email   string `json:"email"`
		Consent bool   `json:"consent"`
		Source  string `json:"source"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Please check your signup details."})
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Please check your signup details."})
		return
	}
	email, valid := canonicalEmail(input.Email)
	if !valid {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "invalid_email", "error": "Please enter a valid email address."})
		return
	}
	if !input.Consent || input.Source != "landing" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "A valid email and consent to prerelease emails are required."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if err := h.store.Join(ctx, email); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "We could not save your place. Please try again."})
		return
	}
	// Same response for a new or existing address; never reveal membership.
	writeJSON(w, http.StatusCreated, map[string]any{"data": map[string]string{"status": "accepted"}})
}

func (h *handler) export(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	entries, err := h.store.Export(ctx)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Waitlist export unavailable."})
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	filename := h.exportFilename
	if filename == "" {
		filename = "prerelease-waitlist.csv"
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	writer := csv.NewWriter(w)
	columns := h.columns
	if columns == nil {
		columns = []CSVColumn{
			{"email", func(e Entry) string { return e.Email }},
			{"joined_at", func(e Entry) string { return e.JoinedAt.Format(time.RFC3339) }},
			{"consent_version", func(e Entry) string { return e.ConsentVersion }},
			{"source", func(e Entry) string { return e.Source }},
			{"announcement_status", func(e Entry) string { return e.AnnouncementState }},
			{"provider_message_id", func(e Entry) string { return e.ProviderMessageID }},
		}
	}
	headers := make([]string, len(columns))
	for i, column := range columns {
		headers[i] = csvCell(column.Header)
	}
	if err := writer.Write(headers); err != nil {
		return
	}
	for _, entry := range entries {
		row := make([]string, len(columns))
		for i, column := range columns {
			row[i] = csvCell(column.Value(entry))
		}
		if err := writer.Write(row); err != nil {
			return
		}
	}
	writer.Flush()
}

func csvCell(value string) string {
	trimmed := strings.TrimLeftFunc(value, unicode.IsSpace)
	if strings.ContainsAny(value[:min(1, len(value))], "\t\r\n") || strings.ContainsAny(trimmed[:min(1, len(trimmed))], "=+-@") {
		return "'" + value
	}
	return value
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
