package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// operationLog retains exactly the helper's published fields for privacy checks.
type operationLog struct {
	level   string
	message string
	err     error
	fields  []Field
}

// TestMongoCommandEntry ensures invalid helpers cannot run a driver operation or
// reach a logger through a typed-nil interface. No database connection is used.
func TestMongoCommandEntry(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"valid helper", nil},
		{"nil helper", ErrInvalidMongoOperation},
		{"nil custom logger", ErrInvalidMongoOperation},
		{"nil logger interface", ErrInvalidMongoOperation},
		{"nil context", ErrInvalidMongoOperation},
		{"nil collection", ErrInvalidMongoOperation},
		{"nil callback", ErrInvalidMongoOperation},
		{"cancelled context", context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, err := mongo.Connect(options.Client().ApplyURI("mongodb://127.0.0.1:1"))
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, client.Disconnect(context.Background())) })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var helper RepositoryLogger = NewMongoRepositoryHelper(nil, &findOneRecordingLogger{}, "")
			collection := client.Database("entry_fixture").Collection("records")
			called := false
			run := func() (int, error) { called = true; return 42, nil }
			switch tc.name {
			case "nil helper":
				helper = (*MongoRepositoryHelper)(nil)
			case "nil custom logger":
				helper = (*findOneRecordingLogger)(nil)
			case "nil logger interface":
				helper = nil
			case "nil context":
				ctx = nil
			case "nil collection":
				collection = nil
			case "nil callback":
				run = nil
			case "cancelled context":
				cancel()
			}
			value, err := mongoCommand(ctx, helper, "entry_test", collection, run)
			require.ErrorIs(t, err, tc.want)
			require.Equal(t, tc.want == nil, called)
			if tc.want != nil {
				require.Zero(t, value)
			} else {
				require.Equal(t, 42, value)
			}
		})
	}
}

type findOneRecordingLogger struct{ entries []operationLog }

func (l *findOneRecordingLogger) Error(_ context.Context, message string, err error, fields ...Field) {
	l.entries = append(l.entries, operationLog{"error", message, err, fields})
}
func (l *findOneRecordingLogger) Warn(_ context.Context, message string, err error, fields ...Field) {
	l.entries = append(l.entries, operationLog{"warn", message, err, fields})
}
func (l *findOneRecordingLogger) Info(_ context.Context, message string, err error, fields ...Field) {
	l.entries = append(l.entries, operationLog{"info", message, err, fields})
}
func (l *findOneRecordingLogger) Debug(_ context.Context, message string, err error, fields ...Field) {
	l.entries = append(l.entries, operationLog{"debug", message, err, fields})
}

func TestFindOneErrorMappingAndPrivateLogs(t *testing.T) {
	missing := errors.New("domain/missing")
	driverErr := mongo.CommandError{Code: 112, Message: "private duplicate: somebody@example.invalid", Labels: []string{"TransientTransactionError"}}
	for _, tc := range []struct {
		name                     string
		input, replacement, want error
		logging                  bool
		class                    string
	}{
		{"success", nil, missing, nil, true, "success"},
		{"mapped absence", mongo.ErrNoDocuments, missing, missing, true, "not_found"},
		{"native absence", mongo.ErrNoDocuments, nil, mongo.ErrNoDocuments, true, "not_found"},
		{"driver preserved", driverErr, missing, driverErr, true, "database"},
		{"disabled", driverErr, missing, driverErr, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := &findOneRecordingLogger{}
			repo := NewMongoRepositoryHelper(nil, log, "")
			got := repo.handleFindOneDecodeError(context.Background(), tc.input, "private collection", "private filter", "private name", tc.logging, tc.replacement)
			require.Equal(t, tc.want, got)
			if !tc.logging {
				require.Empty(t, log.entries)
				return
			}
			require.Len(t, log.entries, 1)
			entry := log.entries[0]
			require.Nil(t, entry.err, "raw errors must not reach a custom logger")
			require.Equal(t, []Field{{Key: "operation", Value: "find_one_decode"}, {Key: "outcome", Value: tc.class}}, entry.fields)
			require.NotContains(t, entry.message, "private")
		})
	}
}
