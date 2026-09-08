package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/ooaklee/ghatd/external/logger"
	"github.com/ooaklee/ghatd/external/observability"
	"github.com/ooaklee/ghatd/external/observability/otelhttp"
	"github.com/ooaklee/ghatd/external/router"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
)

var (
	errQuotaExceeded    = errors.New("example quota exceeded")
	errDependencyFailed = errors.New("example dependency failed")
)

type workResponse struct {
	Status  string `json:"status"`
	Code    string `json:"code,omitempty"`
	TraceID string `json:"trace_id"`
}

type exampleService struct {
	operations    *observability.Operations
	classifier    *observability.ErrorClassifier
	client        *http.Client
	dependencyURL string
}

func newService(operations *observability.Operations, classifier *observability.ErrorClassifier, dependencyURL string) *exampleService {
	return &exampleService{
		operations: operations, classifier: classifier, dependencyURL: dependencyURL,
		client: observability.NewHTTPClient(http.DefaultTransport, 3*time.Second),
	}
}

func (service *exampleService) process(ctx context.Context, reject bool) (traceID string, err error) {
	ctx, operation := service.operations.Start(ctx, "process-work")
	defer operation.Finish(&err)
	traceID = trace.SpanContextFromContext(ctx).TraceID().String()
	err = service.perform(ctx, reject)
	classification := service.classifier.Classify(err)
	logger.AcquireFrom(ctx).Info("work completed",
		zap.String("operation", "process-work"),
		zap.String("outcome", string(classification.Outcome)))
	return traceID, err
}

func (service *exampleService) perform(ctx context.Context, reject bool) error {
	if reject {
		return errQuotaExceeded
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, service.dependencyURL, nil)
	if err != nil {
		return errors.Join(errDependencyFailed, err)
	}
	response, err := service.client.Do(request)
	if err != nil {
		return errors.Join(errDependencyFailed, err)
	}
	defer response.Body.Close()
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		return errors.Join(errDependencyFailed, err)
	}
	if response.StatusCode != http.StatusNoContent {
		return errDependencyFailed
	}
	return nil
}

func newHandler(runtime *observability.Runtime, service *exampleService) http.Handler {
	routes := router.NewRouter(nil, nil).GetRouter()
	routes.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }).Methods(http.MethodGet)
	routes.HandleFunc("/dependency", dependencyHandler).Methods(http.MethodGet)
	routes.HandleFunc("/api/v1/work", workHandler(service, false)).Methods(http.MethodGet)
	routes.HandleFunc("/api/v1/rejected", workHandler(service, true)).Methods(http.MethodGet)
	return otelhttp.Wrap("example-api", runtime.Logger(), routes)
}

func workHandler(service *exampleService, reject bool) http.HandlerFunc {
	return func(w http.ResponseWriter, request *http.Request) {
		traceID, err := service.process(request.Context(), reject)
		body := workResponse{Status: "ok", TraceID: traceID}
		status := http.StatusOK
		if err != nil {
			body.Status, body.Code = "error", service.classifier.Classify(err).Code
			status = http.StatusServiceUnavailable
			if errors.Is(err, errQuotaExceeded) {
				body.Status, status = "rejected", http.StatusTooManyRequests
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(body)
	}
}

func dependencyHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// The standalone worker owns its dependency listener and drains it before the
// Cobra adapter ends and flushes the command's telemetry.
func startLocalDependency(runtime *observability.Runtime) (string, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	routes := router.NewRouter(nil, nil).GetRouter()
	routes.HandleFunc("/dependency", dependencyHandler).Methods(http.MethodGet)
	server := &http.Server{
		Handler: otelhttp.Wrap("example-dependency", runtime.Logger(), routes), ReadHeaderTimeout: 5 * time.Second,
		BaseContext: func(net.Listener) context.Context { return context.WithoutCancel(runtime.Context()) },
	}
	completed := make(chan error, 1)
	go func() { completed <- server.Serve(listener) }()
	closeDependency := func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(runtime.Context()), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			_ = server.Close()
		}
		<-completed
	}
	return listenerURL(listener) + "/dependency", closeDependency, nil
}
