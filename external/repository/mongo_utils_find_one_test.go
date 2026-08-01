package repository

import (
	"context"
	"errors"
	"testing"

	"go.mongodb.org/mongo-driver/v2/mongo"
)

type findOneRecordingLogger struct {
	warns  []error
	errors []error
}

func (l *findOneRecordingLogger) Error(_ context.Context, _ string, err error, _ ...Field) {
	l.errors = append(l.errors, err)
}

func (l *findOneRecordingLogger) Warn(_ context.Context, _ string, err error, _ ...Field) {
	l.warns = append(l.warns, err)
}

func (*findOneRecordingLogger) Info(context.Context, string, error, ...Field)  {}
func (*findOneRecordingLogger) Debug(context.Context, string, error, ...Field) {}

func TestHandleFindOneDecodeErrorMapsMissingDocumentAndWarnsWhenRequested(t *testing.T) {
	logger := &findOneRecordingLogger{}
	repository := NewMongoRepositoryHelper(nil, logger, "")
	notFound := errors.New("resource not found")

	if got := repository.handleFindOneDecodeError(context.Background(), mongo.ErrNoDocuments, "users", nil, "user", true, notFound); !errors.Is(got, notFound) {
		t.Fatalf("missing document error = %v, want mapped not-found error", got)
	}
	if len(logger.warns) != 1 || !errors.Is(logger.warns[0], mongo.ErrNoDocuments) {
		t.Fatalf("warnings = %#v, want one ErrNoDocuments warning", logger.warns)
	}
	if len(logger.errors) != 0 {
		t.Fatalf("errors = %#v, want none", logger.errors)
	}
}

func TestHandleFindOneDecodeErrorPreservesDatabaseErrorAndLogsErrorWhenRequested(t *testing.T) {
	logger := &findOneRecordingLogger{}
	repository := NewMongoRepositoryHelper(nil, logger, "")
	databaseError := errors.New("database unavailable")

	if got := repository.handleFindOneDecodeError(context.Background(), databaseError, "users", nil, "user", true, errors.New("resource not found")); !errors.Is(got, databaseError) {
		t.Fatalf("database error = %v, want original error", got)
	}
	if len(logger.errors) != 1 || !errors.Is(logger.errors[0], databaseError) {
		t.Fatalf("errors = %#v, want one database error", logger.errors)
	}
	if len(logger.warns) != 0 {
		t.Fatalf("warnings = %#v, want none", logger.warns)
	}
}

func TestHandleFindOneDecodeErrorDoesNotLogWhenDisabled(t *testing.T) {
	logger := &findOneRecordingLogger{}
	repository := NewMongoRepositoryHelper(nil, logger, "")
	databaseError := errors.New("database unavailable")

	if got := repository.handleFindOneDecodeError(context.Background(), databaseError, "users", nil, "user", false, errors.New("resource not found")); !errors.Is(got, databaseError) {
		t.Fatalf("database error = %v, want original error", got)
	}
	if len(logger.errors) != 0 || len(logger.warns) != 0 {
		t.Fatalf("logs = errors:%#v warnings:%#v, want none", logger.errors, logger.warns)
	}
}
