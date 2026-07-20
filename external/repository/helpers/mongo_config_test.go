package repositoryhelpers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithTimeoutsBuildsV2OperationTimeout(t *testing.T) {
	operationTimeout := 30 * time.Second
	config := DefaultConfig("mongodb://localhost:27017", "test")
	WithTimeouts(5*time.Second, 3*time.Second, operationTimeout)(config)

	clientOptions := config.BuildClientOptions()

	require.NotNil(t, clientOptions.Timeout)
	assert.Equal(t, operationTimeout, *clientOptions.Timeout)
}

func TestBuildClientOptionsMapsLegacySocketTimeout(t *testing.T) {
	legacyTimeout := 45 * time.Second
	config := DefaultConfig("mongodb://localhost:27017", "test")
	config.SocketTimeout = &legacyTimeout

	clientOptions := config.BuildClientOptions()

	require.NotNil(t, clientOptions.Timeout)
	assert.Equal(t, legacyTimeout, *clientOptions.Timeout)
}
