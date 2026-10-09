package partnerruntime

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/partnermanager"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestRuntimePreparationRequiresLiveContext(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		want        error
	}{
		{"nil_runtime", "runtime", partnermanager.ErrUnavailable},
		{"nil_context", "context", partnermanager.ErrUnavailable},
		{"unbound_runtime", "unbound", partnermanager.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			r := &Runtime{}
			switch tc.state {
			case "runtime":
				r = nil
			case "context":
				ctx = nil
			}
			require.ErrorIs(t, r.Prepare(ctx), tc.want)
		})
	}
}

func TestRuntimeExplicitPreparationAndHostKeyPurposes(t *testing.T) {
	for _, tc := range []struct {
		name     string
		keyCount int
	}{
		{"one_other_host_purpose", 1}, {"two_other_host_purposes", 2}, {"three_other_host_purposes", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			uri := os.Getenv("GHATD_TEST_MONGO_URI")
			if uri == "" {
				t.Skip("set GHATD_TEST_MONGO_URI to an isolated replica set")
			}
			ctx, cancel := context.WithTimeout(t.Context(), 40*time.Second)
			defer cancel()
			client, err := mongo.Connect(options.Client().ApplyURI(uri))
			require.NoError(t, err)
			db := client.Database(fmt.Sprintf("ghatd_partner_prepare_%d", time.Now().UnixNano()))
			t.Cleanup(func() {
				cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
				defer done()
				require.NoError(t, db.Drop(cleanup))
				require.NoError(t, client.Disconnect(cleanup))
			})
			cfg, deps := sharedPartnersCompositionFixture()
			cfg.ReservedKeys = nil
			for i := 0; i < tc.keyCount; i++ {
				cfg.ReservedKeys = append(cfg.ReservedKeys, bytes.Repeat([]byte{byte(i + 1)}, 32))
			}
			r, err := NewRuntime(db, cfg, deps)
			require.NoError(t, err)
			collections, err := db.ListCollectionNames(ctx, bson.M{})
			require.NoError(t, err)
			require.Empty(t, collections, "construction must not prepare or seed storage")
			require.NoError(t, r.Prepare(ctx))
			collections, err = db.ListCollectionNames(ctx, bson.M{})
			require.NoError(t, err)
			require.NotEmpty(t, collections)
			cancelled, stop := context.WithCancel(ctx)
			stop()
			require.ErrorIs(t, r.Prepare(cancelled), context.Canceled)
			count, err := db.Collection("ghatd_owned_records").CountDocuments(ctx, bson.M{})
			require.NoError(t, err)
			require.Zero(t, count, "readiness must create no participants, grants or finances")
		})
	}
}
