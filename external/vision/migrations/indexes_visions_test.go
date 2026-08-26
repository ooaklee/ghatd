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

// TestVisionIndexHelpersPropagateContext verifies every vision index helper forwards its caller context.
func TestVisionIndexHelpersPropagateContext(t *testing.T) {
	tests := []struct {
		name         string
		commandCount int
		run          func(context.Context, *mongo.Database) error
	}{
		{name: "up", commandCount: 1, run: InitVisionIndexesUpWithContext},
		{name: "down", commandCount: 4, run: InitVisionIndexesDownWithContext},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			type contextKey struct{}
			const contextValue = "migration-trace"
			seenContexts := make([]context.Context, 0, test.commandCount)
			monitor := &event.CommandMonitor{
				Started: func(ctx context.Context, _ *event.CommandStartedEvent) {
					seenContexts = append(seenContexts, ctx)
				},
			}

			responses := make([]bson.D, test.commandCount)
			for responseIndex := range responses {
				responses[responseIndex] = bson.D{{Key: "ok", Value: 1}}
			}
			clientOptions := options.Client().SetMonitor(monitor)
			clientOptions.Deployment = drivertest.NewMockDeployment(responses...)
			client, err := mongo.Connect(clientOptions)
			if err != nil {
				t.Fatalf("mongo.Connect() error = %v", err)
			}
			t.Cleanup(func() { _ = client.Disconnect(context.Background()) })

			ctx := context.WithValue(context.Background(), contextKey{}, contextValue)
			if err := test.run(ctx, client.Database("test")); err != nil {
				t.Fatalf("index helper error = %v", err)
			}
			if len(seenContexts) != test.commandCount {
				t.Fatalf("observed %d command contexts, want %d", len(seenContexts), test.commandCount)
			}
			for _, commandContext := range seenContexts {
				if got := commandContext.Value(contextKey{}); got != contextValue {
					t.Fatalf("command context value = %v, want %q", got, contextValue)
				}
			}
		})
	}
}
