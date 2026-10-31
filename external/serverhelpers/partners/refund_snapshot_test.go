package partners

import (
	"github.com/ooaklee/ghatd/external/billing"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestPartnersRefundSnapshotClassification(t *testing.T) {
	cases := []struct {
		name, body string
		want       error
	}{{"exact_bytes", " {\"type\":\"charge.refunded\",\"data\":{\"object\":{\"object\":\"charge\"}}} ", nil}, {"invalid_shape", "{}", billing.ErrRevenueInvalid}, {"missing_file", "", os.ErrNotExist}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "snapshot.json")
			if tc.name != "missing_file" {
				require.NoError(t, os.WriteFile(path, []byte(tc.body), 0600))
			}
			body, err := ReadRefundSnapshot(path)
			if tc.want == nil {
				require.NoError(t, err)
				require.Equal(t, []byte(tc.body), body)
			} else {
				require.ErrorIs(t, err, tc.want)
				require.Nil(t, body)
				if tc.name == "missing_file" {
					require.NotErrorIs(t, err, billing.ErrRevenueInvalid)
				}
			}
		})
	}
}
