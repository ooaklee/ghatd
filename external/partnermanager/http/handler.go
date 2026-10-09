package partnerhttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ooaklee/ghatd/external/http/browsersecurity"
)

// HTTPService delegates fixed operations to owning managers through safe DTOs.
type HTTPService interface {
	Handle(context.Context, Principal, Request) (Response, error)
}

// PrincipalResolver must verify the current host session and account admission.
// Native audiences and transport binding must be derived from verified claims.
type PrincipalResolver interface {
	Resolve(context.Context, *http.Request) (Principal, error)
}
type PrincipalResolverFunc func(context.Context, *http.Request) (Principal, error)

func (f PrincipalResolverFunc) Resolve(ctx context.Context, r *http.Request) (Principal, error) {
	return f(ctx, r)
}

// Observation contains bounded routing metadata only. No raw paths, resource
// IDs, query values, actors, credentials, payloads or owner diagnostics appear.
type Observation struct {
	Method, Operation, RouteTemplate, Stage, ErrorCode string
	Status                                             int
	RetryAfter                                         time.Duration
	Duration                                           time.Duration
}
type Observer func(context.Context, Observation)

// Config is trusted HTTP composition. Security and resolver must represent the
// same current principal/purpose. The host owns CORS, resources and route mounting.
type Config struct {
	Security     *browsersecurity.Security
	Limiter      browsersecurity.Limiter
	MaxBodyBytes int64
	Now          func() time.Time
	Observe      Observer
}

// Handler is the independent member-only Partners HTTP boundary. It never
// admits a guest/draft identity or grants authority from an administrator role.
type Handler struct {
	service      HTTPService
	resolver     PrincipalResolver
	security     *browsersecurity.Security
	limiter      browsersecurity.Limiter
	maxBodyBytes int64
	now          func() time.Time
	observe      Observer
	routes       []Route
}

// New validates transport dependencies without I/O or route registration.
func New(service HTTPService, resolver PrincipalResolver, config Config) (*Handler, error) {
	if missingPartnersAccessPort(service) || missingPartnersAccessPort(resolver) || config.Security == nil {
		return nil, fail("PARTNERS_CONFIGURATION_INVALID", 500)
	}
	if config.MaxBodyBytes == 0 {
		config.MaxBodyBytes = 64 << 10
	}
	if config.MaxBodyBytes < 1 || config.MaxBodyBytes > 64<<10 {
		return nil, fail("PARTNERS_CONFIGURATION_INVALID", 500)
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.Limiter == nil {
		limiter, err := browsersecurity.NewWindowLimiter(120, time.Minute, config.Now)
		if err != nil {
			return nil, fail("PARTNERS_CONFIGURATION_INVALID", 500)
		}
		config.Limiter = limiter
	}
	if missingPartnersAccessPort(config.Limiter) {
		return nil, fail("PARTNERS_CONFIGURATION_INVALID", 500)
	}
	return &Handler{service: service, resolver: resolver, security: config.Security, limiter: config.Limiter,
		maxBodyBytes: config.MaxBodyBytes, now: config.Now, observe: config.Observe, routes: Routes()}, nil
}

var resourceID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,255}$`)

func match(pattern, candidate string) (string, bool) {
	p, c := strings.Split(pattern, "/"), strings.Split(candidate, "/")
	if len(p) != len(c) || len(candidate) > 2048 {
		return "", false
	}
	id := ""
	for i := range p {
		if p[i] == "{id}" {
			if !resourceID.MatchString(c[i]) {
				return "", false
			}
			id = c[i]
			continue
		}
		if p[i] != c[i] {
			return "", false
		}
	}
	return id, true
}

func (h *Handler) resolveRoute(method, candidate string) (Route, string, []string, bool) {
	allowed := map[string]bool{}
	for _, entry := range h.routes {
		id, ok := match(entry.Path, candidate)
		if !ok {
			continue
		}
		if entry.Method == method {
			return entry, id, nil, true
		}
		allowed[entry.Method] = true
	}
	methods := make([]string, 0, len(allowed))
	for method := range allowed {
		methods = append(methods, method)
	}
	sort.Strings(methods)
	return Route{}, "", methods, false
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-transform")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Add("Vary", "Cookie, Authorization, Origin")
	started := h.now()
	method := "OTHER"
	switch r.Method {
	case http.MethodGet, http.MethodPost, http.MethodPatch:
		method = r.Method
	}
	observation := Observation{Method: method, Stage: "route", Status: 500}
	defer func() {
		if h.observe != nil {
			observation.Duration = h.now().Sub(started)
			h.observe(r.Context(), observation)
		}
	}()
	reject := func(err error) {
		code, status := publicError(err)
		observation.Status, observation.ErrorCode = status, code
		writeError(w, code, status)
	}
	bootstrap := r.URL.Path == BootstrapPath && r.Method == http.MethodGet
	entry, id, allowed, found := h.resolveRoute(r.Method, r.URL.Path)
	if !found && !bootstrap {
		if r.URL.Path == BootstrapPath {
			allowed = []string{http.MethodGet}
		}
		if len(allowed) > 0 {
			w.Header().Set("Allow", strings.Join(allowed, ", "))
			reject(fail("PARTNERS_METHOD_NOT_ALLOWED", 405))
		} else {
			reject(fail("PARTNERS_NOT_FOUND", 404))
		}
		return
	}
	observation.Operation, observation.RouteTemplate = entry.Operation, entry.Path
	if bootstrap {
		observation.Operation, observation.RouteTemplate = "partners.csrf", BootstrapPath
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		reject(fail("PARTNERS_INVALID_REQUEST", 400))
		return
	}
	if bootstrap {
		for key, values := range query {
			if key != "audience" || len(values) != 1 || values[0] != "session" {
				reject(fail("PARTNERS_INVALID_REQUEST", 400))
				return
			}
		}
	}
	observation.Stage = "principal"
	principal, err := h.resolver.Resolve(r.Context(), r)
	if err != nil {
		reject(err)
		return
	}
	if err = member(principal); err != nil {
		reject(err)
		return
	}
	if len(r.Header.Values("Authorization")) > 0 && !h.security.Native(r, principal.Transport) {
		reject(fail("PARTNERS_AUTH_REQUIRED", 401))
		return
	}
	if err = h.security.Issue(w, r, principal.Transport); err != nil {
		reject(fail("PARTNERS_DEPENDENCY_UNAVAILABLE", 503))
		return
	}
	observation.Stage = "rate"
	retry, err := h.limiter.Allow(r.Context(), h.security.RateIdentity(r, principal.Transport))
	if err != nil {
		reject(fail("PARTNERS_DEPENDENCY_UNAVAILABLE", 503))
		return
	}
	if retry > 0 {
		observation.RetryAfter = retry
		w.Header().Set("Retry-After", browsersecurity.RetrySeconds(retry))
		reject(fail("PARTNERS_RATE_LIMITED", 429))
		return
	}
	observation.Stage = "transport"
	if r.Method != http.MethodGet {
		if err = h.security.Verify(r, principal.Transport); err != nil {
			if errors.Is(err, browsersecurity.ErrAuthenticationRequired) {
				reject(fail("PARTNERS_AUTH_REQUIRED", 401))
			} else {
				reject(fail("PARTNERS_FORBIDDEN", 403))
			}
			return
		}
	}
	request := Request{Operation: entry.Operation, ID: id, Query: query}
	if r.Method != http.MethodGet {
		keys := r.Header.Values("Idempotency-Key")
		if len(keys) != 1 || !validKey(keys[0]) {
			reject(fail("PARTNERS_INVALID_REQUEST", 400))
			return
		}
		request.Key = keys[0]
	}
	if tags := r.Header.Values("If-Match"); len(tags) > 1 || len(tags) == 1 && !strongETag(tags[0]) {
		reject(fail("PARTNERS_INVALID_REQUEST", 400))
		return
	}
	body, err := readBody(r, h.maxBodyBytes)
	if err != nil {
		reject(err)
		return
	}
	if r.Method == http.MethodGet && len(body) > 0 {
		reject(fail("PARTNERS_INVALID_REQUEST", 400))
		return
	}
	if bootstrap {
		observation.Stage, observation.Status = "response", 204
		w.WriteHeader(http.StatusNoContent)
		return
	}
	request.Body = body
	if r.Method != http.MethodGet && len(body) == 0 {
		request.Body = json.RawMessage(`{}`)
	}
	observation.Stage = "service"
	response, err := h.service.Handle(r.Context(), principal, request)
	if err != nil {
		reject(err)
		return
	}
	observation.Stage = "response"
	if response.Status < 200 || response.Status >= 300 || !json.Valid(response.Body) || (response.ETag != "" && !strongETag(response.ETag)) {
		reject(fail("PARTNERS_INTERNAL_ERROR", 500))
		return
	}
	if response.ETag != "" {
		w.Header().Set("ETag", response.ETag)
	}
	observation.Status = response.Status
	w.WriteHeader(response.Status)
	_, _ = w.Write(response.Body)
}

func validKey(key string) bool {
	if len(key) < 1 || len(key) > 128 {
		return false
	}
	for _, char := range key {
		if char < 33 || char > 126 {
			return false
		}
	}
	return true
}
func strongETag(tag string) bool {
	if len(tag) < 3 || len(tag) > 512 || tag[0] != '"' || tag[len(tag)-1] != '"' {
		return false
	}
	for _, char := range tag[1 : len(tag)-1] {
		if char == '"' || char < 33 || char > 126 {
			return false
		}
	}
	return true
}
func readBody(r *http.Request, maximum int64) (json.RawMessage, error) {
	if r.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maximum+1))
	if err != nil {
		return nil, fail("PARTNERS_INVALID_REQUEST", 400)
	}
	if int64(len(body)) > maximum {
		return nil, fail("PARTNERS_INVALID_REQUEST", 413)
	}
	if len(body) == 0 {
		return nil, nil
	}
	values := r.Header.Values("Content-Type")
	if len(values) != 1 {
		return nil, fail("PARTNERS_INVALID_REQUEST", 400)
	}
	media, parameters, err := mime.ParseMediaType(values[0])
	if err != nil || media != "application/json" || (parameters["charset"] != "" && !strings.EqualFold(parameters["charset"], "utf-8")) {
		return nil, fail("PARTNERS_INVALID_REQUEST", 400)
	}
	var object map[string]json.RawMessage
	if err = json.Unmarshal(body, &object); err != nil || object == nil {
		return nil, fail("PARTNERS_INVALID_REQUEST", 400)
	}
	return body, nil
}

func publicError(err error) (string, int) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return "PARTNERS_DEPENDENCY_UNAVAILABLE", 503
	}
	allowed := map[string][]int{
		"PARTNERS_INVALID_REQUEST": {400, 413}, "PARTNERS_AUTH_REQUIRED": {401},
		"PARTNERS_VERIFICATION_REQUIRED": {403}, "PARTNERS_ACCOUNT_UNAVAILABLE": {403}, "PARTNERS_FORBIDDEN": {403},
		"PARTNERS_NOT_FOUND": {404}, "PARTNERS_METHOD_NOT_ALLOWED": {405},
		"PARTNERS_CONFLICT": {409}, "PARTNERS_INSUFFICIENT_FUNDS": {409}, "PARTNERS_STALE_WRITE": {412},
		"PARTNERS_RATE_LIMITED": {429}, "PARTNERS_DEPENDENCY_UNAVAILABLE": {503}, "PARTNERS_OUTCOME_UNCERTAIN": {503},
		"PARTNERS_INTERNAL_ERROR": {500}, "PARTNERS_CONFIGURATION_INVALID": {500},
	}
	candidates := []*Error{}
	remaining := 64
	var known func(error, int) bool
	known = func(cause error, depth int) bool {
		remaining--
		if cause == nil || depth > 32 || remaining < 0 {
			return false
		}
		if joined, ok := cause.(interface{ Unwrap() []error }); ok {
			children := joined.Unwrap()
			if len(children) == 0 || len(children) > 32 {
				return false
			}
			for _, child := range children {
				if !known(child, depth+1) {
					return false
				}
			}
			return true
		}
		if child := errors.Unwrap(cause); child != nil {
			return known(child, depth+1)
		}
		e, ok := cause.(*Error)
		if !ok || e == nil {
			return false
		}
		for _, status := range allowed[e.Code] {
			if status == e.Status {
				candidates = append(candidates, e)
				return true
			}
		}
		return false
	}
	if !known(err, 0) {
		return "PARTNERS_INTERNAL_ERROR", 500
	}
	for _, code := range []string{"PARTNERS_OUTCOME_UNCERTAIN", "PARTNERS_DEPENDENCY_UNAVAILABLE", "PARTNERS_INTERNAL_ERROR"} {
		for _, e := range candidates {
			if e.Code == code {
				return e.Code, e.Status
			}
		}
	}
	if len(candidates) > 0 {
		return candidates[0].Code, candidates[0].Status
	}
	return "PARTNERS_INTERNAL_ERROR", 500
}
func writeError(w http.ResponseWriter, code string, status int) {
	body, _ := json.Marshal(map[string]any{"error": map[string]string{"code": code, "message": "The request could not be completed."}})
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
