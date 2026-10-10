package internationalisationmanagerhelper

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ooaklee/ghatd/external/catalogue"
	i18n "github.com/ooaklee/ghatd/external/internationalisationmanager"
	"github.com/ooaklee/ghatd/external/telenumcoder"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func TestInitialiseNativeGuardsBeforeIO(t *testing.T) {
	for _, tc := range []struct {
		name string
		want error
	}{
		{"disabled", nil}, {"disabled_ignores_unusable_dependencies", nil},
		{"nil_context", catalogue.ErrUnavailable}, {"nil_database", catalogue.ErrUnavailable},
		{"cancelled", context.Canceled}, {"invalid_endpoint", telenumcoder.ErrLookupUnavailable},
		{"invalid_timeout", telenumcoder.ErrLookupUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var callCtx context.Context = ctx
			// This database is deliberately inert: crossing the pre-I/O boundary
			// on any of these cases would panic instead of silently succeeding.
			db := &mongo.Database{}
			cfg := Config{Enabled: true}
			switch tc.name {
			case "disabled":
				cfg.Enabled = false
			case "disabled_ignores_unusable_dependencies":
				cfg.Enabled = false
				cfg.Reachability.Endpoint = "http://invalid.example"
				db, callCtx = nil, nil
			case "nil_context":
				callCtx = nil
			case "nil_database":
				db = nil
			case "cancelled":
				cancel()
			case "invalid_endpoint":
				cfg.Reachability.Endpoint = "http://invalid.example"
			case "invalid_timeout":
				cfg.Reachability = telenumcoder.HTTPConfig{Endpoint: "https://lookup.example", Timeout: time.Hour}
			}
			service, err := InitialiseNative(callCtx, db, cfg)
			if tc.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.want)
			}
			require.Nil(t, service)
		})
	}
}

func TestInitialiseNativeCataloguePreservation(t *testing.T) {
	uri := os.Getenv("GHATD_TEST_MONGO_URI")
	if uri == "" {
		t.Skip("set GHATD_TEST_MONGO_URI to an isolated replica set")
	}
	for _, mode := range []string{"hidden", "disabled", "deleted", "invalid_configuration"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
			defer cancel()
			client, err := mongo.Connect(options.Client().ApplyURI(uri))
			require.NoError(t, err)
			db := client.Database(fmt.Sprintf("hostapp_i18n_setup_%d", time.Now().UnixNano()))
			t.Cleanup(func() {
				cleanup, done := context.WithTimeout(context.Background(), 10*time.Second)
				defer done()
				require.NoError(t, db.Drop(cleanup))
				require.NoError(t, client.Disconnect(cleanup))
			})
			calls := 0
			provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				_, _ = w.Write([]byte(`{"state":"reachable"}`))
			}))
			t.Cleanup(provider.Close)
			cfg := Config{Enabled: true, Reachability: telenumcoder.HTTPConfig{Endpoint: provider.URL, Client: provider.Client()}}
			if mode == "invalid_configuration" {
				cfg.Reachability.Endpoint = "http://invalid.example"
				service, err := InitialiseNative(ctx, db, cfg)
				require.ErrorIs(t, err, telenumcoder.ErrLookupUnavailable)
				require.Nil(t, service)
				names, err := db.ListCollectionNames(ctx, bson.D{})
				require.NoError(t, err)
				require.Empty(t, names)
				require.Zero(t, calls)
				return
			}
			service, err := InitialiseNative(ctx, db, cfg)
			require.NoError(t, err)
			require.Zero(t, calls, "setup must not contact the optional provider")
			for _, tc := range []struct {
				kind i18n.Kind
				want int
			}{{i18n.Currencies, 165}, {i18n.PhoneCodes, 245}, {i18n.Timezones, 519}, {i18n.Flags, 270}} {
				t.Run(string(tc.kind), func(t *testing.T) {
					query := i18n.ListQuery{Limit: 200}
					seen := map[string]bool{}
					for {
						page, err := service.List(ctx, tc.kind, false, query)
						require.NoError(t, err)
						require.LessOrEqual(t, len(page.Records), 200)
						for _, row := range page.Records {
							require.False(t, seen[row.Code])
							seen[row.Code] = true
							require.Empty(t, row.CreatedBy)
						}
						if page.Cursor == "" {
							break
						}
						query.Cursor = page.Cursor
					}
					require.Len(t, seen, tc.want)
				})
			}
			before, err := service.Get(ctx, i18n.Currencies, "GBP", true)
			require.NoError(t, err)
			var changed i18n.RecordView
			if mode == "deleted" {
				changed, err = service.Remove(ctx, i18n.Currencies, "GBP", before.Revision, "current-admin")
			} else {
				value := mode == "hidden"
				mutation := i18n.Mutation{Enabled: &value}
				if mode == "hidden" {
					mutation = i18n.Mutation{Hidden: &value}
				}
				changed, err = service.Update(ctx, i18n.Currencies, "GBP", mutation, before.Revision, "current-admin")
			}
			require.NoError(t, err)
			require.EqualValues(t, 2, changed.Revision)
			require.Equal(t, "current-admin", changed.UpdatedBy)
			retained, err := service.Get(ctx, i18n.Currencies, "GBP", true)
			require.NoError(t, err)
			require.Equal(t, changed.Revision, retained.Revision)
			require.Equal(t, changed.UpdatedBy, retained.UpdatedBy)
			again, err := InitialiseNative(ctx, db, cfg)
			require.NoError(t, err)
			kept, err := again.Get(ctx, i18n.Currencies, "GBP", true)
			require.NoError(t, err)
			require.Equal(t, retained, kept)
			require.ErrorIs(t, again.ValidateCurrency(ctx, "GBP"), catalogue.ErrNotSelectable)
			require.Zero(t, calls)
			_, err = again.NormalisePhone(ctx, "020 7946 0018", "GB")
			require.NoError(t, err)
			require.Zero(t, calls, "normalisation must not contact reachability")
			result, err := again.CheckPhone(ctx, "020 7946 0018", "GB")
			require.NoError(t, err)
			require.Equal(t, "reachable", result.State)
			require.Equal(t, 1, calls, "only explicit reachability invokes the provider")
		})
	}
}
