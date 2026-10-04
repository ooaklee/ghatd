package contacter

// CommsVoteSummary preserves the private communications HTTP projection.
type CommsVoteSummary struct {
	// CommsID identifies the selected contact.
	CommsID string `json:"comms_id"`
	// EntryID is empty for the original message, otherwise an immutable entry ID.
	EntryID string `json:"entry_id"`
	// Up counts positive votes.
	Up int `json:"up"`
	// Down counts negative votes.
	Down int `json:"down"`
	// ViewerVote is nil when absent; zero is a real downvote.
	ViewerVote *int `json:"viewer_vote"`
}

// CommsVoteResult contains only authorized targets and the verified viewer's own vote.
type CommsVoteResult struct {
	// CommsID selects the parent contact.
	CommsID string `json:"comms_id"`
	// ViewerActorID comes from live authority, never transport input.
	ViewerActorID string `json:"viewer_actor_id"`
	// ByEntry includes the original message and requested child entries.
	ByEntry map[string]CommsVoteSummary `json:"by_entry"`
	// EntryAuthors maps exactly the admitted read entries to persisted authors.
	// It is internal composition metadata, not authentication or a voter list.
	// Reads include every requested entry, including empty authors; mutations
	// leave it empty. User Manager validates its shape before optional enrichment.
	EntryAuthors map[string]string `json:"-" query:"-" form:"-"`
}
