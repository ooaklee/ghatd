package voter

// Value is a vote, not a score. Zero is a real downvote; absence uses nil.
type Value int

const (
	// Down records a negative vote.
	Down Value = 0
	// Up records a positive vote.
	Up Value = 1
	// MaxTargets bounds a single summary read. Consumers may chunk larger pages.
	MaxTargets = 200
)

// Target separates domains and child resources sharing the same identifiers.
// It is supplied by the owning service, never trusted as authorization. Scope
// is optional for domains sharing a database across tenants; it is not branding.
type Target struct {
	// Scope is a server-selected tenant boundary, empty for database isolation.
	Scope string `json:"scope" bson:"scope"`
	// Domain is a stable package-owned discriminator, such as "vision".
	Domain string `json:"domain" bson:"domain"`
	// ResourceID is the resolved internal identity of the parent resource.
	ResourceID string `json:"resource_id" bson:"resource_id"`
	// ChildID is empty for a parent vote, otherwise the comment or entry identity.
	ChildID string `json:"child_id" bson:"child_id"`
}

// Summary contains counts and only the requested viewer's vote, never voter IDs.
type Summary struct {
	// Up counts positive votes for this target.
	Up int `json:"up"`
	// Down counts negative votes for this target.
	Down int `json:"down"`
	// ViewerVote is absent for anonymous readers or a viewer who has not voted.
	ViewerVote *Value `json:"viewer_vote"`
}

// Valid reports whether the value is one of the two supported vote directions.
func (v Value) Valid() bool { return v == Down || v == Up }
