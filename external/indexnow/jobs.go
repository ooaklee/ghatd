package indexnow

import (
	"context"
	"errors"
	"regexp"
	"time"
)

// JobCollection stores only hashed scope identities, lease tokens and progress
// timestamps. It must not contain URLs, API keys or submission payloads.
const JobCollection = "indexnow_jobs"

var (
	// ErrJobInvalid rejects invalid scheduling or persistence input before I/O.
	ErrJobInvalid = errors.New("indexnow: invalid reconciliation job")
	// ErrJobUnavailable reports an uncertain or unavailable durable operation.
	ErrJobUnavailable = errors.New("indexnow: reconciliation storage unavailable")
	// ErrJobBusy means another unexpired lease owns the scope.
	ErrJobBusy = errors.New("indexnow: reconciliation lease busy")
	// ErrJobLeaseLost means the token or unexpired lease no longer owns progress.
	ErrJobLeaseLost = errors.New("indexnow: reconciliation lease lost")
	// ErrJobIncomplete leaves the checkpoint unchanged after a partial scan.
	ErrJobIncomplete = errors.New("indexnow: reconciliation incomplete")
	// ErrJobScanFailed deliberately excludes callback diagnostics from reports.
	ErrJobScanFailed = errors.New("indexnow: reconciliation scan failed")
)

var jobScopePattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// JobLease is the opaque ownership receipt returned by JobRepository. Callers
// must not change its fields before completing or releasing the lease.
type JobLease struct {
	// Scope is a stable host-supplied SHA-256 identity, never a raw origin/key.
	Scope string `bson:"_id"`
	// Token fences one acquisition from all earlier and later owners.
	Token string `bson:"lease_token"`
	// Checkpoint is the inclusive upper bound of the last complete scan.
	Checkpoint time.Time `bson:"checkpoint_at"`
	// Initialized distinguishes activation from an ordinary due scan.
	Initialized bool `bson:"initialized"`
	// ExpiresAt is assigned against MongoDB server time by the durable adapter.
	ExpiresAt time.Time `bson:"lease_until"`
}

// JobRepository atomically initializes durable progress, grants one bounded
// lease, and fences every write by token and unexpired ownership. Implementations
// must reject ambiguous acknowledgement/count receipts. No operation authorizes
// replaying an outbound request whose outcome is uncertain.
type JobRepository interface {
	// Claim seeds a never-seen scope at activation, without historical work.
	// Subsequent acquisitions preserve the persisted checkpoint.
	Claim(ctx context.Context, scope, token string, activation time.Time, lease time.Duration) (*JobLease, error)
	// Complete advances through after a complete scan. For a new activation,
	// passing its unchanged checkpoint only marks initialization complete.
	Complete(ctx context.Context, lease *JobLease, through time.Time) error
	// Release clears ownership only while this exact lease is still unexpired.
	Release(ctx context.Context, lease *JobLease) error
	// Defer preserves the checkpoint and sets a durable server-time retry floor.
	// A positive delay must never be rounded down or shortened by a later call.
	Defer(ctx context.Context, lease *JobLease, delay time.Duration) error
}

func validJobLease(lease *JobLease) bool {
	return lease != nil && jobScopePattern.MatchString(lease.Scope) && validJobToken(lease.Token) &&
		!lease.Checkpoint.IsZero() && !lease.ExpiresAt.IsZero()
}
