package usermanager

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"
	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/audit"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/router"
	"github.com/ooaklee/reply/v2"
)

// CommsConversationService is the optional domain/manager conversation port.
// Provider ingestion is deliberately absent from the public HTTP capability.
type CommsConversationService interface {
	// AppendCommsEntry appends an immutable entry to a comms conversation and
	// returns the stored entry. The Service rechecks live administrator authority,
	// includes actor attribution in the entry, and treats the separate audit sink
	// as best effort.
	AppendCommsEntry(context.Context, *contacter.AppendCommsEntryRequest) (*contacter.AppendCommsEntryResponse, error)
	// ListCommsConversation returns a page of entries for a comms conversation. The
	// Service requires live administrator authority even for reads because notes
	// and mail metadata are private and never projected through optional or public
	// access.
	ListCommsConversation(context.Context, *contacter.ListCommsConversationRequest) (*contacter.CommsConversationPage, error)
}

// CommsConversationErrorMaps includes the live verifier's native error contract.
func (s *Service) CommsConversationErrorMaps() []reply.ErrorManifest {
	return s.StatusManagerErrorMaps()
}

// AppendCommsEntry rechecks live administrator authority and delegates an
// immutable append. Actor attribution is part of the stored entry; the separate
// audit sink is best effort and never changes an acknowledged write to failure.
func (s *Service) AppendCommsEntry(ctx context.Context, req *contacter.AppendCommsEntryRequest) (*contacter.AppendCommsEntryResponse, error) {
	if req == nil {
		return nil, contacter.ErrCommsEntryInvalid
	}
	r := *req
	if err := s.administratorActor(ctx, r.ActorID, contacter.ErrCommsConversationUnavailable); err != nil {
		return nil, err
	}
	p, ok := s.ContacterService.(CommsConversationService)
	if !ok || nilProfilePort(p) {
		return nil, contacter.ErrCommsConversationUnavailable
	}
	result, err := p.AppendCommsEntry(ctx, &r)
	if err != nil {
		return nil, err
	}
	if result == nil || result.Entry == nil || result.Entry.CommsID != req.CommsID || result.Entry.ActorID != req.ActorID || result.Entry.Kind != req.Kind || result.Entry.Body != req.Body || result.Entry.ParentEntryID != req.ParentEntryID || result.Entry.ID == "" || result.Entry.RecordedAt.IsZero() || result.Entry.Email != nil {
		return nil, contacter.ErrCommsConversationUnavailable
	}
	if !result.Replayed && !nilProfilePort(s.AuditService) {
		if err := s.AuditService.LogAuditEvent(ctx, &audit.LogAuditEventRequest{ActorId: result.Entry.ActorID, Action: "comms.entry_added", TargetId: result.Entry.CommsID, TargetType: "comms", Details: map[string]interface{}{"entry_id": result.Entry.ID, "kind": result.Entry.Kind}}); err != nil {
			logger.AcquireOperationFrom(ctx, "external/usermanager", "append-comms-entry").Warn("comms-entry-audit-delivery-failed")
		}
	}
	return result, nil
}

// ListCommsConversation requires live admin authority even for reads: notes and
// mail metadata are private and never projected through optional/public access.
func (s *Service) ListCommsConversation(ctx context.Context, req *contacter.ListCommsConversationRequest) (*contacter.CommsConversationPage, error) {
	if req == nil {
		return nil, contacter.ErrCommsEntryInvalid
	}
	r := *req
	if err := s.administratorActor(ctx, r.ActorID, contacter.ErrCommsConversationUnavailable); err != nil {
		return nil, err
	}
	p, ok := s.ContacterService.(CommsConversationService)
	if !ok || nilProfilePort(p) {
		return nil, contacter.ErrCommsConversationUnavailable
	}
	result, err := p.ListCommsConversation(ctx, &r)
	if err != nil {
		return nil, err
	}
	if result == nil || result.Legacy == nil || result.Legacy.Id != req.CommsID || len(result.Entries) > 100 {
		return nil, contacter.ErrCommsConversationUnavailable
	}
	for _, v := range result.Entries {
		if v.CommsID != req.CommsID {
			return nil, contacter.ErrCommsConversationUnavailable
		}
	}
	return result, nil
}

// conversationManifests keeps verifier errors native and host overrides last.
func (h *Handler) conversationManifests() []reply.ErrorManifest {
	c := errormanifest.NewComposer().Add(UsermanagerErrorMap).Add(DependencyErrorMaps()...)
	if h != nil {
		if p, ok := h.Service.(interface{ CommsConversationErrorMaps() []reply.ErrorManifest }); ok && !nilProfilePort(p) {
			c.Add(p.CommsConversationErrorMaps()...)
		}
		c.AddOverrides(h.ErrorMaps...)
	}
	return c.Build()
}

// conversationHTTPPort binds only a verified actor, never a body/query identity.
// Live authorization is rechecked by the manager after route middleware.
func (h *Handler) conversationHTTPPort(r *http.Request) (CommsConversationService, string, error) {
	if r == nil || r.URL == nil {
		return nil, "", contacter.ErrCommsEntryInvalid
	}
	if err := r.Context().Err(); err != nil {
		return nil, "", err
	}
	actor := helpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if actor == "" {
		return nil, "", ErrUnableToIdentifyUser
	}
	if h == nil {
		return nil, "", contacter.ErrCommsConversationUnavailable
	}
	p, ok := h.Service.(CommsConversationService)
	if !ok || nilProfilePort(p) {
		return nil, "", contacter.ErrCommsConversationUnavailable
	}
	return p, actor, nil
}

// AppendCommsConversationEntry accepts plain-text notes/replies. A successful
// response only records history and never represents sending/delivering email.
func (h *Handler) AppendCommsConversationEntry(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	p, actor, err := h.conversationHTTPPort(r)
	var req *contacter.AppendCommsEntryRequest
	if err == nil {
		if r.Body == nil {
			err = contacter.ErrCommsEntryInvalid
		} else {
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512*1024))
			decoder.DisallowUnknownFields()
			var extra any
			if decoder.Decode(&req) != nil || req == nil || decoder.Decode(&extra) != io.EOF {
				err = contacter.ErrCommsEntryInvalid
			}
		}
	}
	var result *contacter.AppendCommsEntryResponse
	if err == nil {
		req.ActorID = actor
		req.CommsID = mux.Vars(r)["id"]
		if req.Kind != contacter.CommsEntryInternalNote && req.Kind != contacter.CommsEntryReply {
			err = contacter.ErrCommsEntryInvalid
		} else {
			result, err = p.AppendCommsEntry(r.Context(), req)
		}
	}
	h.writeConversationResponse(w, r, result, err)
}

// GetCommsConversation returns a bounded history page through the live manager.
func (h *Handler) GetCommsConversation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	p, actor, err := h.conversationHTTPPort(r)
	var result *contacter.CommsConversationPage
	if err == nil {
		req := &contacter.ListCommsConversationRequest{ActorID: actor, CommsID: mux.Vars(r)["id"], Cursor: r.URL.Query().Get("cursor")}
		if limit := r.URL.Query().Get("limit"); limit != "" {
			req.Limit, err = strconv.Atoi(limit)
			if err != nil || req.Limit < 1 || req.Limit > 100 {
				err = contacter.ErrCommsEntryInvalid
			}
		}
		if err == nil {
			result, err = p.ListCommsConversation(r.Context(), req)
		}
	}
	h.writeConversationResponse(w, r, result, err)
}

// writeConversationResponse uses shared manifests without exposing raw causes.
// POST returns 200 for both a new entry and exact replay; Replayed distinguishes.
func (h *Handler) writeConversationResponse(w http.ResponseWriter, r *http.Request, result any, err error) {
	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
	}
	maps := h.conversationManifests()
	if err == nil && nilProfilePort(result) {
		err = contacter.ErrCommsConversationUnavailable
	}
	if err != nil {
		_ = errormanifest.WriteHTTPError(w, err, maps, reply.WithContext(ctx))
		return
	}
	_ = reply.NewReplier(maps).NewHTTPDataResponse(w, http.StatusOK, result, reply.WithContext(ctx))
}

// attachCommsConversationRoutes adds private routes without extending the legacy
// handler interface. Custom older handlers fail closed instead of exposing data.
func attachCommsConversationRoutes(group *router.RouteGroup, handler UsermanagerHandler) {
	p, ok := handler.(interface {
		GetCommsConversation(http.ResponseWriter, *http.Request)
		AppendCommsConversationEntry(http.ResponseWriter, *http.Request)
	})
	get, post := http.HandlerFunc(unavailableCommsConversation), http.HandlerFunc(unavailableCommsConversation)
	if ok && !nilProfilePort(p) {
		get, post = p.GetCommsConversation, p.AppendCommsConversationEntry
	}
	group.Handle(router.RouteDefinition{Path: "/comms/{id}/conversation", Operation: "usermanager.GetCommsConversation", Methods: []string{http.MethodGet, http.MethodOptions}}, get)
	group.Handle(router.RouteDefinition{Path: "/comms/{id}/conversation", Operation: "usermanager.AppendCommsConversationEntry", Methods: []string{http.MethodPost, http.MethodOptions}}, post)
}

// unavailableCommsConversation is the protected missing-capability backstop.
func unavailableCommsConversation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	_ = errormanifest.WriteHTTPError(w, contacter.ErrCommsConversationUnavailable, []reply.ErrorManifest{contacter.ContacterErrorMap}, reply.WithContext(r.Context()))
}
