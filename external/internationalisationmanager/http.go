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

type Kind string

const (
	Currencies Kind = "currencies"
	PhoneCodes Kind = "phonecodes"
	Flags      Kind = "flags"
	Timezones  Kind = "timezones"
)

func (k Kind) valid() bool { return k == Currencies || k == PhoneCodes || k == Flags || k == Timezones }

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
type ListQuery struct {
	Cursor string
	Limit  int
}
type ListResult struct {
	Records []RecordView
	Cursor  string
}
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
type PhoneCheck struct {
	NormalisedNumber string `json:"normalised_number"`
	RegionCode       string `json:"region_code"`
	State            string `json:"state"`
	Channel          string `json:"channel"`
}

// HTTPService is the manager boundary. Handlers never call storage or providers.
type HTTPService interface {
	List(context.Context, Kind, bool, ListQuery) (ListResult, error)
	Get(context.Context, Kind, string, bool) (RecordView, error)
	Create(context.Context, Kind, string, Mutation, string) (RecordView, error)
	Update(context.Context, Kind, string, Mutation, int64, string) (RecordView, error)
	Remove(context.Context, Kind, string, int64, string) (RecordView, error)
	Restore(context.Context, Kind, string, int64, string) (RecordView, error)
	CheckPhone(context.Context, string, string) (PhoneCheck, error)
}

// Identity is populated exclusively by the host's live authentication boundary.
// Subject is an opaque host security binding, never decoded from a request body.
type Identity struct {
	ActorID string
	Subject any
}
type HTTPSecurity interface {
	Resolve(context.Context, *http.Request, bool) (Identity, error)
	Issue(http.ResponseWriter, *http.Request, Identity) error
	Verify(*http.Request, Identity) error
	Limit(context.Context, *http.Request, Identity, string) error
}
type Error struct {
	Code   string
	Status int
	Field  string
}

func (e *Error) Error() string { return e.Code }
func invalidHTTP() error       { return &Error{Code: "I18N_INVALID_REQUEST", Status: 400} }

type Handler struct {
	service  HTTPService
	security HTTPSecurity
}

func NewHandler(service HTTPService, security HTTPSecurity) (*Handler, error) {
	if service == nil || security == nil {
		return nil, errors.New("internationalisationmanager: service and security required")
	}
	return &Handler{service: service, security: security}, nil
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if err := h.serve(w, r); err != nil {
		writeHTTPError(w, err)
	}
}
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
func methodError(w http.ResponseWriter, allow string) error {
	w.Header().Set("Allow", allow)
	return &Error{Code: "I18N_METHOD_NOT_ALLOWED", Status: 405}
}
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
