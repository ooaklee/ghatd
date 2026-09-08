package observability

import (
	"context"
	"errors"
	"strings"

	"go.mongodb.org/mongo-driver/v2/event"
	"go.opentelemetry.io/contrib/instrumentation/go.mongodb.org/mongo-driver/v2/mongo/otelmongo"
)

var telemetrySafeMongoError = errors.New("MongoDB command failed")

// NewMongoCommandMonitor creates an OpenTelemetry command monitor for the
// MongoDB v2 driver. MongoDB command text is always disabled because commands
// can contain query values and documents with sensitive application data.
func NewMongoCommandMonitor(options ...otelmongo.Option) *event.CommandMonitor {
	options = append(options,
		otelmongo.WithCommandAttributeDisabled(true),
		otelmongo.WithSpanNameFormatter(mongoSpanName),
	)
	monitor := otelmongo.NewMonitor(options...)

	// otelmongo uses the driver's error text as the span status description.
	// Driver errors can echo query values or document fragments, so pass a
	// data-free copy to telemetry. This does not alter the error returned by the
	// MongoDB operation itself.
	failed := monitor.Failed
	monitor.Failed = func(ctx context.Context, event *event.CommandFailedEvent) {
		if event == nil {
			return
		}
		sanitisedEvent := *event
		sanitisedEvent.Failure = telemetrySafeMongoError
		failed(ctx, &sanitisedEvent)
	}

	return monitor
}

// mongoSpanName returns a bounded operation-only name that cannot expose
// command documents through a caller-supplied formatter.
func mongoSpanName(command *event.CommandStartedEvent) string {
	if command == nil {
		return "mongodb.command"
	}

	operation := strings.ToLower(strings.TrimSpace(command.CommandName))
	if operation == "" || len(operation) > 64 {
		return "mongodb.command"
	}
	for _, character := range operation {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') {
			return "mongodb.command"
		}
	}

	return "mongodb." + operation
}
