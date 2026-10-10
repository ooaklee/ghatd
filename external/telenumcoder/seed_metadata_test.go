package telenumcoder

import (
	"strconv"
	"testing"

	"github.com/nyaruka/phonenumbers"
	"github.com/stretchr/testify/require"
)

// Seed definitions retain their original snapshot, while validation uses the
// module's pinned library. Detect drift explicitly rather than silently
// refreshing seeds or discovering a mismatch halfway through setup.
func TestSeedRegionsMatchRuntimeMetadata(t *testing.T) {
	rows, err := Seeds()
	require.NoError(t, err)
	require.Len(t, rows, 245)
	supported := phonenumbers.GetSupportedRegions()
	require.Len(t, supported, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		t.Run(row.Code, func(t *testing.T) {
			require.False(t, seen[row.Code], "seed region is unique")
			seen[row.Code] = true
			require.True(t, supported[row.Code], "seed region remains supported")
			require.Equal(t, "+"+strconv.Itoa(phonenumbers.GetCountryCodeForRegion(row.Code)), row.CallingCode)
		})
	}
}
