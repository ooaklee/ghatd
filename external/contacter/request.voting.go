package contacter

import "github.com/ooaklee/ghatd/external/voter"

// GetCommsVotesRequest selects up to 100 child entries plus the original message.
type GetCommsVotesRequest struct {
	// ActorID is supplied by an authorized manager, never transport decoding.
	ActorID string `json:"-" query:"-" form:"-"`
	// CommsID is the selected resource, not a caller identity.
	CommsID string
	// EntryIDs must all belong to CommsID; duplicates fail the whole request.
	EntryIDs []string
}

// ChangeCommsVoteRequest selects one authorized contact/entry and a vote direction.
type ChangeCommsVoteRequest struct {
	// ActorID is supplied by an authorized manager, never transport decoding.
	ActorID string `json:"-" query:"-" form:"-"`
	// CommsID is the selected parent resource.
	CommsID string
	// EntryID is empty for the original message.
	EntryID string
	// Vote is used only by SetCommsVote; RemoveCommsVote ignores this field.
	Vote voter.Value
}
