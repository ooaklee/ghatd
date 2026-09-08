package observability

import (
	"context"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

const (
	browserIntakeMaximumBytes = 64 * 1024
	browserIntakeMaximumSpans = 16
	browserIntakeScope        = "github.com/ooaklee/ghatd/browser"
)

// BrowserTraceIntakeConfig defines server-owned identity and admission limits.
// Transport settings are snapshotted from the standard trace OTLP environment.
// No browser resource, scope, instance ID, URL or exception payload is trusted.
type BrowserTraceIntakeConfig struct {
	ServiceName, Namespace, Environment, Version string
	AllowedOrigins, RouteGroups, APIGroups       []string
	MaxConcurrent, BatchesPerMinute, Burst       int
	Timeout                                      time.Duration
	MeterProvider                                metric.MeterProvider
}

// BrowserTraceIntake validates and rebuilds bounded browser spans before one
// synchronous OTLP export. Construct one owning intake per service process and
// mount its exact route outside authentication, inside whole-router telemetry.
type BrowserTraceIntake struct {
	origins, routes, apis map[string]struct{}
	resource              *resourcepb.Resource
	timeout               time.Duration
	maxConcurrent         int
	rate, burst           float64
	sender                *browserIntakeSender
	count                 metric.Int64Counter
	ctx                   context.Context
	cancel                context.CancelFunc

	mu        sync.Mutex
	closed    bool
	inflight  int
	tokens    float64
	lastToken time.Time
	done      chan struct{}
	doneOnce  sync.Once
	closeOnce sync.Once
}

// NewBrowserTraceIntake creates no SDK, global provider, listener or network
// connection. Zero limits select four concurrent forwards, 60 batches/minute,
// burst ten and a two-second handling deadline. Maximums are 32, 600, 128 and
// ten seconds respectively. The fixed body/span bounds are 64 KiB and 16 spans.
// Only the default/explicit otlp trace exporter is supported by this intake.
func NewBrowserTraceIntake(config BrowserTraceIntakeConfig) (*BrowserTraceIntake, error) {
	if config.MaxConcurrent == 0 {
		config.MaxConcurrent = 4
	}
	if config.BatchesPerMinute == 0 {
		config.BatchesPerMinute = 60
	}
	if config.Burst == 0 {
		config.Burst = 10
	}
	if config.Timeout == 0 {
		config.Timeout = 2 * time.Second
	}
	if config.MaxConcurrent < 1 || config.MaxConcurrent > 32 || config.BatchesPerMinute < 1 || config.BatchesPerMinute > 600 || config.Burst < 1 || config.Burst > 128 || config.Timeout < 0 || config.Timeout > 10*time.Second {
		return nil, errors.New("browser intake limits are outside their supported bounds")
	}
	if config.ServiceName == "" {
		return nil, errors.New("browser intake ServiceName is required")
	}
	resource := &resourcepb.Resource{}
	for _, item := range []struct{ key, value string }{{"service.name", config.ServiceName}, {"service.namespace", config.Namespace}, {"deployment.environment.name", config.Environment}, {"service.version", config.Version}} {
		if len(item.value) > 256 || !configurationTextSafe(item.value) {
			return nil, errors.New("browser intake resource configuration is invalid")
		}
		if item.value != "" {
			resource.Attributes = append(resource.Attributes, browserStringAttribute(item.key, item.value))
		}
	}
	origins := make(map[string]struct{}, len(config.AllowedOrigins))
	if len(config.AllowedOrigins) == 0 || len(config.AllowedOrigins) > 32 {
		return nil, errors.New("browser intake requires one through 32 explicit origins")
	}
	for _, origin := range config.AllowedOrigins {
		if !browserCanonicalOrigin(origin) {
			return nil, errors.New("browser intake origin configuration is invalid")
		}
		origins[origin] = struct{}{}
	}
	routes, err := browserGroupSet(config.RouteGroups)
	if err != nil {
		return nil, err
	}
	apis, err := browserGroupSet(config.APIGroups)
	if err != nil {
		return nil, err
	}
	settings, err := browserTraceTransportConfiguration()
	if err != nil {
		return nil, err
	}
	sender, err := newBrowserIntakeSender(settings)
	if err != nil {
		return nil, err
	}
	provider := config.MeterProvider
	if provider == nil {
		provider = otel.GetMeterProvider()
	}
	count, err := provider.Meter(browserIntakeScope).Int64Counter("ghatd.browser.intake.batch.count", metric.WithDescription("Browser trace intake batch outcomes"))
	if err != nil {
		sender.close()
		return nil, errors.New("create browser intake counter failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &BrowserTraceIntake{origins: origins, routes: routes, apis: apis, resource: resource, timeout: config.Timeout, maxConcurrent: config.MaxConcurrent, rate: float64(config.BatchesPerMinute) / 60, burst: float64(config.Burst), tokens: float64(config.Burst), lastToken: time.Now(), sender: sender, count: count, ctx: ctx, cancel: cancel, done: make(chan struct{})}, nil
}

func browserTraceTransportConfiguration() (resolvedSignalConfiguration, error) {
	settings := resolvedSignalConfiguration{signal: "traces", exporter: "otlp"}
	if exporter := os.Getenv("OTEL_TRACES_EXPORTER"); exporter != "" && exporter != "otlp" {
		return settings, errors.New("browser intake requires OTEL_TRACES_EXPORTER=otlp")
	}
	report := ConfigurationReport{}
	item := SignalConfiguration{Signal: "traces"}
	report.inspectOTLP(os.Getenv, &item, &settings)
	for _, issue := range report.Issues {
		if issue.Severity == "error" {
			return settings, errors.New("invalid browser intake transport configuration: " + issue.Field + " (" + issue.Code + ")")
		}
	}
	return settings, nil
}

func browserCanonicalOrigin(origin string) bool {
	if len(origin) > 256 || !configurationTextSafe(origin) {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed == nil || parsed.Scheme != "http" && parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.Opaque != "" {
		return false
	}
	if strings.ToLower(parsed.Host) != parsed.Host || parsed.String() != origin || strings.ContainsAny(parsed.Host, "\\ %*") || strings.HasSuffix(parsed.Host, ":") {
		return false
	}
	hostname := parsed.Hostname()
	if address, err := netip.ParseAddr(hostname); err == nil {
		if address.Zone() != "" || address.String() != hostname || address.Is6() != strings.HasPrefix(parsed.Host, "[") {
			return false
		}
	} else {
		if len(hostname) > 253 || strings.Trim(hostname, "0123456789.") == "" || strings.ContainsAny(parsed.Host, "[]") {
			return false
		}
		for _, label := range strings.Split(hostname, ".") {
			if !browserHostLabelPattern.MatchString(label) {
				return false
			}
		}
	}
	if port := parsed.Port(); port != "" {
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 || strconv.Itoa(value) != port || parsed.Scheme == "http" && port == "80" || parsed.Scheme == "https" && port == "443" {
			return false
		}
	}
	return parsed.Hostname() != ""
}

var browserGroupPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
var browserHostLabelPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func browserGroupSet(values []string) (map[string]struct{}, error) {
	groups := map[string]struct{}{"other": {}}
	if len(values) > 32 {
		return nil, errors.New("browser intake group configuration exceeds 32 entries")
	}
	for _, value := range values {
		if !browserGroupPattern.MatchString(value) {
			return nil, errors.New("browser intake group configuration is invalid")
		}
		groups[value] = struct{}{}
	}
	if len(groups) > 32 {
		return nil, errors.New("browser intake groups must include other within the 32-entry bound")
	}
	return groups, nil
}

func browserStringAttribute(key, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: key, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}}}
}

func (intake *BrowserTraceIntake) admit(now time.Time) (bool, bool) {
	intake.mu.Lock()
	defer intake.mu.Unlock()
	if intake.closed {
		return false, true
	}
	intake.tokens = min(intake.burst, intake.tokens+now.Sub(intake.lastToken).Seconds()*intake.rate)
	intake.lastToken = now
	if intake.inflight >= intake.maxConcurrent || intake.tokens < 1 {
		return false, false
	}
	intake.tokens--
	intake.inflight++
	return true, false
}

func (intake *BrowserTraceIntake) release() {
	intake.mu.Lock()
	defer intake.mu.Unlock()
	intake.inflight--
	if intake.closed && intake.inflight == 0 {
		intake.doneOnce.Do(func() { close(intake.done) })
	}
}

// ServeHTTP accepts only JSON POST requests with one exact configured Origin.
// Bodies require a working ResponseController read deadline; wrap writers with
// Unwrap support. Errors have empty bodies and fixed statuses. Acceptance (202)
// means the downstream OTLP receiver accepted the rebuilt batch, not indexing.
func (intake *BrowserTraceIntake) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	statusCode, outcome := http.StatusServiceUnavailable, "unavailable"
	admitted := false
	defer func() {
		if admitted {
			defer intake.release()
		}
		if intake != nil && intake.count != nil {
			intake.count.Add(request.Context(), 1, metric.WithAttributes(attribute.String("outcome", outcome)))
		}
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Content-Length", "0")
		if statusCode != http.StatusAccepted {
			request.Close = true
			writer.Header().Set("Connection", "close")
			// net/http may close/drain its original body after the handler returns,
			// including when wrappers gave this handler a cloned Request. Expire
			// the real read deadline on every rejection before that cleanup runs.
			_ = http.NewResponseController(writer).SetReadDeadline(time.Now())
		}
		writer.WriteHeader(statusCode)
	}()
	if intake == nil {
		return
	}
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		statusCode, outcome = http.StatusMethodNotAllowed, "method"
		return
	}
	origin := request.Header.Values("Origin")
	if len(origin) != 1 {
		statusCode, outcome = http.StatusForbidden, "forbidden"
		return
	}
	if _, allowed := intake.origins[origin[0]]; !allowed {
		statusCode, outcome = http.StatusForbidden, "forbidden"
		return
	}
	mediaType, params, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || len(request.Header.Values("Content-Type")) != 1 || len(request.Header.Values("Content-Encoding")) != 0 || len(params) > 1 || len(params) == 1 && !strings.EqualFold(params["charset"], "utf-8") {
		statusCode, outcome = http.StatusUnsupportedMediaType, "media"
		return
	}
	if request.ContentLength > browserIntakeMaximumBytes {
		statusCode, outcome = http.StatusRequestEntityTooLarge, "too-large"
		return
	}
	if accepted, closed := intake.admit(time.Now()); !accepted {
		if !closed {
			statusCode, outcome = http.StatusTooManyRequests, "rate-limited"
		}
		return
	}
	admitted = true
	ctx, cancel := context.WithTimeout(request.Context(), intake.timeout)
	defer cancel()
	stopShutdown := context.AfterFunc(intake.ctx, cancel)
	defer stopShutdown()
	controller := http.NewResponseController(writer)
	deadline, _ := ctx.Deadline()
	if controller.SetReadDeadline(deadline) != nil {
		return
	}
	readCancelled := make(chan struct{})
	stopRead := context.AfterFunc(ctx, func() { _ = controller.SetReadDeadline(time.Now()); close(readCancelled) })
	body, readErr := io.ReadAll(io.LimitReader(request.Body, browserIntakeMaximumBytes+1))
	if !stopRead() {
		<-readCancelled
	}
	if readErr != nil {
		var networkError net.Error
		if ctx.Err() != nil || errors.As(readErr, &networkError) && networkError.Timeout() {
			statusCode, outcome = http.StatusRequestTimeout, "timeout"
		} else {
			statusCode, outcome = http.StatusBadRequest, "invalid"
		}
		return
	}
	if len(body) > browserIntakeMaximumBytes {
		statusCode, outcome = http.StatusRequestEntityTooLarge, "too-large"
		return
	}
	// EOF was consumed. Only now is it safe to clear the connection deadline:
	// timeout/oversize paths retain it and close the connection to avoid a drain.
	_ = controller.SetReadDeadline(time.Time{})
	batch, err := intake.decode(body, time.Now())
	if err != nil {
		statusCode, outcome = http.StatusBadRequest, "invalid"
		return
	}
	statusCode = intake.sender.send(ctx, batch)
	switch statusCode {
	case http.StatusAccepted:
		outcome = "accepted"
	case http.StatusBadGateway:
		outcome = "rejected"
	}
}

// Shutdown rejects new batches, cancels active reads/exports and closes owned
// transports. It waits for admitted handlers within ctx; repeated calls are
// safe. No provider globals are changed and no intake batch is retried.
func (intake *BrowserTraceIntake) Shutdown(ctx context.Context) error {
	if intake == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	intake.mu.Lock()
	intake.closed = true
	if intake.inflight == 0 {
		intake.doneOnce.Do(func() { close(intake.done) })
	}
	intake.mu.Unlock()
	intake.closeOnce.Do(func() { intake.cancel(); intake.sender.close() })
	select {
	case <-intake.done:
		return nil
	case <-ctx.Done():
		return errors.New("browser intake shutdown deadline exceeded")
	}
}
