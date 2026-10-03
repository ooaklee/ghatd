// Package accesspolicy owns system-scoped grants and atomic usage accounting.
// It does not authenticate credentials, invent user roles, or trust JWT quotas.
package accesspolicy

import (
	"context"
	"time"
)

const (
	// UserSubject identifies a stored user's grants in one system.
	UserSubject = "user"
	// APITokenSubject identifies grants for one independently verified API token.
	APITokenSubject = "api_token"
)

// Subject scopes authority to a system and a verified identity, never an email.
type Subject struct {
	// System is the server-configured resource system, not a request header.
	System string `json:"system" bson:"system"`
	// Kind separates user grants from credential-specific delegated grants.
	Kind string `json:"kind" bson:"kind"`
	// ID is the verified user ID or persistent API-token ID according to Kind.
	ID string `json:"id" bson:"id"`
}

// TokenLimits controls API-token inventory and expiry without role rankings.
// Zero disables the relevant capability; no value implicitly means unlimited.
type TokenLimits struct {
	// Permanent is the maximum number of stored non-expiring credentials.
	Permanent int64 `json:"permanent" bson:"permanent"`
	// Ephemeral is the maximum number of stored expiring credentials.
	Ephemeral int64 `json:"ephemeral" bson:"ephemeral"`
	// MinimumTTL is the minimum positive lifetime, measured in seconds.
	MinimumTTL int64 `json:"minimum_ttl" bson:"minimum_ttl"`
	// MaximumTTL is the maximum lifetime; it must fit a Go time.Duration.
	MaximumTTL int64 `json:"maximum_ttl" bson:"maximum_ttl"`
	// TTLIncrement is the nonzero absolute TTL step when ephemeral tokens are enabled.
	TTLIncrement int64 `json:"ttl_increment" bson:"ttl_increment"`
}

// Limit specifies a fixed UTC window's maximum admitted operations.
type Limit struct {
	// Maximum bounds successful admissions, not downstream business success.
	Maximum int64 `json:"maximum" bson:"maximum"`
	// WindowSeconds anchors fixed windows to Unix epoch, with no per-request drift.
	WindowSeconds int64 `json:"window_seconds" bson:"window_seconds"`
}

// Grant is a versioned current policy, not a bearer or a signing-key document.
type Grant struct {
	// Subject is the exact system/identity tuple owning this policy.
	Subject Subject `json:"subject" bson:"subject"`
	// Revision increments on administrative replacement, never on usage updates.
	Revision int64 `json:"revision" bson:"revision"`
	// Enabled must be true to authorize or consume, including on idempotent replay.
	Enabled bool `json:"enabled" bson:"enabled"`
	// ExpiresAt optionally limits the grant; zero means no policy expiry.
	ExpiresAt time.Time `json:"expires_at,omitempty" bson:"expires_at,omitempty"`
	// Scopes are exact credential grants. They are never merged with account roles.
	Scopes []string `json:"scopes" bson:"scopes"`
	// Permissions are exact current system permissions for this subject.
	Permissions []string `json:"permissions" bson:"permissions"`
	// Tokens holds explicit creation limits, usually on a user subject.
	Tokens TokenLimits `json:"tokens" bson:"tokens"`
	// Limits maps bounded metric identifiers to their current quota definitions.
	Limits map[string]Limit `json:"limits" bson:"limits"`
}

// Usage reports a committed admission and its original window on replay.
type Usage struct {
	// Count is the window count after this admission, not a live replay-time total.
	Count int64 `json:"count" bson:"count"`
	// Maximum is the enforced quota at admission time.
	Maximum int64 `json:"maximum" bson:"maximum"`
	// ResetsAt is the fixed window's exclusive end.
	ResetsAt time.Time `json:"resets_at" bson:"resets_at"`
	// Replayed reports a prior admission with the same key and request fingerprint.
	Replayed bool `json:"replayed" bson:"-"`
}

// Consumption binds one admission to a trusted operation and its requirements.
// Stores must check these requirements against the same grant revision fenced
// by the counter/receipt transaction, including on replay.
type Consumption struct {
	// Subject is the verified account or credential within a configured system.
	Subject Subject
	// Metric names a configured usage budget, not a client-selected quota.
	Metric string
	// Key is a bounded operation identity scoped by subject and metric.
	Key string
	// Fingerprint detects reuse of Key for a different normalized operation.
	Fingerprint string
	// Scopes requires every listed credential grant at admission time.
	Scopes []string
	// Permissions requires every listed current permission at admission time.
	Permissions []string
}

// ConsumptionAction separates current resource authorization from new business
// effects. Both callbacks run inside the policy store's transaction and may retry;
// neither may perform external effects or end the transaction. Database calls
// must use the supplied context and the store's managed client.
type ConsumptionAction struct {
	// Check revalidates current resource authority on both first use and replay.
	// It should acquire the domain's required concurrency fence when ownership can
	// change, rather than relying solely on an old snapshot or an earlier route check.
	Check func(context.Context) error
	// Apply persists the business mutation/receipt only for a newly charged operation.
	// It is skipped on committed replay; automatic retries may rerun aborted attempts.
	Apply func(context.Context, Usage) error
}
