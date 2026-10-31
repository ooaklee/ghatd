// The serverhelpers example assembles an offline handler and owns its lifecycle.
package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"time"

	"github.com/ooaklee/ghatd/external/serverhelpers"
	"github.com/ooaklee/ghatd/external/serverhelpers/publication"
)

// config is resolved by the application before starting resources.
type config struct {
	Origin         string
	EnableIndexNow bool
}

type exampleStore struct{ closed bool }

func (s *exampleStore) Close(context.Context) error { s.closed = true; return nil }

type exampleWorker struct {
	store   *exampleStore
	stopped bool
}

func (w *exampleWorker) Drain(context.Context) error {
	if w.store.closed {
		return fmt.Errorf("store closed before worker")
	}
	w.stopped = true
	return nil
}

func run(ctx context.Context, output io.Writer, cfg config) (err error) {
	lifecycle := serverhelpers.NewLifecycle(serverhelpers.LifecycleConfig{Timeout: time.Second, Telemetry: func(context.Context) error { _, err := fmt.Fprintln(output, "telemetry stopped"); return err }})
	defer func() {
		if closeErr := lifecycle.Close(ctx); err == nil {
			err = closeErr
		}
	}()
	store := &exampleStore{}
	worker := &exampleWorker{store: store}
	if err = lifecycle.AddResource(store.Close, worker.Drain); err != nil {
		return err
	}
	// Disabled configuration constructs no client and makes no network request.
	if _, err = publication.BuildIndexNow(publication.IndexNowConfig{Enabled: cfg.EnableIndexNow, SiteURL: cfg.Origin}); err != nil {
		return err
	}
	filter, err := publication.NewSitemapFilter(publication.SitemapFilterConfig{Origin: cfg.Origin})
	if err != nil {
		return err
	}
	allowed, err := filter.AllowIndexNowURL(ctx, "/pricing")
	if err != nil {
		return err
	}
	router := http.NewServeMux()
	router.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	_, err = fmt.Fprintf(output, "health=%d public-pricing=%t\n", response.Code, allowed)
	return err
}
func main() {
	if err := run(context.Background(), os.Stdout, config{Origin: "https://example.test"}); err != nil {
		panic(err)
	}
}
