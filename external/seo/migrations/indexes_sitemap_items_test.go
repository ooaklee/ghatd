package migrations

import (
	"context"
	"testing"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/event"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"go.mongodb.org/mongo-driver/v2/x/mongo/driver/drivertest"
)

// TestSitemapItemIndexHelpersPropagateContext verifies sitemap index helpers forward their caller context.
func TestSitemapItemIndexHelpersPropagateContext(t *testing.T) {
	tests := []struct {
		name string
		run  func(context.Context, *mongo.Database) error
	}{
		{name: "up", run: InitSitemapItemIndexesUpWithContext},
		{name: "down", run: InitSitemapItemIndexesDownWithContext},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			type contextKey struct{}
			const contextValue = "migration-trace"
			var commandContext context.Context
			monitor := &event.CommandMonitor{
				Started: func(ctx context.Context, _ *event.CommandStartedEvent) {
					commandContext = ctx
				},
			}

			clientOptions := options.Client().SetMonitor(monitor)
			clientOptions.Deployment = drivertest.NewMockDeployment(
				bson.D{{Key: "ok", Value: 1}},
			)
			client, err := mongo.Connect(clientOptions)
			if err != nil {
				t.Fatalf("mongo.Connect() error = %v", err)
			}
			t.Cleanup(func() { _ = client.Disconnect(context.Background()) })

			ctx := context.WithValue(context.Background(), contextKey{}, contextValue)
			if err := test.run(ctx, client.Database("test")); err != nil {
				t.Fatalf("index helper error = %v", err)
			}
			if commandContext == nil {
				t.Fatal("MongoDB command monitor did not receive a context")
			}
			if got := commandContext.Value(contextKey{}); got != contextValue {
				t.Fatalf("command context value = %v, want %q", got, contextValue)
			}
		})
	}
}
