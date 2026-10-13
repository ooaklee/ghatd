package partnerstore

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/encryption"
	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/ooaklee/ghatd/external/repository"
	"github.com/ooaklee/ghatd/external/repository/recordstore"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type cursorLogEntry struct {
	message string
	fields  []repository.Field
}

type cursorRecordingLogger struct {
	mu       sync.Mutex
	errors   []cursorLogEntry
	observed *observer.ObservedLogs
}

func (l *cursorRecordingLogger) Error(ctx context.Context, message string, _ error, fields ...repository.Field) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.errors = append(l.errors, cursorLogEntry{message, fields})
	repository.NewZapRepositoryLogger().Error(ctx, message, nil, fields...)
}
func (*cursorRecordingLogger) Warn(context.Context, string, error, ...repository.Field)  {}
func (*cursorRecordingLogger) Info(context.Context, string, error, ...repository.Field)  {}
func (*cursorRecordingLogger) Debug(context.Context, string, error, ...repository.Field) {}

type cursorCommandResults struct {
	mu        sync.Mutex
	succeeded map[string]int
	failed    int
}

// This fixture owns a random database on the explicitly selected test replica
// set, and records native command outcomes separately from repository log levels.
func mongoCursorFixture(t *testing.T, failFind bool) (*WorkRepository, *partnermanager.WorkQueue, *mongo.Database, context.Context, *cursorRecordingLogger, *cursorCommandResults) {
	t.Helper()
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_URI to an isolated replica set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	t.Cleanup(cancel)
	commands := &cursorCommandResults{succeeded: map[string]int{}}
	monitor := &event.CommandMonitor{
		Succeeded: func(_ context.Context, e *event.CommandSucceededEvent) {
			commands.mu.Lock()
			defer commands.mu.Unlock()
			commands.succeeded[e.CommandName]++
		},
		Failed: func(context.Context, *event.CommandFailedEvent) {
			commands.mu.Lock()
			defer commands.mu.Unlock()
			commands.failed++
		},
	}
	client, err := mongo.Connect(options.Client().ApplyURI(uri).SetMonitor(monitor))
	require.NoError(t, err)
	db := client.Database("ghatd_cursor_test_" + strings.ToLower(rand.Text()))
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		require.NoError(t, db.Drop(cleanup))
		require.NoError(t, client.Disconnect(cleanup))
	})
	cipher, err := encryption.NewPayloadCipher([]byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	core, observed := observer.New(zap.DebugLevel)
	ctx = logger.TransitWith(ctx, zap.New(core))
	log := &cursorRecordingLogger{observed: observed}
	port, err := repository.NewMongoDbRepositoryFromDatabase(db, log)
	require.NoError(t, err)
	var operations recordstore.MongoPort = port
	if failFind {
		operations = cursorFailedFindPort{operations}
	}
	store, err := recordstore.NewMongoStore(db, operations, cipher)
	require.NoError(t, err)
	require.NoError(t, store.EnsureIndexes(ctx))
	require.NoError(t, store.Probe(ctx))
	repo, err := NewWorkRepository(store)
	require.NoError(t, err)
	queue, err := partnermanager.NewWorkQueue(repo, &mongoTestClock{time.Now().UTC()}, partnermanager.WorkQueueConfig{ProgramID: "fixture-program", LeaseDuration: time.Minute, RetryBase: time.Second, RetryMax: time.Hour})
	require.NoError(t, err)
	return repo, queue, db, ctx, log, commands
}

// Only this fixture's query is invalid; no server-wide failpoint or other test
// database is changed. The native driver must return a genuine command error.
type cursorFailedFindPort struct{ recordstore.MongoPort }

func (p cursorFailedFindPort) ExecuteFindOneCommandDecodeResult(ctx context.Context, collection *mongo.Collection, filter any, result any, name string, logError bool, onFailure error) error {
	if name == "owned-record" {
		filter = bson.M{"$invalid_cursor_test_operator": 1}
	}
	return p.MongoPort.ExecuteFindOneCommandDecodeResult(ctx, collection, filter, result, name, logError, onFailure)
}

func TestMongoDiscoveryCursorDiagnosticsPreserveAbsenceBehavior(t *testing.T) {
	cases := []struct {
		name, kind string
	}{
		{"signup", partnermanager.WorkSignup},
		{"revenue_source", partnermanager.WorkRevenueSource},
		{"revenue", partnermanager.WorkRevenue},
		{"maturity", partnermanager.WorkMaturity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, queue, db, ctx, log, commands := mongoCursorFixture(t, false)
			for pass := 0; pass < 3; pass++ {
				cursor, err := queue.Cursor(ctx, tc.kind)
				require.NoError(t, err)
				require.Equal(t, partnermanager.DiscoveryCursor{}, cursor)
				_, err = repo.GetDiscoveryCursor(ctx, "fixture-program", tc.kind)
				require.ErrorIs(t, err, partnermanager.ErrNotFound)
			}
			count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{"kind": kindWorkCursor})
			require.NoError(t, err)
			require.Zero(t, count, "normal cursor reads must not seed durable state")
			t.Logf("six absent cursor reads: repository errors=%d, successful finds=%d, successful aborts=%d, native command failures=%d", len(log.errors), commands.succeeded["find"], commands.succeeded["abortTransaction"], commands.failed)
			require.Zero(t, commands.failed)
			require.Len(t, log.errors, 6, "diagnostics must retain the existing error severity and count")
			require.Equal(t, 6, commands.succeeded["abortTransaction"], "absence must still abort; the behavior fix is separate")
			for _, entry := range log.observed.FilterMessage("mongo-operation-failed").All() {
				fields := entry.ContextMap()
				require.Equal(t, "partners.discovery_cursor.read", fields["owner_operation"])
				require.Equal(t, tc.kind, fields["work_kind"])
				require.Equal(t, "partner.not_found", fields["error_identity"])
				require.Equal(t, "partner.not_found", fields["last_callback_error_identity"])
				require.EqualValues(t, 1, fields["callback_attempts"])
				require.NotContains(t, fields, "error")
			}
		})
	}
}

func TestMongoDiscoveryCursorDiagnosticsPreserveFailures(t *testing.T) {
	cases := []struct {
		name                         string
		seed, corrupt, driverFailure bool
	}{
		{name: "existing_cursor", seed: true},
		{name: "unreadable_cursor", seed: true, corrupt: true},
		{name: "native_database_error", driverFailure: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo, queue, db, ctx, log, commands := mongoCursorFixture(t, tc.driverFailure)
			if tc.seed {
				require.NoError(t, queue.EnqueuePage(ctx, partnermanager.WorkSignup, partnermanager.DiscoveryCursor{}, partnermanager.DiscoveryCursor{AfterID: "source"}, []partnermanager.WorkCandidate{{SourceID: "source", SourceFingerprint: "fingerprint"}}))
			}
			if tc.corrupt {
				_, err := db.Collection("ghatd_owned_records").UpdateOne(ctx, bson.M{"kind": kindWorkCursor}, bson.M{"$set": bson.M{"payload": []byte("unreadable")}})
				require.NoError(t, err)
			}
			cursor, err := repo.GetDiscoveryCursor(ctx, "fixture-program", partnermanager.WorkSignup)
			if !tc.corrupt && !tc.driverFailure {
				require.NoError(t, err)
				require.Equal(t, "source", cursor.AfterID)
				require.Empty(t, log.errors)
				return
			}
			require.Error(t, err)
			require.False(t, errors.Is(err, partnermanager.ErrNotFound))
			require.NotEmpty(t, log.errors, "unexpected callback/driver failures must remain visible")
			last := log.errors[len(log.errors)-1]
			require.Equal(t, "mongo-operation-failed", last.message)
			require.Contains(t, last.fields, repository.Field{Key: "operation", Value: "transaction"})
			require.Contains(t, last.fields, repository.Field{Key: "outcome", Value: "database"})
			if tc.driverFailure {
				var server mongo.ServerError
				require.ErrorAs(t, err, &server)
				require.Positive(t, commands.failed)
			}
		})
	}
}
