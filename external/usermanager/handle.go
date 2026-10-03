package usermanager

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	accesshelpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/auth"
	"github.com/ooaklee/ghatd/external/router"
	user "github.com/ooaklee/ghatd/external/user/v2"
	"github.com/ooaklee/reply/v2"
)

// UserHandleService is an optional lower-domain capability. User Manager only
// binds a trusted actor and delegates; it owns no handle collection or policy.
type UserHandleService interface {
	// GetUserHandle reads the live account projection without assigning a name.
	GetUserHandle(context.Context, string) (*user.UserHandle, error)
	// ValidateUserHandle checks syntax and advisory availability for the actor.
	ValidateUserHandle(context.Context, *user.ValidateUserHandleRequest) (*user.ValidateUserHandleResponse, error)
	// UpdateUserHandle delegates exact-candidate uniqueness and revision checks.
	UpdateUserHandle(context.Context, *user.UpdateUserHandleRequest) (*user.UserHandle, error)
}

// MyHandleRequest identifies the self-service actor, never a client-chosen target.
type MyHandleRequest struct {
	// ActorID must come from the already verified active session.
	ActorID string `json:"-"`
	// Handle is the only editable payload for validate/update operations.
	Handle string
	// ExpectedRevision is read from the mandatory If-Match on updates.
	ExpectedRevision int64
}

// userHandleService validates the trusted command before exposing lower methods.
func (s *Service) userHandleService(ctx context.Context, req *MyHandleRequest) (UserHandleService, error) {
	if req == nil || req.ActorID == "" {
		return nil, ErrUnableToIdentifyUser
	}
	if ctx == nil || s == nil || s.UserService == nil {
		return nil, user.ErrHandleUnsupported
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	service, ok := s.UserService.(UserHandleService)
	if !ok {
		return nil, user.ErrHandleUnsupported
	}
	return service, nil
}

// GetMyHandle delegates live state and projection checks to the user domain.
func (s *Service) GetMyHandle(ctx context.Context, req *MyHandleRequest) (*user.UserHandle, error) {
	service, err := s.userHandleService(ctx, req)
	if err != nil {
		return nil, err
	}
	return service.GetUserHandle(ctx, req.ActorID)
}

// ValidateMyHandle checks an advisory candidate without reserving it.
func (s *Service) ValidateMyHandle(ctx context.Context, req *MyHandleRequest) (*user.ValidateUserHandleResponse, error) {
	service, err := s.userHandleService(ctx, req)
	if err != nil {
		return nil, err
	}
	return service.ValidateUserHandle(ctx, &user.ValidateUserHandleRequest{UserID: req.ActorID, Handle: req.Handle})
}

// UpdateMyHandle passes the actor as the immutable target for revision-safe writes.
func (s *Service) UpdateMyHandle(ctx context.Context, req *MyHandleRequest) (*user.UserHandle, error) {
	service, err := s.userHandleService(ctx, req)
	if err != nil {
		return nil, err
	}
	return service.UpdateUserHandle(ctx, &user.UpdateUserHandleRequest{UserID: req.ActorID, Handle: req.Handle, ExpectedRevision: req.ExpectedRevision})
}

// handleManager is optional to preserve the existing handler service interface.
type handleManager interface {
	GetMyHandle(context.Context, *MyHandleRequest) (*user.UserHandle, error)
	ValidateMyHandle(context.Context, *MyHandleRequest) (*user.ValidateUserHandleResponse, error)
	UpdateMyHandle(context.Context, *MyHandleRequest) (*user.UserHandle, error)
}

// handleActor reuses verified claims and rejects API, mixed, anonymous, proof,
// inactive and mismatched account contexts. Authentication middleware must first
// verify credentials and publish live account/session state; this is not a verifier.
func handleActor(r *http.Request) (string, error) {
	if r == nil {
		return "", router.ErrRouteUnauthenticated
	}
	ctx := r.Context()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	session := accesshelpers.AcquireSessionFrom(ctx)
	account := accesshelpers.AcquireUserFrom(ctx)
	if session == nil || account == nil || account.ID != session.UserID || account.Status != "ACTIVE" || !auth.MatchesUserType(session.UserType, account) {
		return "", router.ErrRouteUnauthenticated
	}
	return session.UserID, nil
}

// readHandleBody accepts exactly one non-null, case-sensitive handle field.
// Unknown/duplicate fields, trailing data and encoded or oversized bodies fail.
func readHandleBody(r *http.Request) (string, error) {
	if r == nil || r.Body == nil || len(r.Header.Values("Content-Type")) != 1 || len(r.Header.Values("Content-Encoding")) != 0 {
		return "", ErrInvalidUserBody
	}
	media, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || (params["charset"] != "" && !strings.EqualFold(params["charset"], "utf-8")) {
		return "", ErrInvalidUserBody
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1025))
	if err != nil || len(body) > 1024 || !utf8.Valid(body) {
		return "", ErrInvalidUserBody
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return "", ErrInvalidUserBody
	}
	token, err = decoder.Token()
	if err != nil || token != "handle" {
		return "", ErrInvalidUserBody
	}
	var handle *string
	if err := decoder.Decode(&handle); err != nil || handle == nil {
		return "", ErrInvalidUserBody
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim('}') {
		return "", ErrInvalidUserBody
	}
	if _, err := decoder.Token(); err != io.EOF {
		return "", ErrInvalidUserBody
	}
	return *handle, nil
}

// handleRevision adds numeric semantics to the shared strict ETag grammar.
func handleRevision(r *http.Request) (int64, error) {
	tag, err := (router.StrongETagPolicy{MaxBytes: 18, ASCIIOnly: true}).IfMatch(r, true)
	if err != nil {
		return 0, err
	}
	value := tag[1 : len(tag)-1]
	revision, err := strconv.ParseInt(value, 10, 64)
	if err != nil || revision < 0 || revision >= user.MaxHandleRevision || strconv.FormatInt(revision, 10) != value {
		return 0, router.ErrRouteInvalidRevision
	}
	return revision, nil
}

// GetMyHandle returns current self metadata and an ETag, including "0" if unset.
func (h *Handler) GetMyHandle(w http.ResponseWriter, r *http.Request) { h.serveHandle(w, r, "get") }

// ValidateMyHandle returns availability/suggestion without mutation or reservation.
func (h *Handler) ValidateMyHandle(w http.ResponseWriter, r *http.Request) {
	h.serveHandle(w, r, "validate")
}

// UpdateMyHandle applies the exact candidate under a mandatory numeric If-Match.
func (h *Handler) UpdateMyHandle(w http.ResponseWriter, r *http.Request) {
	h.serveHandle(w, r, "update")
}

// serveHandle keeps the transport contract on the shared reply/error manifest.
// Private diagnostics and supplied handle strings are never logged here.
func (h *Handler) serveHandle(w http.ResponseWriter, r *http.Request, operation string) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	actor, err := handleActor(r)
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err)
		return
	}
	service, ok := h.Service.(handleManager)
	if !ok {
		_ = h.NewHTTPErrorResponse(w, user.ErrHandleUnsupported)
		return
	}
	request := &MyHandleRequest{ActorID: actor}
	if operation != "get" {
		request.Handle, err = readHandleBody(r)
	}
	if err == nil && operation == "update" {
		request.ExpectedRevision, err = handleRevision(r)
	}
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err, reply.WithContext(r.Context()))
		return
	}
	if operation == "validate" {
		response, err := service.ValidateMyHandle(r.Context(), request)
		if err != nil {
			_ = h.NewHTTPErrorResponse(w, err, reply.WithContext(r.Context()))
			return
		}
		if response == nil {
			_ = h.NewHTTPErrorResponse(w, user.ErrDatabaseError)
			return
		}
		h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response, reply.WithContext(r.Context()))
		return
	}
	var response *user.UserHandle
	if operation == "update" {
		response, err = service.UpdateMyHandle(r.Context(), request)
	} else {
		response, err = service.GetMyHandle(r.Context(), request)
	}
	if err != nil {
		_ = h.NewHTTPErrorResponse(w, err, reply.WithContext(r.Context()))
		return
	}
	if response == nil {
		_ = h.NewHTTPErrorResponse(w, user.ErrDatabaseError)
		return
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(response.Metadata.Revision, 10)))
	h.GetBaseResponseHandler().NewHTTPDataResponse(w, http.StatusOK, response, reply.WithContext(r.Context()))
}

// handleHTTP is an optional route capability, distinct from legacy manager APIs.
type handleHTTP interface {
	GetMyHandle(http.ResponseWriter, *http.Request)
	ValidateMyHandle(http.ResponseWriter, *http.Request)
	UpdateMyHandle(http.ResponseWriter, *http.Request)
}

// attachHandleRoutes adds only explicitly enabled routes. Missing handlers or
// session middleware leave registry validation invalid instead of silently omitting
// promised endpoints. PATCH also declares revision admission to the shared guard.
func attachHandleRoutes(request *AttachRoutesRequest) {
	group := request.Router.NewRouteGroup(APIUserManagerV1Prefix, router.ActiveSession, request.ActiveOnlyMiddleware)
	var get, validate, update http.HandlerFunc
	if handler, ok := request.Handler.(handleHTTP); ok {
		get, validate, update = handler.GetMyHandle, handler.ValidateMyHandle, handler.UpdateMyHandle
	}
	group.Handle(router.RouteDefinition{Path: "/me/handle", Operation: "usermanager.GetMyHandle", Methods: []string{http.MethodGet}}, get)
	group.Handle(router.RouteDefinition{Path: "/me/handle/validate", Operation: "usermanager.ValidateMyHandle", Methods: []string{http.MethodPost}}, validate)
	group.Handle(router.RouteDefinition{Path: "/me/handle", Operation: "usermanager.UpdateMyHandle", Methods: []string{http.MethodPatch}, Policy: router.RoutePolicy{RevisionRequired: true}}, update)
}
