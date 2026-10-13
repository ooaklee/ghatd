package repository

import (
	"context"
	"errors"
	"fmt"
	"github.com/ooaklee/ghatd/external/observability"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"testing"
)

func TestMongoDiagnosticIdentityPreservesErrorAndSeverity(t *testing.T) {
	sentinel := errors.New("private-owning-sentinel-value")
	unknown := errors.New("private-provider-payload user@example.test")
	classifier, err := observability.NewErrorClassifier(observability.ErrorRule{Err: sentinel, Code: "owner.absent", Outcome: observability.OutcomeError})
	require.NoError(t, err)
	for _, tc := range []struct {
		name            string
		err             error
		identity, level string
	}{
		{"bare_owner_error", sentinel, "owner.absent", "error"},
		{"wrapped_owner_error", fmt.Errorf("private-message: %w", sentinel), "owner.absent", "error"},
		{"joined_match_does_not_suppress_failure", errors.Join(sentinel, unknown), "owner.absent", "error"},
		{"unknown_private_error", unknown, "internal", "error"},
		{"deadline", context.DeadlineExceeded, "timeout", "error"},
		{"success", nil, "", "debug"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, e := mongo.Connect(options.Client().ApplyURI("mongodb://127.0.0.1:1"))
			require.NoError(t, e)
			t.Cleanup(func() { require.NoError(t, client.Disconnect(context.Background())) })
			log := &findOneRecordingLogger{}
			ctx := WithMongoErrorClassifier(t.Context(), classifier)
			result, e := mongoCommand(ctx, log, "fixture", client.Database("fixture").Collection("records"), func() (int, error) { return 7, tc.err })
			require.Equal(t, 7, result)
			require.True(t, e == tc.err, "telemetry must return the identical error")
			require.Len(t, log.entries, 1)
			entry := log.entries[0]
			require.Equal(t, tc.level, entry.level)
			require.Nil(t, entry.err)
			require.Contains(t, entry.fields, Field{Key: "error_identity", Value: tc.identity})
			require.NotContains(t, fmt.Sprint(entry.fields), "private")
			require.NotContains(t, fmt.Sprint(entry.fields), "example.test")
		})
	}
	require.Nil(t, WithMongoErrorClassifier(nil, classifier))
}

func TestMongoTransactionDiagnosticsNativeFailureAndRetry(t *testing.T) {
	for _, tc := range []struct {
		name              string
		failCommit        bool
		transientCallback bool
		wantAttempts      int
		wantError         bool
	}{
		{"native_commit_error_after_successful_callback", true, false, 1, true},
		{"transient_callback_then_success", false, true, 2, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, db, ctx, log := isolatedRepository(t)
			require.NoError(t, repo.ProbeMongoTransactions(ctx, db.Collection("records")))
			if tc.failCommit {
				admin := db.Client().Database("admin")
				require.NoError(t, admin.RunCommand(ctx, bson.D{{Key: "configureFailPoint", Value: "failCommand"}, {Key: "mode", Value: bson.M{"times": 1}}, {Key: "data", Value: bson.M{"failCommands": bson.A{"commitTransaction"}, "errorCode": 2}}}).Err())
				t.Cleanup(func() {
					require.NoError(t, admin.RunCommand(context.Background(), bson.D{{Key: "configureFailPoint", Value: "failCommand"}, {Key: "mode", Value: "off"}}).Err())
				})
			}
			var sessionID bson.Raw
			if tc.failCommit {
				t.Cleanup(func() {
					if sessionID != nil {
						require.NoError(t, db.Client().Database("admin").RunCommand(context.Background(), bson.D{{Key: "killSessions", Value: bson.A{sessionID}}}).Err())
					}
				})
			}
			attempts := 0
			err := repo.WithMongoTransaction(ctx, db, func(tx context.Context) error {
				attempts++
				sessionID = mongo.SessionFromContext(tx).ID()
				_, e := db.Collection("records").CountDocuments(tx, bson.M{})
				if e != nil {
					return e
				}
				if tc.transientCallback && attempts == 1 {
					return mongo.CommandError{Code: 112, Message: "private-native-error", Labels: []string{"TransientTransactionError"}}
				}
				return nil
			})
			require.Equal(t, tc.wantAttempts, attempts)
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			entry := log.entries[len(log.entries)-1]
			require.Contains(t, entry.fields, Field{Key: "callback_attempts", Value: tc.wantAttempts})
			require.Contains(t, entry.fields, Field{Key: "last_callback_error_identity", Value: "success"})
			require.NotContains(t, fmt.Sprint(entry.fields), "private")
			require.Nil(t, entry.err)
			if tc.failCommit {
				require.Contains(t, entry.fields, Field{Key: "mongo_error_code", Value: int32(2)})
			}
		})
	}
}

// Existing operation fields remain exact; native metadata has precisely three
// additional keys and cannot become an arbitrary repository-field escape hatch.
func requireNativeMongoDiagnosticFields(t *testing.T, fields []Field) {
	t.Helper()
	if len(fields) == 2 {
		return
	}
	require.Len(t, fields, 5)
	require.Equal(t, "mongo_error_code", fields[2].Key)
	require.Equal(t, "mongo_transient_transaction", fields[3].Key)
	require.Equal(t, "mongo_unknown_commit_result", fields[4].Key)
	require.IsType(t, int32(0), fields[2].Value)
	require.IsType(t, false, fields[3].Value)
	require.IsType(t, false, fields[4].Value)
}
