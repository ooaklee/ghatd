package voter

// SetVoteRequest sets or replaces the caller's one vote on an authorized target.
type SetVoteRequest struct {
	// ActorID is bound by the integrating service after authorization.
	ActorID string `json:"-"`
	// Target is selected and authorized by the owning domain.
	Target Target
	// Vote is Up or Down; domain policy may prohibit Down before this call.
	Vote Value
}

// RemoveVoteRequest removes only the caller's vote; absence is successful.
type RemoveVoteRequest struct {
	// ActorID is the verified caller, never the target resource's owner.
	ActorID string `json:"-"`
	// Target must already have passed the owning domain's access checks.
	Target Target
}

// GetSummariesRequest reads a bounded set of targets already authorized by a
// consumer. Empty ActorID explicitly requests counts without a viewer vote.
type GetSummariesRequest struct {
	// ActorID optionally identifies the authorized viewer, bound server-side.
	ActorID string `json:"-"`
	// Targets contains unique structured identities, at most MaxTargets.
	Targets []Target
}
