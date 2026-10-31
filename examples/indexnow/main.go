// The IndexNow example exercises the real service against a local mock engine.
// It never sends notifications to a public search engine.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/ooaklee/ghatd/external/indexnow"
)

const (
	demoKey      = "local-demo-indexnow-key-2026"
	demoSite     = "https://example.com"
	historyLimit = 200
)

//go:embed index.html
var page []byte

// submission is a local review record of a service invocation. The receipt is
// the service's own response; this example does not reinterpret it as indexing.
type submission struct {
	At         time.Time `json:"at"`
	Mode       string    `json:"mode"`
	URLs       []string  `json:"urls"`
	Simulation string    `json:"simulation"`
	Receipt    any       `json:"receipt,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// engineRequest records the protocol fields received by the local mock. The
// fixed demonstration key is omitted because it adds no useful audit context.
type engineRequest struct {
	At          time.Time `json:"at"`
	Method      string    `json:"method"`
	Host        string    `json:"host"`
	URLs        []string  `json:"urls"`
	KeyLocation string    `json:"key_location"`
	Status      int       `json:"status"`
}

type demo struct {
	service      *indexnow.Service
	mu           sync.Mutex
	submissions  []submission
	requests     []engineRequest
	attempts     map[string]indexnow.Attempt
	completedIDs []string
}

type simulationKey struct{}

// localTransport confines every outbound request to this example's mock engine.
// A request-scoped simulation avoids races between concurrent browser requests.
type localTransport struct {
	engineHost string
	transport  *http.Transport
}

func (t *localTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "http" || request.URL.Host != t.engineHost {
		return nil, errors.New("demo blocks requests outside its local mock engine")
	}
	mode, _ := request.Context().Value(simulationKey{}).(string)
	if mode == "network" {
		return nil, errors.New("simulated local transport failure")
	}
	clone := request.Clone(request.Context())
	clone.Header.Set("X-Demo-Response", mode)
	return t.transport.RoundTrip(clone)
}

// newDemo constructs a private engine and the production service. The returned
// cleanup closes both the engine listener and the transport's idle connections.
func newDemo() (*demo, func(), error) {
	d := &demo{attempts: make(map[string]indexnow.Attempt)}
	engine := httptest.NewServer(http.HandlerFunc(d.serveEngine))
	engineURL, err := url.Parse(engine.URL)
	if err != nil {
		engine.Close()
		return nil, nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	cleanup := func() {
		transport.CloseIdleConnections()
		engine.Close()
	}
	client := &http.Client{
		Transport: &localTransport{engineHost: engineURL.Host, transport: transport},
		Timeout:   5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("demo does not follow redirects")
		},
	}
	d.service, err = indexnow.NewService(demoKey, indexnow.Config{
		SiteURL:    demoSite,
		Endpoint:   engine.URL + "/indexnow",
		HTTPClient: client,
		Repository: d,
	})
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return d, cleanup, nil
}

func (d *demo) handler() http.Handler {
	router := mux.NewRouter()
	indexnow.NewHandler(d.service).RegisterRoutes(router)
	router.HandleFunc("/", func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'")
		_, _ = w.Write(page)
	}).Methods(http.MethodGet)
	router.HandleFunc("/api/submit", d.submit).Methods(http.MethodPost)
	router.HandleFunc("/api/history", d.history).Methods(http.MethodGet)
	router.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}).Methods(http.MethodGet)
	return router
}

func (d *demo) submit(w http.ResponseWriter, request *http.Request) {
	// Browser-origin checks keep another site's script from driving this local
	// demonstration. Requests without Origin remain usable with curl.
	if origin := request.Header.Get("Origin"); origin != "" && origin != "http://"+request.Host {
		http.Error(w, "local origin required", http.StatusForbidden)
		return
	}
	if contentType := request.Header.Get("Content-Type"); contentType != "application/json" && !strings.HasPrefix(contentType, "application/json;") {
		http.Error(w, "application/json required", http.StatusUnsupportedMediaType)
		return
	}
	var input struct {
		Mode       string   `json:"mode"`
		URLs       []string `json:"urls"`
		Simulation string   `json:"simulation"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		http.Error(w, "invalid request JSON", http.StatusBadRequest)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		http.Error(w, "one JSON object required", http.StatusBadRequest)
		return
	}
	if input.Mode != "single" && input.Mode != "batch" {
		http.Error(w, "mode must be single or batch", http.StatusBadRequest)
		return
	}
	if input.Mode == "single" && len(input.URLs) != 1 {
		http.Error(w, "single mode requires one URL", http.StatusBadRequest)
		return
	}
	if !validSimulation(input.Simulation) {
		http.Error(w, "unsupported response simulation", http.StatusBadRequest)
		return
	}
	ctx := context.WithValue(request.Context(), simulationKey{}, input.Simulation)
	entry := submission{At: time.Now().UTC(), Mode: input.Mode, URLs: input.URLs, Simulation: input.Simulation}
	var err error
	if input.Mode == "single" {
		entry.Receipt, err = d.service.SubmitURL(ctx, input.URLs[0])
	} else {
		entry.Receipt, err = d.service.SubmitURLs(ctx, &indexnow.SubmitURLsRequest{URLs: input.URLs})
	}
	if err != nil {
		entry.Error = err.Error()
	}
	d.mu.Lock()
	d.submissions = appendBounded(d.submissions, entry)
	d.mu.Unlock()
	writeJSON(w, entry)
}

func validSimulation(mode string) bool {
	switch mode {
	case "200", "202", "400", "403", "422", "429", "500", "network":
		return true
	default:
		return false
	}
}

// RecordAttempt implements the package's audit port using volatile example
// storage. It copies the URL slice so callers retain ownership of their values.
func (d *demo) RecordAttempt(ctx context.Context, attempt *indexnow.Attempt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.attempts[attempt.ID]; exists {
		return errors.New("demo attempt already exists")
	}
	copy := *attempt
	copy.URLs = append([]string(nil), attempt.URLs...)
	d.attempts[attempt.ID] = copy
	return nil
}

// FinishAttempt retains the most recent completed audit records. Pending
// records stay present until completion; a process restart clears all records.
func (d *demo) FinishAttempt(ctx context.Context, attempt *indexnow.Attempt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	stored, exists := d.attempts[attempt.ID]
	if !exists {
		return errors.New("demo attempt was not recorded")
	}
	copy := *attempt
	copy.URLs = append([]string(nil), attempt.URLs...)
	d.attempts[attempt.ID] = copy
	if stored.FinishedAt.IsZero() && !attempt.FinishedAt.IsZero() {
		d.completedIDs = append(d.completedIDs, attempt.ID)
		if len(d.completedIDs) > historyLimit {
			delete(d.attempts, d.completedIDs[0])
			d.completedIDs = d.completedIDs[1:]
		}
	}
	return nil
}

// serveEngine captures a protocol request and returns the explicitly selected
// response. It is a simulator, not an independent implementation of validation.
func (d *demo) serveEngine(w http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/indexnow" {
		http.NotFound(w, request)
		return
	}
	entry := engineRequest{At: time.Now().UTC(), Method: request.Method}
	switch request.Method {
	case http.MethodGet:
		entry.URLs = []string{request.URL.Query().Get("url")}
		entry.KeyLocation = request.URL.Query().Get("keyLocation")
	case http.MethodPost:
		var payload struct {
			Host        string   `json:"host"`
			URLs        []string `json:"urlList"`
			KeyLocation string   `json:"keyLocation"`
		}
		if err := json.NewDecoder(io.LimitReader(request.Body, 4<<20)).Decode(&payload); err != nil {
			http.Error(w, "invalid mock request JSON", http.StatusBadRequest)
			return
		}
		entry.Host, entry.URLs, entry.KeyLocation = payload.Host, payload.URLs, payload.KeyLocation
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	entry.Status, _ = strconv.Atoi(request.Header.Get("X-Demo-Response"))
	if entry.Status == 0 {
		entry.Status = http.StatusOK
	}
	d.mu.Lock()
	d.requests = appendBounded(d.requests, entry)
	d.mu.Unlock()
	if entry.Status == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", "30")
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(entry.Status)
	_, _ = fmt.Fprintln(w, http.StatusText(entry.Status))
}

func (d *demo) history(w http.ResponseWriter, _ *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	attempts := make([]indexnow.Attempt, 0, len(d.attempts))
	for _, attempt := range d.attempts {
		attempts = append(attempts, attempt)
	}
	sort.Slice(attempts, func(i, j int) bool {
		if attempts[i].StartedAt.Equal(attempts[j].StartedAt) {
			return attempts[i].ID < attempts[j].ID
		}
		return attempts[i].StartedAt.Before(attempts[j].StartedAt)
	})
	writeJSON(w, struct {
		Submissions    []submission       `json:"submissions"`
		EngineRequests []engineRequest    `json:"engine_requests"`
		AuditAttempts  []indexnow.Attempt `json:"audit_attempts"`
	}{Submissions: d.submissions, EngineRequests: d.requests, AuditAttempts: attempts})
}

// appendBounded retains the most recent local records without unbounded growth.
func appendBounded[T any](entries []T, entry T) []T {
	if len(entries) >= historyLimit {
		copy(entries, entries[len(entries)-historyLimit+1:])
		entries = entries[:historyLimit-1]
	}
	return append(entries, entry)
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(value)
}

// loopbackAddress rejects wildcard and hostname listeners so the example's
// unauthenticated review controls are only exposed on an explicit loopback IP.
func loopbackAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return errors.New("listen address must use an explicit loopback IP")
	}
	return nil
}

func run(ctx context.Context, address string) error {
	if err := loopbackAddress(address); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return err
	}
	defer listener.Close()
	d, cleanup, err := newDemo()
	if err != nil {
		return err
	}
	defer cleanup()
	server := &http.Server{
		Handler:           d.handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	defer server.Close()
	completed := make(chan error, 1)
	go func() { completed <- server.Serve(listener) }()
	fmt.Printf("IndexNow local-only demo ready: http://%s\nNo public search engine notifications are sent. Canonical fixture: %s\n", listener.Addr(), demoSite)
	select {
	case err := <-completed:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownCtx)
	}
}

func main() {
	listen := flag.String("listen", "127.0.0.1:8097", "Explicit loopback IP and port for the local demo")
	flag.Parse()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, *listen); err != nil {
		fmt.Fprintln(os.Stderr, "IndexNow demo:", err)
		os.Exit(1)
	}
}
