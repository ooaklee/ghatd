package repositoryhelpers

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithTopology(t *testing.T) {
	for _, tc := range []struct {
		name, uri, replica, wantReplica string
		direct, wantDirect, malformed   bool
	}{
		{"preserves other options", "mongodb://localhost:27049/?appName=fixture&retryWrites=true", "rs-test", "rs-test", true, true, false},
		{"unconfigured URI unchanged", "mongodb://localhost:27017", "", "", false, false, false},
		{"adds required slash", "mongodb://localhost:27049", "rs-test", "rs-test", true, true, false},
		{"retains existing topology", "mongodb://localhost:27017/?replicaSet=existing&directConnection=true", "", "existing", false, true, false},
		{"explicit values override URI", "mongodb://localhost:27017/?replicaSet=existing&directConnection=false", "chosen", "chosen", true, true, false},
		{"direct connection alone", "mongodb://localhost:27017", "", "", true, true, false},
		{"replica set alone", "mongodb://localhost:27017", "chosen", "chosen", false, false, false},
		{"malformed URI left for driver", "%invalid", "chosen", "", true, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig(tc.uri, "fixture")
			WithTopology(tc.replica, tc.direct)(cfg)
			if tc.malformed {
				require.Equal(t, tc.uri, cfg.ConnectionString)
				require.Error(t, cfg.BuildClientOptions().Validate())
				return
			}
			client := cfg.BuildClientOptions()
			require.NoError(t, client.Validate())
			if tc.wantReplica == "" {
				require.Nil(t, client.ReplicaSet)
			} else {
				require.NotNil(t, client.ReplicaSet)
				require.Equal(t, tc.wantReplica, *client.ReplicaSet)
			}
			if tc.wantDirect {
				require.NotNil(t, client.Direct)
				require.True(t, *client.Direct)
			} else {
				require.Nil(t, client.Direct)
			}
			if tc.name == "preserves other options" {
				require.Equal(t, "fixture", *client.AppName)
				require.True(t, *client.RetryWrites)
			}
			if tc.name == "unconfigured URI unchanged" {
				require.Equal(t, tc.uri, cfg.ConnectionString)
			}
			parsed, err := url.Parse(cfg.ConnectionString)
			require.NoError(t, err)
			if parsed.RawQuery != "" {
				require.NotEmpty(t, parsed.Path)
			}
		})
	}
	// No configuration exists to mutate, and this must not panic.
	WithTopology("chosen", true)(nil)
}
