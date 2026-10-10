package internationalisationmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const BasePath = "/api/v1/i18n"

// Kind names one catalogue resource exposed over HTTP: currencies, phone codes,
// flags or timezones.
type Kind string

const (
	Currencies Kind = "currencies"
	PhoneCodes Kind = "phonecodes"
	Flags      Kind = "flags"
	Timezones  Kind = "timezones"
)

// valid reports whether k is one of the four supported catalogue kinds.
func (k Kind) valid() bool { return k == Currencies || k == PhoneCodes || k == Flags || k == Timezones }

// RecordView is the transport projection of a catalogue record. Audit fields
// (created_by and similar) are populated only for admin requests; kind-specific
// fields are omitempty.
type RecordView struct {
	Code          string     `json:"code"`
	Name          string     `json:"name"`
	Enabled       bool       `json:"enabled"`
	Hidden        bool       `json:"hidden"`
	Revision      int64      `json:"revision"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	DeletedAt     *time.Time `json:"deleted_at"`
	CreatedBy     string     `json:"created_by,omitempty"`
	UpdatedBy     string     `json:"updated_by,omitempty"`
	DeletedBy     string     `json:"deleted_by,omitempty"`
	RegionCodes   []string   `json:"region_codes,omitempty"`
	FlagID        string     `json:"flag_id,omitempty"`
	FlagURL       string     `json:"flag_url,omitempty"`
	MinorUnit     *int       `json:"minor_unit,omitempty"`
	CallingCode   string     `json:"calling_code,omitempty"`
	DialPrefixes  []string   `json:"dial_prefixes,omitempty"`
	SVG           string     `json:"svg,omitempty"`
	SVGURL        string     `json:"svg_url,omitempty"`
	OffsetSeconds *int       `json:"offset_seconds,omitempty"`
	LocalTime     string     `json:"local_time,omitempty"`
}

// ListQuery carries a page size and optional opaque continuation cursor for
// list requests.
type ListQuery struct {
	Cursor string
	Limit  int
}

// ListResult returns one page of record views plus the next cursor, empty at
// the end of the listing.
type ListResult struct {
	Records []RecordView
	Cursor  string
}

// Mutation is a partial update body: nil fields mean unchanged. Kind-specific
// fields are validated against the target resource by validMutation.
type Mutation struct {
	Name         *string   `json:"name,omitempty"`
	Enabled      *bool     `json:"enabled,omitempty"`
	Hidden       *bool     `json:"hidden,omitempty"`
	RegionCodes  *[]string `json:"region_codes,omitempty"`
	FlagID       *string   `json:"flag_id,omitempty"`
	MinorUnit    *int      `json:"minor_unit,omitempty"`
	CallingCode  *string   `json:"calling_code,omitempty"`
	DialPrefixes *[]string `json:"dial_prefixes,omitempty"`
	SVG          *string   `json:"svg,omitempty"`
}

// PhoneCheck reports a normalised phone number, its region, validation state
// and the detected channel.
type PhoneCheck struct {
	NormalisedNumber string `json:"normalised_number"`
	RegionCode       string `json:"region_code"`
	State            string `json:"state"`
	Channel          string `json:"channel"`
}

// HTTPService is the manager boundary. Handlers never call storage or providers.
type HTTPService interface {
	// List returns one page of records for the requested Kind, shaped by the admin
	// flag and ListQuery; flags use the public listing for non-admin callers and
	// other kinds attach flag URLs and a next cursor.
	List(context.Context, Kind, bool, ListQuery) (ListResult, error)
	// Get returns the RecordView for a Kind and code, restricting non-admin callers
	// to selectable records without raw SVG and attaching a referenced flag's URL
	// when that flag exists and remains selectable.
	Get(context.Context, Kind, string, bool) (RecordView, error)
	// Create persists a new record of the given Kind and code using the supplied
	// Mutation and actor as creator, applying kind-specific defaults and validation
	// before returning the created view.
	Create(context.Context, Kind, string, Mutation, string) (RecordView, error)
	// Update applies a compare-and-swap partial update for the Kind and code: it
	// merges mutation fields into the current record and submits them with the
	// expected revision and actor.
	Update(context.Context, Kind, string, Mutation, int64, string) (RecordView, error)
	// Remove soft-deletes the record of the given Kind and code, requiring the
	// current revision and actor, and returns the resulting view.
	Remove(context.Context, Kind, string, int64, string) (RecordView, error)
	// Restore un-deletes a soft-deleted record of the given Kind and code,
	// requiring the revision observed while deleted and the actor, returning the
	// restored view.
	Restore(context.Context, Kind, string, int64, string) (RecordView, error)
	// CheckPhone normalises and validates a phone number for a region, returning
	// the normalised number, region code, state and channel; unknown or
	// non-selectable regions map to a field error.
	CheckPhone(context.Context, string, string) (PhoneCheck, error)
}

// Identity is populated exclusively by the host's live authentication boundary.
// Subject is an opaque host security binding, never decoded from a request body.
type Identity struct {
	ActorID string
	Subject any
}

// HTTPSecurity is the host-supplied transport security port: identity
// resolution, CSRF issuance/verification and per-route rate limiting.
type HTTPSecurity interface {
	// Resolve derives the verified Identity for an incoming request, with the
	// boolean flag influencing how strictly identity is required, as part of the
	// host-supplied transport security port.
	Resolve(context.Context, *http.Request, bool) (Identity, error)
	// Issue writes a CSRF credential for the response tied to the resolved
	// Identity, part of the host-supplied transport security port.
	Issue(http.ResponseWriter, *http.Request, Identity) error
	// Verify checks that the request carries a valid CSRF credential matching the
	// resolved Identity, as part of the host-supplied transport security port.
	Verify(*http.Request, Identity) error
	// Limit applies per-route rate limiting for the request and Identity, with the
	// string naming the route, returning an error when admission is refused.
	Limit(context.Context, *http.Request, Identity, string) error
}

// Error is a transport error carrying a stable code, HTTP status and optional
// offending field.
type Error struct {
	Code   string
	Status int
	Field  string
}

// Error returns the machine-readable code as the error text.
func (e *Error) Error() string { return e.Code }

// invalidHTTP returns the generic 400 invalid-request transport error.
func invalidHTTP() error { return &Error{Code: "I18N_INVALID_REQUEST", Status: 400} }

// Handler is the HTTP boundary; it forwards to the composed service behind the
// injected transport security port.
type Handler struct {
	service  HTTPService
	security HTTPSecurity
}

// NewHandler rejects nil service or security wiring and otherwise returns a
// ready Handler.
func NewHandler(service HTTPService, security HTTPSecurity) (*Handler, error) {
	if service == nil || security == nil {
		return nil, errors.New("internationalisationmanager: service and security required")
	}
	return &Handler{service: service, security: security}, nil
}

// ServeHTTP sets no-store, nosniff and no-referrer headers on every response
// and writes any handler error via writeHTTPError.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if err := h.serve(w, r); err != nil {
		writeHTTPError(w, err)
	}
}

// routeParts splits the path under BasePath into decoded segments, rejecting
// escapes, empty segments and NUL, CR, LF or backslash characters as invalid
// requests.
func routeParts(r *http.Request) ([]string, error) {
	path := r.URL.EscapedPath()
	if !strings.HasPrefix(path, BasePath+"/") {
		return nil, &Error{Code: "I18N_NOT_FOUND", Status: 404}
	}
	raw := strings.Split(strings.TrimSuffix(strings.TrimPrefix(path, BasePath+"/"), "/"), "/")
	out := make([]string, len(raw))
	for i, part := range raw {
		value, err := url.PathUnescape(part)
		if err != nil || value == "" || strings.ContainsAny(value, "\x00\r\n\\") {
			return nil, invalidHTTP()
		}
		out[i] = value
	}
	return out, nil
}

// serve dispatches one request: admin routes require a resolved actor identity,
// csrf bootstrap and phone checks are the only unauthenticated mutations-
// adjacent paths, list/get/svg are GET-only, and admin writes require CSRF
// verification plus a quoted If-Match revision (except create). Non-admin reads
// hide SVG bodies; SVG responses carry a revision ETag.
func (h *Handler) serve(w http.ResponseWriter, r *http.Request) error {
	parts, err := routeParts(r)
	if err != nil {
		return err
	}
	admin := parts[0] == "admin"
	if admin {
		parts = parts[1:]
	}
	if len(parts) == 0 {
		return &Error{Code: "I18N_NOT_FOUND", Status: 404}
	}
	bootstrap := !admin && len(parts) == 1 && parts[0] == "csrf"
	checking := !admin && len(parts) == 2 && parts[0] == "phonecodes" && parts[1] == "check"
	identity := Identity{}
	if admin || bootstrap || checking {
		identity, err = h.security.Resolve(r.Context(), r, admin)
		if err != nil {
			return err
		}
		if admin && identity.ActorID == "" {
			return &Error{Code: "I18N_AUTH_REQUIRED", Status: 401}
		}
	}
	if err = h.security.Limit(r.Context(), r, identity, parts[0]); err != nil {
		return err
	}
	if bootstrap {
		if r.Method != http.MethodGet {
			return methodError(w, "GET")
		}
		if len(r.URL.Query()) > 0 {
			return invalidHTTP()
		}
		if err = h.security.Issue(w, r, identity); err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	if checking {
		if r.Method != http.MethodPost {
			return methodError(w, "POST")
		}
		if len(r.URL.Query()) > 0 {
			return invalidHTTP()
		}
		if err = h.security.Verify(r, identity); err != nil {
			return err
		}
		var input struct {
			Phone      string `json:"phone"`
			RegionCode string `json:"region_code"`
		}
		if err = decodeHTTP(w, r, &input, 4096); err != nil {
			return err
		}
		result, err := h.service.CheckPhone(r.Context(), input.Phone, input.RegionCode)
		if err != nil {
			return err
		}
		return writeJSON(w, 200, result, "", 0)
	}
	kind := Kind(parts[0])
	if !kind.valid() {
		return &Error{Code: "I18N_NOT_FOUND", Status: 404}
	}
	if len(parts) == 1 {
		if r.Method != http.MethodGet {
			return methodError(w, "GET")
		}
		query := ListQuery{Limit: 200, Cursor: r.URL.Query().Get("cursor")}
		for key, values := range r.URL.Query() {
			if (key != "limit" && key != "cursor") || len(values) != 1 {
				return invalidHTTP()
			}
		}
		if len(query.Cursor) > 256 {
			return invalidHTTP()
		}
		if raw := r.URL.Query().Get("limit"); raw != "" {
			query.Limit, err = strconv.Atoi(raw)
			if err != nil || query.Limit < 1 || query.Limit > 200 {
				return invalidHTTP()
			}
		}
		result, err := h.service.List(r.Context(), kind, admin, query)
		if err != nil {
			return err
		}
		if result.Records == nil {
			result.Records = []RecordView{}
		}
		return writeJSON(w, 200, result.Records, result.Cursor, 0)
	}
	if len(parts) > 3 {
		return &Error{Code: "I18N_NOT_FOUND", Status: 404}
	}
	code := parts[1]
	if len(parts) == 3 && parts[2] == "svg" && kind == Flags {
		if r.Method != http.MethodGet {
			return methodError(w, "GET")
		}
		for key, values := range r.URL.Query() {
			if key != "v" || len(values) != 1 {
				return invalidHTTP()
			}
		}
		value, err := h.service.Get(r.Context(), Flags, code, admin)
		if err != nil {
			return err
		}
		w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'none'; sandbox")
		tag := fmt.Sprintf(`"flag-%s-%d"`, code, value.Revision)
		w.Header().Set("ETag", tag)
		if !admin {
			w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
		}
		if r.Header.Get("If-None-Match") == tag {
			w.WriteHeader(http.StatusNotModified)
			return nil
		}
		_, err = io.WriteString(w, value.SVG)
		return err
	}
	if len(r.URL.Query()) > 0 {
		return invalidHTTP()
	}
	restore := len(parts) == 3 && parts[2] == "restore"
	if len(parts) == 3 && !restore {
		return &Error{Code: "I18N_NOT_FOUND", Status: 404}
	}
	if r.Method == http.MethodGet && !restore {
		value, err := h.service.Get(r.Context(), kind, code, admin)
		if err != nil {
			return err
		}
		if !admin {
			value.SVG = ""
		}
		return writeJSON(w, 200, value, "", value.Revision)
	}
	if !admin {
		return methodError(w, "GET")
	}
	if r.Method != http.MethodPost && r.Method != http.MethodPatch && r.Method != http.MethodDelete {
		return methodError(w, "GET, POST, PATCH, DELETE")
	}
	if restore && r.Method != http.MethodPost {
		return methodError(w, "POST")
	}
	if err = h.security.Verify(r, identity); err != nil {
		return err
	}
	var revision int64
	if restore || r.Method != http.MethodPost {
		revision, err = revisionHeader(r)
		if err != nil {
			return err
		}
	} else if len(r.Header.Values("If-Match")) > 0 {
		return invalidHTTP()
	}
	var input Mutation
	if err = decodeHTTP(w, r, &input, 300*1024); err != nil {
		return err
	}
	var value RecordView
	status := 200
	switch {
	case restore:
		if input != (Mutation{}) {
			return invalidHTTP()
		}
		value, err = h.service.Restore(r.Context(), kind, code, revision, identity.ActorID)
	case r.Method == http.MethodPost:
		value, err = h.service.Create(r.Context(), kind, code, input, identity.ActorID)
		status = 201
	case r.Method == http.MethodPatch:
		value, err = h.service.Update(r.Context(), kind, code, input, revision, identity.ActorID)
	case r.Method == http.MethodDelete:
		if input != (Mutation{}) {
			return invalidHTTP()
		}
		value, err = h.service.Remove(r.Context(), kind, code, revision, identity.ActorID)
	}
	if err != nil {
		return err
	}
	return writeJSON(w, status, value, "", value.Revision)
}

// revisionHeader parses exactly one quoted If-Match header holding a canonical
// positive decimal revision; absence yields 428 I18N_REVISION_REQUIRED.
func revisionHeader(r *http.Request) (int64, error) {
	values := r.Header.Values("If-Match")
	if len(values) == 0 {
		return 0, &Error{Code: "I18N_REVISION_REQUIRED", Status: 428}
	}
	if len(values) != 1 {
		return 0, invalidHTTP()
	}
	value := values[0]
	if len(value) < 3 || value[0] != '"' || value[len(value)-1] != '"' {
		return 0, invalidHTTP()
	}
	revision, err := strconv.ParseInt(value[1:len(value)-1], 10, 64)
	if err != nil || revision < 1 || strconv.FormatInt(revision, 10) != value[1:len(value)-1] {
		return 0, invalidHTTP()
	}
	return revision, nil
}

// methodError sets the Allow header and returns the 405 transport error.
func methodError(w http.ResponseWriter, allow string) error {
	w.Header().Set("Allow", allow)
	return &Error{Code: "I18N_METHOD_NOT_ALLOWED", Status: 405}
}

// decodeHTTP decodes one JSON object up to limit bytes with unknown fields and
// trailing data rejected; wrong media type yields 415.
func decodeHTTP(w http.ResponseWriter, r *http.Request, target any, limit int64) error {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return &Error{Code: "I18N_JSON_REQUIRED", Status: 415}
	}
	reader := http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		return invalidHTTP()
	}
	if err = decoder.Decode(&struct{}{}); err != io.EOF {
		return invalidHTTP()
	}
	return nil
}

// writeJSON writes a data/meta envelope, sets an ETag when revision is
// positive, and returns any write failure.
func writeJSON(w http.ResponseWriter, status int, data any, cursor string, revision int64) error {
	response := struct {
		Data any `json:"data"`
		Meta struct {
			Next string `json:"next_cursor"`
		} `json:"meta"`
	}{Data: data}
	response.Meta.Next = cursor
	body, err := json.Marshal(response)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if revision > 0 {
		w.Header().Set("ETag", fmt.Sprintf(`"%d"`, revision))
	}
	w.WriteHeader(status)
	_, err = w.Write(body)
	return err
}

// writeHTTPError maps a transport Error to its status and code, defaults
// everything else to a generic 500, and always returns a fixed opaque message
// with no-store.
func writeHTTPError(w http.ResponseWriter, err error) {
	status, code, field := 500, "I18N_UNAVAILABLE", ""
	var typed *Error
	if errors.As(err, &typed) {
		status, code, field = typed.Status, typed.Code, typed.Field
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "field": field, "message": "The internationalisation request could not be completed."}})
}
