package usermanager

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	helpers "github.com/ooaklee/ghatd/external/accessmanager/helpers"
	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/errormanifest"
	"github.com/ooaklee/ghatd/external/voter"
	"github.com/ooaklee/reply/v2"
)

// CommsVoteManager is the optional manager capability consumed by HTTP handlers.
// Custom legacy managers remain source-compatible and unavailable for voting.
type CommsVoteManager interface {
	// GetCommsVotes authorizes the viewer before exposing private vote summaries.
	GetCommsVotes(context.Context, *contacter.GetCommsVotesRequest) (*CommsVotePage, error)
	// SetCommsVote authorizes and delegates one actor-bound vote assignment.
	SetCommsVote(context.Context, *contacter.ChangeCommsVoteRequest) (*CommsVotePage, error)
	// RemoveCommsVote authorizes and delegates removal of the actor's own vote.
	RemoveCommsVote(context.Context, *contacter.ChangeCommsVoteRequest) (*CommsVotePage, error)
}

// commsVoteHTTPPort binds only prior verified identity; manager admission rechecks
// the live session and account before invoking the lower-domain service.
func (h *Handler) commsVoteHTTPPort(r *http.Request) (CommsVoteManager, string, error) {
	if r == nil || r.URL == nil {
		return nil, "", contacter.ErrCommsVoteInvalid
	}
	if err := r.Context().Err(); err != nil {
		return nil, "", err
	}
	actor := helpers.AcquireAuthenticatedUserIDFrom(r.Context())
	if actor == "" {
		return nil, "", ErrUnableToIdentifyUser
	}
	if h == nil {
		return nil, "", contacter.ErrCommsVoteUnavailable
	}
	p, ok := h.Service.(CommsVoteManager)
	if !ok || nilProfilePort(p) {
		return nil, "", contacter.ErrCommsVoteUnavailable
	}
	return p, actor, nil
}

// GetCommsVotes maps private target selectors into the User Manager operation.
func (h *Handler) GetCommsVotes(w http.ResponseWriter, r *http.Request) {
	p, actor, err := h.commsVoteHTTPPort(r)
	var result *CommsVotePage
	if err == nil {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		result, err = p.GetCommsVotes(ctx, &contacter.GetCommsVotesRequest{ActorID: actor, CommsID: mux.Vars(r)["id"], EntryIDs: r.URL.Query()["entry_id"]})
	}
	h.writeCommsVoteResponse(w, r, result, err)
}

// SetCommsVote strictly decodes a direction; body/query actor IDs are not trusted.
func (h *Handler) SetCommsVote(w http.ResponseWriter, r *http.Request) {
	h.changeCommsVote(w, r, false)
}

// RemoveCommsVote maps only route-selected targets and verified actor identity.
func (h *Handler) RemoveCommsVote(w http.ResponseWriter, r *http.Request) {
	h.changeCommsVote(w, r, true)
}

// changeCommsVote handles transport only. The owning route applies RequireOwner
// before body decoding, and User Manager—not the handler—authorizes the command.
func (h *Handler) changeCommsVote(w http.ResponseWriter, r *http.Request, remove bool) {
	p, actor, err := h.commsVoteHTTPPort(r)
	var result *CommsVotePage
	if err == nil {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		req := &contacter.ChangeCommsVoteRequest{ActorID: actor, CommsID: mux.Vars(r)["id"], EntryID: mux.Vars(r)["entryId"]}
		if remove {
			result, err = p.RemoveCommsVote(ctx, req)
		} else {
			var input struct {
				Vote *voter.Value `json:"vote"`
			}
			if r.Body == nil {
				err = contacter.ErrCommsVoteInvalid
			} else {
				decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
				decoder.DisallowUnknownFields()
				var extra any
				if decoder.Decode(&input) != nil || decoder.Decode(&extra) != io.EOF || input.Vote == nil {
					err = contacter.ErrCommsVoteInvalid
				} else {
					req.Vote = *input.Vote
					result, err = p.SetCommsVote(ctx, req)
				}
			}
		}
	}
	h.writeCommsVoteResponse(w, r, result, err)
}

// commsVoteManifests preserves verifier errors and gives host overrides final
// precedence, without leaking the communications wire aliases into Vision APIs.
func (h *Handler) commsVoteManifests() []reply.ErrorManifest {
	c := errormanifest.NewComposer().Add(UsermanagerErrorMap).Add(DependencyErrorMaps()...)
	if h != nil {
		if p, ok := h.Service.(interface{ CommsConversationErrorMaps() []reply.ErrorManifest }); ok && !nilProfilePort(p) {
			c.Add(p.CommsConversationErrorMaps()...)
		}
	}
	c.Add(commsVoteErrorMap())
	if h != nil {
		c.AddOverrides(h.ErrorMaps...)
	}
	return c.Build()
}

// writeCommsVoteResponse keeps the existing envelope and private/no-store headers.
func (h *Handler) writeCommsVoteResponse(w http.ResponseWriter, r *http.Request, result *CommsVotePage, err error) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	ctx := context.Background()
	if r != nil {
		ctx = r.Context()
	}
	if err == nil && result == nil {
		err = contacter.ErrCommsVoteUnavailable
	}
	maps := h.commsVoteManifests()
	if err != nil {
		_ = errormanifest.WriteHTTPError(w, err, maps, reply.WithContext(ctx))
		return
	}
	_ = reply.NewReplier(maps).NewHTTPDataResponse(w, http.StatusOK, result, reply.WithContext(ctx))
}
