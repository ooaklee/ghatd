package usermanager

import "github.com/ooaklee/ghatd/external/contacter"

// CommsParticipant is a bounded private author/viewer label. It deliberately
// excludes email, roles and the wider user profile; names are plain text.
type CommsParticipant struct {
	// ID identifies a known entry author or the verified viewer, never all voters.
	ID string `json:"id"`
	// NanoID is the public short identifier when available.
	NanoID string `json:"nano_id"`
	// FullName is a display name, not trusted markup or an email fallback.
	FullName string `json:"full_name"`
}

// CommsVotePage is the manager-owned private HTTP projection. Lower-domain
// author metadata never crosses this boundary, even when enrichment fails.
type CommsVotePage struct {
	// CommsID selects the contact validated by the lower domain.
	CommsID string `json:"comms_id"`
	// ViewerActorID is the verified caller, not transport-supplied identity.
	ViewerActorID string `json:"viewer_actor_id"`
	// ByEntry contains the requested summaries, with an empty key for the contact.
	ByEntry map[string]contacter.CommsVoteSummary `json:"by_entry"`
	// Participants contains best-effort read labels; mutations return an empty array.
	Participants []CommsParticipant `json:"participants"`
}
