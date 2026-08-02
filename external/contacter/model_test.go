package contacter_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ooaklee/ghatd/external/contacter"
)

func TestCommsTypeStatsPreservesDefaultsAndCustomTypes(t *testing.T) {
	stats := contacter.CommsTypeStats{Feedback: 2}
	stats.Set(contacter.CommsType("service-question"), 3)

	payload, err := json.Marshal(stats)
	require.NoError(t, err)

	var counts map[string]int64
	require.NoError(t, json.Unmarshal(payload, &counts))
	assert.Equal(t, int64(2), counts["feedback"])
	assert.Equal(t, int64(3), counts["service_question"])

	var decoded contacter.CommsTypeStats
	require.NoError(t, json.Unmarshal(payload, &decoded))
	assert.Equal(t, int64(2), decoded.Feedback)
	assert.Equal(t, int64(3), decoded.Count(contacter.CommsType("service-question")))
}
