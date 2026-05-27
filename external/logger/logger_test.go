package logger_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	ghatdlogger "github.com/ooaklee/ghatd/external/logger"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestAcquireFrom(t *testing.T) {
	testLogger := zap.NewExample()

	ctx := ghatdlogger.TransitWith(context.Background(), testLogger)

	retrievedLogger := ghatdlogger.AcquireFrom(ctx)

	assert.Samef(t, testLogger, retrievedLogger, "Logger are not the same")
}

func TestAcquirePackageFromAddsGHATDAttribution(t *testing.T) {
	core, observed := observer.New(zapcore.DebugLevel)
	testLogger := zap.New(core)
	ctx := ghatdlogger.TransitWith(context.Background(), testLogger)

	logger := ghatdlogger.AcquirePackageFrom(ctx, "external/notifier")
	logger.Debug("package-log")

	entries := observed.All()
	assert.Len(t, entries, 1)
	assert.Equal(t, ghatdlogger.SourceGHATD, entries[0].ContextMap()[ghatdlogger.FieldSource])
	assert.Equal(t, "external/notifier", entries[0].ContextMap()[ghatdlogger.FieldPackage])
}

func TestAcquireOperationFromAddsOperationAttribution(t *testing.T) {
	core, observed := observer.New(zapcore.DebugLevel)
	testLogger := zap.New(core)
	ctx := ghatdlogger.TransitWith(context.Background(), testLogger)

	logger := ghatdlogger.AcquireOperationFrom(ctx, "external/notifier", "notify-user")
	logger.Debug("operation-log")

	entries := observed.All()
	assert.Len(t, entries, 1)
	assert.Equal(t, ghatdlogger.SourceGHATD, entries[0].ContextMap()[ghatdlogger.FieldSource])
	assert.Equal(t, "external/notifier", entries[0].ContextMap()[ghatdlogger.FieldPackage])
	assert.Equal(t, "notify-user", entries[0].ContextMap()[ghatdlogger.FieldOperation])
}

func TestRequestPathHandlesMissingURL(t *testing.T) {
	assert.Equal(t, "", ghatdlogger.RequestPath(nil))
	assert.Equal(t, "", ghatdlogger.RequestPath(&http.Request{}))
	assert.Equal(t, "/health", ghatdlogger.RequestPath(&http.Request{URL: &url.URL{Path: "/health"}}))
}

func TestSafeValueKeepsOperationalFieldsAndRedactsPayloads(t *testing.T) {
	createdAt := time.Date(2026, 5, 27, 12, 30, 0, 0, time.UTC)
	value := struct {
		UserID    string
		Email     string
		Token     string
		Payload   map[string]string
		Page      int
		Enabled   bool
		CreatedAt time.Time
	}{
		UserID:    "user_123",
		Email:     "person@example.com",
		Token:     "secret-token",
		Payload:   map[string]string{"safe-key": "raw-value"},
		Page:      2,
		Enabled:   true,
		CreatedAt: createdAt,
	}

	safeValue, ok := ghatdlogger.SafeValue(value).(map[string]any)

	assert.True(t, ok)
	assert.Equal(t, "user_123", safeValue["user-id"])
	assert.Equal(t, int64(2), safeValue["page"])
	assert.Equal(t, true, safeValue["enabled"])
	assert.Equal(t, "2026-05-27T12:30:00Z", safeValue["created-at"])
	assert.Equal(t, true, safeValue["email-present"])
	assert.Equal(t, true, safeValue["token-present"])
	assert.Equal(t, true, safeValue["payload-present"])
	assert.Equal(t, 1, safeValue["payload-count"])
	assert.NotContains(t, safeValue, "email")
	assert.NotContains(t, safeValue, "token")
	assert.NotContains(t, safeValue, "payload")
}
