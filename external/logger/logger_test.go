package logger_test

import (
	"context"
	"testing"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestAcquireFrom(t *testing.T) {
	testLogger := zap.NewExample()

	ctx := logger.TransitWith(context.Background(), testLogger)

	retrievedLogger := logger.AcquireFrom(ctx)

	assert.Samef(t, testLogger, retrievedLogger, "Logger are not the same")
}
