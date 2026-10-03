package accesspolicy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/ooaklee/ghatd/external/repository"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/mongo/readconcern"
	"go.mongodb.org/mongo-driver/v2/mongo/readpref"
	"go.mongodb.org/mongo-driver/v2/mongo/writeconcern"
)

const (
	// These collections are owned exclusively by the policy store. Their _id
	// indexes enforce tuple uniqueness; receipts and audits deliberately have no TTL.
	grantCollection   = "access_policy_grants"
	auditCollection   = "access_policy_audit"
	counterCollection = "access_policy_counters"
	receiptCollection = "access_policy_receipts"
)

// MongoStore implements grants, revision auditing and usage on one managed Mongo
// client/database. Call Initialize before serving traffic. It requires a replica
// set or sharded cluster with transactions; there is no standalone fallback.
// Do not copy a MongoStore after initialization or disconnect its shared client.
type MongoStore struct {
	// database belongs to MongoDbRepository; the store never owns its lifecycle.
	database *mongo.Database
	// core supplies privacy-safe operations and transaction lifecycle helpers.
	core *repository.MongoDbRepository
	// ready becomes true only after collection setup and a transaction probe.
	ready atomic.Bool
}

var _ Store = (*MongoStore)(nil)

// NewMongoStore resolves the repository's default database without constructing a
// second client or mutating collections. Initialize is an explicit startup step.
func NewMongoStore(ctx context.Context, repo *repository.MongoDbRepository) (*MongoStore, error) {
	if ctx == nil || repo == nil || repo.GetHelper() == nil || mongo.SessionFromContext(ctx) != nil {
		return nil, ErrConfiguration
	}
	db, err := repo.GetDatabase(ctx, "")
	if err != nil {
		return nil, err
	}
	if db == nil {
		return nil, ErrConfiguration
	}
	return &MongoStore{database: db, core: repo}, nil
}

// Initialize idempotently creates the four collections and verifies transactional
// read/write/commit support. Setup requires create/read/write/delete privileges;
// it seeds no policy and its atomic probe leaves no committed document behind.
// Use a bounded context and fail host startup on error. No TTL or destructive
// migration is installed, so retries cannot silently forget prior admissions.
func (s *MongoStore) Initialize(ctx context.Context) error {
	if ctx == nil || s == nil || s.database == nil || s.core == nil || mongo.SessionFromContext(ctx) != nil {
		return ErrConfiguration
	}
	for _, name := range []string{grantCollection, auditCollection, counterCollection, receiptCollection} {
		if err := s.core.EnsureMongoCollection(ctx, s.database, name); err != nil {
			return err
		}
	}
	err := s.core.ProbeMongoTransactions(ctx, s.collection(grantCollection))
	if err == nil {
		s.ready.Store(true)
	}
	return err
}

// Read returns a fresh primary/majority snapshot, including disabled grants for
// management inspection. Service.Resolve applies eligibility. Existing sessions
// are rejected so an older caller snapshot cannot masquerade as a live read.
func (s *MongoStore) Read(ctx context.Context, subject Subject) (Grant, error) {
	if err := s.checkContext(ctx); err != nil {
		return Grant{}, err
	}
	if !validSubject(subject) {
		return Grant{}, ErrDenied
	}
	return s.readGrant(ctx, subject)
}

// Replace commits a create-only or revision-CAS replacement and its audit record
// together. Grant.Revision must equal expected+1. This persistence operation is
// trusted-only: callers should normally use Service.ReplaceGrant for live actor
// authorization. Usage fencing does not change the administrative revision.
// Returned expiry uses BSON millisecond precision, matching subsequent reads.
func (s *MongoStore) Replace(ctx context.Context, grant Grant, expected int64, actor string, at time.Time) (Grant, error) {
	if err := s.checkContext(ctx); err != nil {
		return Grant{}, err
	}
	if expected < 0 || expected >= 9007199254740991 || grant.Revision != expected+1 || validateGrant(grant) != nil || !validName(actor) || at.IsZero() {
		return Grant{}, ErrConfiguration
	}
	record := encodeGrant(grant)
	err := s.transaction(ctx, func(tx context.Context) error {
		if expected == 0 {
			if _, err := s.core.ExecuteInsertOneCommand(tx, s.collection(grantCollection), record, "grant"); err != nil {
				return err
			}
		} else {
			result, err := s.core.ExecuteReplaceOneCommandResult(tx, s.collection(grantCollection), bson.M{"_id": record.ID, "revision": expected}, record)
			if err != nil {
				return err
			}
			if result.MatchedCount != 1 {
				return ErrConflict
			}
		}
		_, err := s.core.ExecuteInsertOneCommand(tx, s.collection(auditCollection), mongoAudit{
			ID:    tupleID(record.ID, strconv.FormatInt(grant.Revision, 10)),
			Actor: actor, Expected: expected, At: at.UTC(), Grant: record,
		}, "audit")
		return err
	})
	if mongo.IsDuplicateKeyError(err) {
		return Grant{}, ErrConflict
	}
	if err != nil {
		return Grant{}, err
	}
	return record.decode()
}

// WithGrant serializes a policy and callback writes in one transaction. All
// callback database operations MUST use tx and this repository's client, return
// database errors, and never end the session/transaction. Automatic retries may
// invoke the callback repeatedly: do not send messages, make remote calls, or
// publish results until this method returns nil. Caller sessions are rejected
// rather than silently detached; use this as the outer transaction boundary.
func (s *MongoStore) WithGrant(ctx context.Context, subject Subject, at time.Time, callback func(context.Context, Grant) error) error {
	if err := s.checkContext(ctx); err != nil {
		return err
	}
	if !validSubject(subject) || at.IsZero() || callback == nil {
		return ErrConfiguration
	}
	now := advancingClock(at)
	return s.transaction(ctx, func(tx context.Context) error {
		grant, err := s.fenceGrant(tx, subject, now())
		if err != nil {
			return err
		}
		// Preserve the original eligibility snapshot even if the trusted callback
		// changes its copy's slices, limits or expiry.
		copy, err := encodeGrant(grant).decode()
		if err != nil {
			return err
		}
		if err := callback(tx, copy); err != nil {
			return err
		}
		return usableGrant(grant, subject, now())
	})
}

// Consume checks current policy, fences revocation, and commits a counter and
// replay receipt atomically. Replays recheck authority and a positive current
// metric but return the original usage even after a positive limit reduction.
// Changing window duration deliberately starts a separate budget; ordinary grant
// revision changes preserve existing counts. Missing/zero metrics deny replay.
func (s *MongoStore) Consume(ctx context.Context, request Consumption, at time.Time) (Usage, error) {
	return s.consume(ctx, request, at, nil)
}

// WithConsumption commits callback writes, quota and receipt in one transaction.
// It always runs Check for current resource authority but skips Apply on a
// matching replay. Callbacks have WithGrant's context/client/retry obligations;
// the policy store cannot infer resource authority from an operation key.
func (s *MongoStore) WithConsumption(ctx context.Context, request Consumption, at time.Time, action ConsumptionAction) (Usage, error) {
	if action.Check == nil || action.Apply == nil {
		return Usage{}, ErrConfiguration
	}
	return s.consume(ctx, request, at, &action)
}

// consume owns both admission-only and atomic business-operation paths.
func (s *MongoStore) consume(ctx context.Context, request Consumption, at time.Time, action *ConsumptionAction) (Usage, error) {
	if err := s.checkContext(ctx); err != nil {
		return Usage{}, err
	}
	if validateConsumption(request) != nil || at.IsZero() {
		return Usage{}, ErrConfiguration
	}
	request.Scopes = append([]string(nil), request.Scopes...)
	request.Permissions = append([]string(nil), request.Permissions...)
	now := advancingClock(at)
	var usage Usage
	err := s.transaction(ctx, func(tx context.Context) error {
		grant, err := s.fenceGrant(tx, request.Subject, now())
		if err != nil {
			return err
		}
		if !includes(grant.Scopes, request.Scopes) || !includes(grant.Permissions, request.Permissions) {
			return ErrDenied
		}
		limit, exists := grant.Limits[request.Metric]
		if !exists {
			return ErrDenied
		}
		if limit.Maximum == 0 {
			return ErrLimitReached
		}
		if action != nil {
			if err := action.Check(tx); err != nil {
				return err
			}
		}
		subjectID := grantID(request.Subject)
		receiptID := tupleID(subjectID, request.Metric, request.Key)
		var receipt mongoReceipt
		err = s.core.ExecuteFindOneCommandDecodeResult(tx, s.collection(receiptCollection), bson.M{"_id": receiptID}, &receipt, "policy", true, nil)
		if err == nil {
			if receipt.Subject != request.Subject || receipt.Metric != request.Metric || receipt.Key != request.Key || receipt.Revision < 1 || receipt.Usage.Count < 1 || receipt.Usage.Maximum < receipt.Usage.Count || receipt.Usage.ResetsAt.IsZero() {
				return ErrConfiguration
			}
			if receipt.Fingerprint != request.Fingerprint {
				return ErrConflict
			}
			usage = receipt.Usage
			usage.Replayed = true
			return usableGrant(grant, request.Subject, now())
		}
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return err
		}
		start, end := fixedWindow(now(), limit.WindowSeconds)
		counterID := tupleID(subjectID, request.Metric, strconv.FormatInt(limit.WindowSeconds, 10), strconv.FormatInt(start.Unix(), 10))
		var counter mongoCounter
		err = s.core.ExecuteFindOneCommandDecodeResult(tx, s.collection(counterCollection), bson.M{"_id": counterID}, &counter, "policy", true, nil)
		if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
			return err
		}
		if err == nil && (counter.Subject != request.Subject || counter.Metric != request.Metric || counter.WindowSeconds != limit.WindowSeconds || !counter.StartsAt.Equal(start)) {
			return ErrConfiguration
		}
		if counter.Count < 0 {
			return ErrConfiguration
		}
		if counter.Count >= limit.Maximum {
			return ErrLimitReached
		}
		counter = mongoCounter{ID: counterID, Subject: request.Subject, Metric: request.Metric, WindowSeconds: limit.WindowSeconds, StartsAt: start, Count: counter.Count + 1}
		if _, err := s.core.ExecuteReplaceOneCommandResult(tx, s.collection(counterCollection), bson.M{"_id": counterID}, counter, options.Replace().SetUpsert(true)); err != nil {
			return err
		}
		usage = Usage{Count: counter.Count, Maximum: limit.Maximum, ResetsAt: end}
		receipt = mongoReceipt{ID: receiptID, Subject: request.Subject, Metric: request.Metric, Key: request.Key, Fingerprint: request.Fingerprint, Revision: grant.Revision, Usage: usage}
		if _, err := s.core.ExecuteInsertOneCommand(tx, s.collection(receiptCollection), receipt, "receipt"); err != nil {
			return err
		}
		if action != nil {
			if err := action.Apply(tx, usage); err != nil {
				return err
			}
		}
		return usableGrant(grant, request.Subject, now())
	})
	if err != nil {
		return Usage{}, err
	}
	return usage, nil
}

// checkContext requires explicit initialization and rejects nested or foreign
// sessions. The driver otherwise silently replaces caller sessions.
func (s *MongoStore) checkContext(ctx context.Context) error {
	if ctx == nil || s == nil || s.database == nil || s.core == nil || !s.ready.Load() || mongo.SessionFromContext(ctx) != nil {
		return ErrConfiguration
	}
	return ctx.Err()
}

// collection forces primary reads and majority durability independently of
// permissive defaults on the host client. Transaction options override concerns.
func (s *MongoStore) collection(name string) *mongo.Collection {
	return s.database.Collection(name, options.Collection().SetReadConcern(readconcern.Majority()).SetWriteConcern(writeconcern.Majority()).SetReadPreference(readpref.Primary()))
}

// transaction delegates bounded transient/uncertain-commit retries to the driver.
// It does not wrap errors before the driver can inspect their retry labels.
func (s *MongoStore) transaction(ctx context.Context, callback func(context.Context) error) error {
	err := s.core.WithMongoTransaction(ctx, s.database, callback)
	if errors.Is(err, repository.ErrInvalidMongoOperation) {
		return ErrConfiguration
	}
	return err
}

// readGrant decodes a new policy value and rejects inconsistent persisted tuples.
func (s *MongoStore) readGrant(ctx context.Context, subject Subject) (Grant, error) {
	var record mongoGrant
	err := s.core.ExecuteFindOneCommandDecodeResult(ctx, s.collection(grantCollection), bson.M{"_id": grantID(subject)}, &record, "policy", true, nil)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return Grant{}, ErrDenied
	}
	if err != nil {
		return Grant{}, err
	}
	grant, err := record.decode()
	if err == nil && grant.Subject != subject {
		return Grant{}, ErrConfiguration
	}
	return grant, err
}

// fenceGrant forces an actual document write, preventing snapshot write skew
// between a policy edit and a counter or inventory write in another collection.
// The random marker avoids both no-op updates and an eventually overflowing count.
func (s *MongoStore) fenceGrant(ctx context.Context, subject Subject, now time.Time) (Grant, error) {
	grant, err := s.readGrant(ctx, subject)
	if err != nil {
		return Grant{}, err
	}
	if err := usableGrant(grant, subject, now); err != nil {
		return Grant{}, err
	}
	result, err := s.core.ExecuteUpdateOneCommandResult(ctx, s.collection(grantCollection), bson.M{"_id": grantID(subject), "revision": grant.Revision}, bson.M{"$set": bson.M{"fence": rand.Text()}})
	if err != nil {
		return Grant{}, err
	}
	if result.MatchedCount != 1 {
		return Grant{}, ErrConflict
	}
	return grant, nil
}

// tupleID uses a canonical array encoding, not delimiter concatenation, so
// identifiers containing punctuation cannot alias another subject or operation.
func tupleID(parts ...string) string {
	encoded, _ := json.Marshal(parts) // strings are validated UTF-8 at entry points
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// grantID defines the immutable namespace shared by policy, counters and receipts.
func grantID(subject Subject) string { return tupleID(subject.System, subject.Kind, subject.ID) }

// advancingClock retains an injected operation epoch while advancing during
// driver retries and callbacks. It is not a promise of post-commit expiry checks.
func advancingClock(at time.Time) func() time.Time {
	started := time.Now()
	return func() time.Time { return at.Add(time.Since(started)).UTC() }
}

// fixedWindow floors to a UTC Unix-epoch interval, including pre-epoch times.
func fixedWindow(at time.Time, seconds int64) (time.Time, time.Time) {
	unix := at.Unix()
	start := unix - ((unix%seconds)+seconds)%seconds
	return time.Unix(start, 0).UTC(), time.Unix(start+seconds, 0).UTC()
}

// mongoLimit stores metric names as values, allowing dots/$ without creating
// dynamic BSON keys or update operators. Entries are sorted when encoded.
type mongoLimit struct {
	Name  string `bson:"name"`  // Name is the exact quota metric.
	Limit Limit  `bson:"limit"` // Limit is its fixed-window policy.
}

// mongoGrant is the private persistence schema; maps are encoded as named pairs.
type mongoGrant struct {
	ID          string       `bson:"_id"`                  // ID hashes the immutable subject tuple.
	Subject     Subject      `bson:"subject"`              // Subject retains inspectable identity metadata.
	Revision    int64        `bson:"revision"`             // Revision changes only on policy replacement.
	Enabled     bool         `bson:"enabled"`              // Enabled is the explicit allow/deny switch.
	ExpiresAt   time.Time    `bson:"expires_at,omitempty"` // ExpiresAt is stored at BSON millisecond precision.
	Scopes      []string     `bson:"scopes"`               // Scopes are exact delegated capabilities.
	Permissions []string     `bson:"permissions"`          // Permissions are exact authorization names.
	Tokens      TokenLimits  `bson:"tokens"`               // Tokens bounds credential inventory and TTL.
	Limits      []mongoLimit `bson:"limits"`               // Limits avoids unsafe map-key encoding.
	Fence       string       `bson:"fence,omitempty"`      // Fence forces serialization across related writes.
}

// encodeGrant detaches caller-owned slices/maps and normalizes BSON time precision.
func encodeGrant(grant Grant) mongoGrant {
	record := mongoGrant{ID: grantID(grant.Subject), Subject: grant.Subject, Revision: grant.Revision, Enabled: grant.Enabled, ExpiresAt: grant.ExpiresAt.UTC().Truncate(time.Millisecond), Scopes: append([]string(nil), grant.Scopes...), Permissions: append([]string(nil), grant.Permissions...), Tokens: grant.Tokens}
	for name, limit := range grant.Limits {
		record.Limits = append(record.Limits, mongoLimit{Name: name, Limit: limit})
	}
	sort.Slice(record.Limits, func(i, j int) bool { return record.Limits[i].Name < record.Limits[j].Name })
	return record
}

// decode validates persisted policy and prevents ambiguous duplicate metric names.
func (record mongoGrant) decode() (Grant, error) {
	grant := Grant{Subject: record.Subject, Revision: record.Revision, Enabled: record.Enabled, ExpiresAt: record.ExpiresAt, Scopes: append([]string(nil), record.Scopes...), Permissions: append([]string(nil), record.Permissions...), Tokens: record.Tokens, Limits: make(map[string]Limit, len(record.Limits))}
	for _, entry := range record.Limits {
		if _, exists := grant.Limits[entry.Name]; exists {
			return Grant{}, ErrConfiguration
		}
		grant.Limits[entry.Name] = entry.Limit
	}
	if record.ID != grantID(grant.Subject) || grant.Revision < 1 || grant.Revision > 9007199254740991 || validateGrant(grant) != nil {
		return Grant{}, ErrConfiguration
	}
	return grant, nil
}

// mongoAudit is immutable replacement evidence written with the policy itself.
type mongoAudit struct {
	ID       string     `bson:"_id"`               // ID binds one subject and administrative revision.
	Actor    string     `bson:"actor"`             // Actor comes from the live management authorizer.
	Expected int64      `bson:"expected_revision"` // Expected records the caller's CAS revision.
	At       time.Time  `bson:"at"`                // At is the trusted request timestamp.
	Grant    mongoGrant `bson:"grant"`             // Grant preserves the complete resulting policy.
}

// mongoCounter holds admitted operations for one fixed window, not JWT claims.
type mongoCounter struct {
	ID            string    `bson:"_id"`            // ID includes subject, metric, duration and epoch start.
	Subject       Subject   `bson:"subject"`        // Subject is the separately billed identity.
	Metric        string    `bson:"metric"`         // Metric is selected by trusted route/domain code.
	WindowSeconds int64     `bson:"window_seconds"` // WindowSeconds distinguishes duration changes.
	StartsAt      time.Time `bson:"starts_at"`      // StartsAt is the inclusive UTC window boundary.
	Count         int64     `bson:"count"`          // Count changes only with a newly committed receipt.
}

// mongoReceipt retains replay identity across windows and policy revisions.
type mongoReceipt struct {
	ID          string  `bson:"_id"`         // ID includes subject, metric and operation key, not time.
	Subject     Subject `bson:"subject"`     // Subject prevents cross-system credential reuse.
	Metric      string  `bson:"metric"`      // Metric identifies the charged budget.
	Key         string  `bson:"key"`         // Key is a trusted operation identity.
	Fingerprint string  `bson:"fingerprint"` // Fingerprint binds the normalized command.
	Revision    int64   `bson:"revision"`    // Revision records the policy at original admission.
	Usage       Usage   `bson:"usage"`       // Usage is the original result returned on replay.
}
