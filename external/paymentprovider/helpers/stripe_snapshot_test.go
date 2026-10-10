package helpers

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Audit disposition: retained proof is bounded private file input, never a
// permissive provider payload or a public diagnostic on failure.
func TestReadRetainedStripeRefundSnapshotFile(t *testing.T) {
	cases := []struct {
		name, body string
		mode       os.FileMode
		ok         bool
	}{{"exact_refund", `{"type":"charge.refunded","data":{"object":{"object":"charge"}}}`, 0600, true}, {"malformed", `{`, 0600, false}, {"other_event", `{"type":"invoice.paid","data":{"object":{"object":"charge"}}}`, 0600, false}, {"other_object", `{"type":"charge.refunded","data":{"object":{"object":"invoice"}}}`, 0600, false}, {"empty", "", 0600, false}, {"public_file", `{}`, 0644, false}, {"oversized", string(bytes.Repeat([]byte("a"), int(stripeRetainedSnapshotLimit+1))), 0600, false}, {"missing", "", 0600, false}, {"directory", "", 0700, false}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "snapshot.json")
			if tc.name == "directory" {
				require.NoError(t, os.Mkdir(path, tc.mode))
			} else if tc.name != "missing" {
				require.NoError(t, os.WriteFile(path, []byte(tc.body), tc.mode))
				require.NoError(t, os.Chmod(path, tc.mode))
			}
			body, err := ReadRetainedStripeRefundSnapshotFile(path)
			if tc.ok {
				require.NoError(t, err)
				require.Equal(t, tc.body, string(body))
			} else {
				require.Error(t, err)
				require.Nil(t, body)
				if tc.name == "missing" {
					require.ErrorIs(t, err, os.ErrNotExist)
					require.NotErrorIs(t, err, ErrStripeRetainedSnapshotInvalid)
				} else {
					require.ErrorIs(t, err, ErrStripeRetainedSnapshotInvalid)
				}
			}
		})
	}
}
