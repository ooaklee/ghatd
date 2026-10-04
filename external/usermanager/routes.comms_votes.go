package usermanager

import (
	"net/http"

	"github.com/ooaklee/ghatd/external/contacter"
	"github.com/ooaklee/ghatd/external/router"
)

// attachCommsVoteRoutes owns the private voting HTTP surface under UMS. Existing
// commsconversation.* operation keys are retained for policy compatibility only;
// they no longer imply route ownership by the lower package. Older custom
// handlers receive a protected unavailable response rather than a bypass.
func attachCommsVoteRoutes(group *router.RouteGroup, handler UsermanagerHandler) {
	p, ok := handler.(interface {
		GetCommsVotes(http.ResponseWriter, *http.Request)
		SetCommsVote(http.ResponseWriter, *http.Request)
		RemoveCommsVote(http.ResponseWriter, *http.Request)
	})
	get, set, remove := http.HandlerFunc(unavailableCommsVotes), http.HandlerFunc(unavailableCommsVotes), http.HandlerFunc(unavailableCommsVotes)
	if ok && !nilProfilePort(p) {
		get, set, remove = p.GetCommsVotes, p.SetCommsVote, p.RemoveCommsVote
	}
	group.Handle(router.RouteDefinition{Path: "/comms/{id}/conversation/votes", Operation: "commsconversation.ReadVotes", Methods: []string{http.MethodGet, http.MethodOptions}}, get)
	for _, path := range []string{"/comms/{id}/vote", "/comms/{id}/conversation/{entryId}/vote"} {
		group.Handle(router.RouteDefinition{Path: path, Operation: "commsconversation.SetVote", Methods: []string{http.MethodPost, http.MethodOptions}}, RequireCommsOwner(set).ServeHTTP)
		group.Handle(router.RouteDefinition{Path: path, Operation: "commsconversation.RemoveVote", Methods: []string{http.MethodDelete, http.MethodOptions}}, RequireCommsOwner(remove).ServeHTTP)
	}
}

// unavailableCommsVotes is the protected optional-capability backstop.
func unavailableCommsVotes(w http.ResponseWriter, r *http.Request) {
	(*Handler)(nil).writeCommsVoteResponse(w, r, nil, contacter.ErrCommsVoteUnavailable)
}
