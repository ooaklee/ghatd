package observability_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ghodss/yaml"
	"github.com/google/uuid"
	prometheuspb "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	collectorlog "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logpb "go.opentelemetry.io/proto/otlp/logs/v1"
	metricpb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

const collectorContractHelperImage = "busybox:1.37.0"

// TestCollectorResilienceContract loads the shipped Collector configuration,
// shortening only test timing/capacity limits. Every topology owns its network,
// containers and volume. No production endpoint or credentials are involved.
// These checks concern data already accepted by the exporter's persistent
// queue, not the SDK, batch processor or tail sampler's in-memory buffers.
func TestCollectorResilienceContract(t *testing.T) {
	if os.Getenv("GHATD_TEST_COLLECTOR") != "1" {
		t.Skip("set GHATD_TEST_COLLECTOR=1 to run real Collector resilience contracts")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("Collector resilience contract requires the Docker CLI")
	}
	collectorDocker(t, "connect to Docker", "info", "--format", "{{.ServerVersion}}")
	for _, image := range []string{collectorContractImage, collectorContractHelperImage} {
		collectorDocker(t, "find pinned test image; pull "+image, "image", "inspect", image)
	}
	t.Run("persistent queues survive forced recreation", testCollectorQueueRecovery)
	t.Run("tail sampling keeps errors and slow traces", testCollectorTailSampling)
	t.Run("bounded retry reports exhausted delivery", testCollectorBoundedRetry)
	t.Run("permanent rejection is not retried", testCollectorPermanentRejection)
	t.Run("queue pressure remains visible while healthy", testCollectorQueuePressure)
}

func testCollectorQueueRecovery(t *testing.T) {
	topology := newResilienceTopology(t)
	backend := topology.createCollector(t, "backend", "", readResilienceFile(t, "testdata/collector.yaml"))
	config := resilienceConfiguration(t, false)
	producer := topology.createCollector(t, "producer", topology.volume, config)
	producer.start(t)
	configureContract(t, "http/protobuf", producer.endpoint())
	exerciseContract(t)
	postResilienceSnapshot(t, producer.endpoint(), resilienceFixture("insertion-contract", 11, time.Millisecond, false))
	producer.waitMetrics(t, "all signal payloads reach persistent queues", 15*time.Second, func(metrics resilienceMetrics) bool {
		for _, signal := range []string{"traces", "metrics", "logs"} {
			if metrics.value("otelcol_exporter_queue_size", "data_type", signal) <= 0 {
				return false
			}
		}
		return true
	})
	producer.assertHealthy(t)
	// v0.160.0 includes dispatched retrying items in its persistent queue
	// gauge. The acknowledged, nonempty queues above are the restart boundary.
	collectorDocker(t, "kill queued Collector", "kill", "--signal", "KILL", producer.name)
	assert.Equal(t, "10001:10001:700", strings.TrimSpace(string(collectorDocker(t, "verify persistent queue ownership",
		"run", "--rm", "--pull=never", "--network=none", "--mount", "type=volume,source="+topology.volume+",target=/var/lib/otelcol",
		collectorContractHelperImage, "stat", "-c", "%u:%g:%a", "/var/lib/otelcol/queue"))))

	// Same volume, storage ID and exporter ID; the replacement process starts
	// before the backend so recovery cannot be mistaken for new SDK delivery.
	producer = topology.createCollector(t, "producer", topology.volume, config)
	producer.start(t)
	producer.waitMetrics(t, "all signal queues reload after process replacement", 15*time.Second, func(metrics resilienceMetrics) bool {
		for _, signal := range []string{"traces", "metrics", "logs"} {
			if metrics.value("otelcol_exporter_queue_size", "data_type", signal) <= 0 {
				return false
			}
		}
		return true
	})
	backend.start(t)
	producer.waitMetrics(t, "recovered queues drain to the backend", 20*time.Second, func(metrics resilienceMetrics) bool {
		for _, signal := range []string{"traces", "metrics", "logs"} {
			value, exists := metrics.lookup("otelcol_exporter_queue_size", "data_type", signal)
			if !exists || value != 0 {
				return false
			}
		}
		return metrics.value("otelcol_exporter_sent_spans") >= 7 && metrics.value("otelcol_exporter_sent_log_records") >= 4 &&
			metrics.value("otelcol_exporter_sent_metric_points") > 0
	})
	producer.stop(t)
	snapshot := backend.finish(t)
	assertContract(t, selectResilienceService(snapshot, "contract-api"))
	inserted := selectResilienceService(snapshot, "insertion-contract")
	require.NotEmpty(t, inserted.traces)
	require.NotEmpty(t, inserted.metrics)
	require.NotEmpty(t, inserted.logs)
	assertResilienceResource(t, inserted, "insertion-contract", "collector-fallback", "test-fallback")
}

func testCollectorTailSampling(t *testing.T) {
	topology := newResilienceTopology(t)
	backend := topology.createCollector(t, "backend", "", readResilienceFile(t, "testdata/collector.yaml"))
	backend.start(t)
	producer := topology.createCollector(t, "producer", topology.volume, resilienceConfiguration(t, true))
	producer.start(t)
	for index, example := range []struct {
		name     string
		duration time.Duration
		failed   bool
	}{
		{"tail-error", time.Millisecond, true},
		{"tail-slow", 3 * time.Second, false},
		{"tail-normal", time.Millisecond, false},
	} {
		fixture := resilienceFixture(example.name, byte(index+21), example.duration, example.failed)
		scope := fixture.traces[0].ResourceSpans[0].ScopeSpans[0]
		root := scope.Spans[0]
		child := proto.Clone(root).(*tracepb.Span)
		child.Name, child.SpanId, child.ParentSpanId = "synthetic-child", bytes.Repeat([]byte{byte(index + 61)}, 8), root.SpanId
		// The error policy must retain the whole trace when only a child fails.
		root.Status.Code = tracepb.Status_STATUS_CODE_OK
		scope.Spans = append(scope.Spans, child)
		postResilienceSnapshot(t, producer.endpoint(), fixture)
	}
	producer.waitMetrics(t, "all three tail decisions finish", 15*time.Second, func(metrics resilienceMetrics) bool {
		// The per-policy counter counts both decisions once for each policy.
		// Use global decisions so ordinary input must have been evaluated too,
		// rather than merely disappearing from an unfinished shutdown buffer.
		return metrics.value("otelcol_processor_tail_sampling_global_count_traces_sampled", "sampled", "true") == 2 &&
			metrics.value("otelcol_processor_tail_sampling_global_count_traces_sampled", "sampled", "false") == 1 &&
			metrics.value("otelcol_exporter_sent_spans") >= 4 && metrics.value("otelcol_exporter_sent_log_records") >= 3
	})
	producer.stop(t)
	snapshot := backend.finish(t)
	for _, name := range []string{"tail-error", "tail-slow", "tail-normal"} {
		selected := selectResilienceService(snapshot, name)
		if name == "tail-normal" {
			assert.Empty(t, selected.traces, "baseline zero must drop the complete ordinary trace")
		} else {
			require.Len(t, selected.traces, 1)
			require.Len(t, selected.traces[0].ResourceSpans[0].ScopeSpans[0].Spans, 2, "a selected trace retains both parent and child")
		}
		require.Len(t, selected.metrics, 1, "tail sampling must retain ordinary measurements")
		require.Len(t, selected.logs, 1, "tail sampling must retain native correlated logs")
		measurements := selected.metrics[0].ResourceMetrics[0].ScopeMetrics[0].Metrics
		require.Len(t, measurements, 1)
		points := measurements[0].GetSum().GetDataPoints()
		require.Len(t, points, 1)
		assert.EqualValues(t, 1, points[0].GetAsInt())
		records := selected.logs[0].ResourceLogs[0].ScopeLogs[0].LogRecords
		require.Len(t, records, 1)
		record := records[0]
		assert.Len(t, record.TraceId, 16)
		assert.Len(t, record.SpanId, 8)
		if len(selected.traces) > 0 {
			spans := selected.traces[0].ResourceSpans[0].ScopeSpans[0].Spans
			span, child := spans[0], spans[1]
			if span.Name == "synthetic-child" {
				span, child = child, span
			}
			assert.Equal(t, span.TraceId, record.TraceId)
			assert.Equal(t, span.SpanId, record.SpanId)
			assert.Equal(t, span.SpanId, child.ParentSpanId)
			assert.Equal(t, span.TraceId, child.TraceId)
			assert.Equal(t, tracepb.Status_STATUS_CODE_OK, span.GetStatus().Code)
			if name == "tail-error" {
				assert.Equal(t, tracepb.Status_STATUS_CODE_ERROR, child.GetStatus().Code)
			}
		}
		assertResilienceResource(t, selected, name, "collector-fallback", "test-fallback")
	}
}

func testCollectorBoundedRetry(t *testing.T) {
	topology := newResilienceTopology(t)
	config := resilienceConfigurationMap(t, false)
	exporter := resilienceMap(t, config, "exporters", "otlp_http/backend")
	exporter["timeout"] = "200ms"
	retry := resilienceMap(t, exporter, "retry_on_failure")
	retry["initial_interval"], retry["max_interval"], retry["max_elapsed_time"] = "100ms", "200ms", "1s"
	producer := topology.createCollector(t, "producer", topology.volume, encodeResilienceConfiguration(t, config))
	producer.start(t)
	fixture := resilienceFixture("retry-contract", 31, time.Millisecond, false)
	fixture.traces, fixture.metrics = nil, nil
	postResilienceSnapshot(t, producer.endpoint(), fixture)
	producer.waitMetrics(t, "bounded retries end in a visible delivery failure", 8*time.Second, func(metrics resilienceMetrics) bool {
		queue, exists := metrics.lookup("otelcol_exporter_queue_size", "data_type", "logs")
		return exists && queue == 0 && metrics.value("otelcol_exporter_send_failed_log_records") >= 1 &&
			metrics.value("otelcol_receiver_accepted_log_records") >= 1
	})
	producer.assertHealthy(t)
}

func testCollectorPermanentRejection(t *testing.T) {
	topology := newResilienceTopology(t)
	rejector := "ghatd-otel-reject-" + uuid.NewString()
	collectorDocker(t, "create permanent-rejection backend", "create", "--pull=never", "--name", rejector,
		"--network", topology.network, "--network-alias", "backend", "-p", "127.0.0.1::4318",
		collectorContractHelperImage, "httpd", "-f", "-p", "4318", "-h", "/tmp")
	removeResilienceContainer(t, rejector)
	collectorDocker(t, "start permanent-rejection backend", "start", rejector)
	rejectAddress := resilienceAddresses(t, rejector, []string{"4318/tcp"})["4318/tcp"]
	resilienceWait(t, "permanent-rejection backend starts", 10*time.Second, func() bool {
		return resilienceHTTPStatus("http://"+rejectAddress+"/v1/logs") == http.StatusNotFound
	})
	rejectClient := &http.Client{Transport: &http.Transport{}, Timeout: time.Second}
	defer rejectClient.CloseIdleConnections()
	rejection, err := rejectClient.Post("http://"+rejectAddress+"/v1/logs", "application/x-protobuf", bytes.NewReader(nil))
	require.NoError(t, err)
	require.NoError(t, rejection.Body.Close())
	require.Contains(t, []int{http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusNotImplemented}, rejection.StatusCode)
	producer := topology.createCollector(t, "producer", topology.volume, resilienceConfiguration(t, false))
	producer.start(t)
	fixture := resilienceFixture("rejection-contract", 32, time.Millisecond, false)
	fixture.traces, fixture.metrics = nil, nil
	postResilienceSnapshot(t, producer.endpoint(), fixture)
	// The normal retry budget is 60s. A real permanent HTTP rejection clears its queue
	// immediately rather than enter that retry loop; health stays available.
	producer.waitMetrics(t, "permanent rejection completes without a retry budget", 5*time.Second, func(metrics resilienceMetrics) bool {
		queue, exists := metrics.lookup("otelcol_exporter_queue_size", "data_type", "logs")
		return exists && queue == 0 && metrics.value("otelcol_exporter_send_failed_log_records") >= 1
	})
	producer.assertHealthy(t)
}

func testCollectorQueuePressure(t *testing.T) {
	topology := newResilienceTopology(t)
	config := resilienceConfigurationMap(t, false)
	queue := resilienceMap(t, config, "exporters", "otlp_http/backend", "sending_queue")
	queue["queue_size"], queue["num_consumers"] = 4096, 1
	batch := resilienceMap(t, config, "processors", "batch")
	batch["send_batch_size"], batch["send_batch_max_size"] = 1, 1
	producer := topology.createCollector(t, "producer", topology.volume, encodeResilienceConfiguration(t, config))
	producer.start(t)
	fixture := resilienceFixture("pressure-contract", 33, time.Millisecond, false)
	fixture.traces, fixture.metrics = nil, nil
	fixture.logs[0].ResourceLogs[0].ScopeLogs[0].LogRecords[0].Attributes = []*commonpb.KeyValue{resilienceAttribute("synthetic.padding", strings.Repeat("x", 1024))}
	for range 12 {
		postResilienceSnapshot(t, producer.endpoint(), fixture)
	}
	producer.waitMetrics(t, "finite queue pressure emits enqueue failures", 10*time.Second, func(metrics resilienceMetrics) bool {
		size := metrics.value("otelcol_exporter_queue_size", "data_type", "logs")
		capacity := metrics.value("otelcol_exporter_queue_capacity", "data_type", "logs")
		return capacity == 4096 && size > 0 && size <= capacity &&
			metrics.value("otelcol_exporter_enqueue_failed_log_records") > 0
	})
	producer.assertHealthy(t)
}

type resilienceTopology struct{ network, volume string }

func newResilienceTopology(t *testing.T) resilienceTopology {
	t.Helper()
	topology := resilienceTopology{network: "ghatd-otel-network-" + uuid.NewString(), volume: "ghatd-otel-queue-" + uuid.NewString()}
	// A dedicated bridge isolates these fixtures from other containers. Docker
	// Desktop disables published ports on --internal networks; only loopback
	// bindings below expose the test receiver and independent health scrape.
	collectorDocker(t, "create isolated test network", "network", "create", topology.network)
	t.Cleanup(func() { cleanupResilienceResource(t, "network", "rm", topology.network) })
	collectorDocker(t, "create persistent test volume", "volume", "create", topology.volume)
	t.Cleanup(func() { cleanupResilienceResource(t, "volume", "rm", topology.volume) })
	collectorDocker(t, "initialize nonroot persistent storage", "run", "--rm", "--pull=never", "--network=none",
		"--mount", "type=volume,source="+topology.volume+",target=/var/lib/otelcol", collectorContractHelperImage,
		"sh", "-c", "mkdir -p /var/lib/otelcol/queue && chown 10001:10001 /var/lib/otelcol/queue && chmod 0700 /var/lib/otelcol/queue")
	return topology
}

type resilienceCollector struct {
	name      string
	addresses map[string]string
}

func (topology resilienceTopology) createCollector(t *testing.T, alias, volume string, config []byte) *resilienceCollector {
	t.Helper()
	collector := &resilienceCollector{name: "ghatd-otel-resilience-" + uuid.NewString()}
	args := []string{"create", "--pull=never", "--name", collector.name, "--network", topology.network, "--network-alias", alias,
		"-p", "127.0.0.1::4318", "-p", "127.0.0.1::13133", "-p", "127.0.0.1::8888",
		"--env", "COLLECTOR_BACKEND_ENDPOINT=http://backend:4318", "--env", "COLLECTOR_AUTHORIZATION=",
		"--env", "COLLECTOR_SERVICE_NAMESPACE=collector-fallback", "--env", "COLLECTOR_ENVIRONMENT=test-fallback",
		"--env", "COLLECTOR_SERVICE_NAME=collector-contract-self"}
	if volume != "" {
		args = append(args, "--mount", "type=volume,source="+volume+",target=/var/lib/otelcol")
	}
	args = append(args, collectorContractImage, "--config=/config.yaml")
	collectorDocker(t, "create resilience Collector", args...)
	removeResilienceContainer(t, collector.name)
	assert.Equal(t, "10001:10001", strings.TrimSpace(string(collectorDocker(t, "verify Collector nonroot user", "inspect", "--format", "{{.Config.User}}", collector.name))))
	directory := t.TempDir()
	configPath := filepath.Join(directory, "collector.yaml")
	require.NoError(t, os.WriteFile(configPath, config, 0o644))
	collectorDocker(t, "copy resilience configuration", "cp", configPath, collector.name+":/config.yaml")
	output := filepath.Join(directory, "output")
	require.NoError(t, os.Mkdir(output, 0o777))
	require.NoError(t, os.Chmod(output, 0o777))
	collectorDocker(t, "prepare nonroot signal output", "cp", output, collector.name+":/output")
	return collector
}

func (collector *resilienceCollector) start(t *testing.T) {
	t.Helper()
	collectorDocker(t, "start resilience Collector", "start", collector.name)
	collector.addresses = resilienceAddresses(t, collector.name, []string{"4318/tcp", "13133/tcp", "8888/tcp"})
	waitForContractCollector(t, "http://"+collector.addresses["13133/tcp"]+"/healthz")
}

func (collector *resilienceCollector) endpoint() string {
	return "http://" + collector.addresses["4318/tcp"]
}

func (collector *resilienceCollector) assertHealthy(t *testing.T) {
	t.Helper()
	assert.Equal(t, http.StatusOK, resilienceHTTPStatus("http://"+collector.addresses["13133/tcp"]+"/healthz"), "readiness must not imply successful backend delivery")
}

func (collector *resilienceCollector) stop(t *testing.T) {
	t.Helper()
	collectorDocker(t, "stop resilience Collector", "stop", "--time", "5", collector.name)
	var state struct {
		Running  bool
		ExitCode int
	}
	decodeCollectorJSON(t, collectorDocker(t, "inspect completed Collector", "inspect", "--format", "{{json .State}}", collector.name), &state)
	require.False(t, state.Running)
	require.Zero(t, state.ExitCode, "normal Collector completion must drain successfully")
}

func (collector *resilienceCollector) finish(t *testing.T) contractSnapshot {
	t.Helper()
	collector.stop(t)
	directory := filepath.Join(t.TempDir(), "received")
	collectorDocker(t, "copy recovered signal output", "cp", collector.name+":/output", directory)
	return contractSnapshot{
		traces: readCollectorJSONL(t, filepath.Join(directory, "traces.jsonl"), func() *collectortrace.ExportTraceServiceRequest { return new(collectortrace.ExportTraceServiceRequest) }),
		metrics: readCollectorJSONL(t, filepath.Join(directory, "metrics.jsonl"), func() *collectormetric.ExportMetricsServiceRequest {
			return new(collectormetric.ExportMetricsServiceRequest)
		}),
		logs: readCollectorJSONL(t, filepath.Join(directory, "logs.jsonl"), func() *collectorlog.ExportLogsServiceRequest { return new(collectorlog.ExportLogsServiceRequest) }),
	}
}

func resilienceAddresses(t *testing.T, container string, expected []string) map[string]string {
	t.Helper()
	var ports map[string][]struct{ HostIP, HostPort string }
	decodeCollectorJSON(t, collectorDocker(t, "inspect resilience ports", "inspect", "--format", "{{json .NetworkSettings.Ports}}", container), &ports)
	addresses := make(map[string]string, len(expected))
	for _, port := range expected {
		bindings := ports[port]
		require.Len(t, bindings, 1, "test services need one loopback binding")
		require.Equal(t, "127.0.0.1", bindings[0].HostIP)
		number, err := strconv.Atoi(bindings[0].HostPort)
		require.NoError(t, err)
		require.True(t, number > 0 && number <= 65535)
		addresses[port] = net.JoinHostPort(bindings[0].HostIP, bindings[0].HostPort)
	}
	return addresses
}

func removeResilienceContainer(t *testing.T, name string) {
	t.Helper()
	t.Cleanup(func() { cleanupResilienceResource(t, "rm", "--force", name) })
}

func cleanupResilienceResource(t *testing.T, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "docker", args...).Run(); err != nil {
		t.Error("Collector resilience contract could not remove its isolated test resource")
	}
}

func resilienceConfiguration(t *testing.T, tail bool) []byte {
	t.Helper()
	return encodeResilienceConfiguration(t, resilienceConfigurationMap(t, tail))
}

func resilienceConfigurationMap(t *testing.T, tail bool) map[string]any {
	t.Helper()
	var config map[string]any
	decodeResilienceConfiguration(t, readResilienceFile(t, "../../examples/observability/collector/collector.yaml"), &config)
	if tail {
		var overlay map[string]any
		decodeResilienceConfiguration(t, readResilienceFile(t, "../../examples/observability/collector/collector-tail.yaml"), &overlay)
		mergeResilienceConfiguration(config, overlay)
		sampler := resilienceMap(t, config, "processors", "tail_sampling")
		sampler["decision_wait"] = "1s"
		policies, ok := sampler["policies"].([]any)
		require.True(t, ok)
		baselineFound := false
		for _, value := range policies {
			policy, ok := value.(map[string]any)
			require.True(t, ok)
			if policy["type"] == "probabilistic" {
				resilienceMap(t, policy, "probabilistic")["sampling_percentage"] = 0
				baselineFound = true
			}
		}
		require.True(t, baselineFound, "tail fixture must explicitly override the shipped baseline")
	}
	batch := resilienceMap(t, config, "processors", "batch")
	batch["timeout"] = "100ms"
	exporter := resilienceMap(t, config, "exporters", "otlp_http/backend")
	exporter["timeout"] = "500ms"
	retry := resilienceMap(t, exporter, "retry_on_failure")
	retry["initial_interval"], retry["max_interval"], retry["max_elapsed_time"] = "100ms", "500ms", "60s"
	return config
}

func resilienceMap(t *testing.T, root map[string]any, path ...string) map[string]any {
	t.Helper()
	for _, key := range path {
		next, ok := root[key].(map[string]any)
		require.True(t, ok, "required shipped Collector configuration structure is missing")
		root = next
	}
	return root
}

func mergeResilienceConfiguration(target, overlay map[string]any) {
	for key, value := range overlay {
		child, sourceMap := value.(map[string]any)
		existing, targetMap := target[key].(map[string]any)
		if sourceMap && targetMap {
			mergeResilienceConfiguration(existing, child)
		} else {
			target[key] = value
		}
	}
}

func readResilienceFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err, "required Collector fixture is missing")
	return data
}

func decodeResilienceConfiguration(t *testing.T, data []byte, target any) {
	t.Helper()
	encoded, err := yaml.YAMLToJSON(data)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(encoded, target))
}

func encodeResilienceConfiguration(t *testing.T, config map[string]any) []byte {
	t.Helper()
	encoded, err := json.Marshal(config)
	require.NoError(t, err)
	data, err := yaml.JSONToYAML(encoded)
	require.NoError(t, err)
	return data
}

type resilienceMetrics map[string]*prometheuspb.MetricFamily

func (metrics resilienceMetrics) lookup(name string, labels ...string) (float64, bool) {
	var total float64
	found := false
	for _, metric := range metrics[name].GetMetric() {
		matches := true
		for index := 0; index < len(labels); index += 2 {
			match := false
			for _, label := range metric.Label {
				if label.GetName() == labels[index] && label.GetValue() == labels[index+1] {
					match = true
				}
			}
			matches = matches && match
		}
		if matches {
			found = true
			total += metric.GetCounter().GetValue() + metric.GetGauge().GetValue()
		}
	}
	return total, found
}

func (metrics resilienceMetrics) value(name string, labels ...string) float64 {
	value, _ := metrics.lookup(name, labels...)
	return value
}

func (collector *resilienceCollector) waitMetrics(t *testing.T, phase string, timeout time.Duration, predicate func(resilienceMetrics) bool) {
	t.Helper()
	resilienceWait(t, phase, timeout, func() bool {
		client := &http.Client{Transport: &http.Transport{}, Timeout: time.Second}
		defer client.CloseIdleConnections()
		response, err := client.Get("http://" + collector.addresses["8888/tcp"] + "/metrics")
		if err != nil {
			return false
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return false
		}
		parser := expfmt.NewTextParser(model.UTF8Validation)
		metrics, err := parser.TextToMetricFamilies(io.LimitReader(response.Body, 4<<20))
		return err == nil && predicate(metrics)
	})
}

func resilienceWait(t *testing.T, phase string, timeout time.Duration, condition func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for {
		if condition() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Collector resilience contract timed out: %s", phase)
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func resilienceHTTPStatus(endpoint string) int {
	client := &http.Client{Transport: &http.Transport{}, Timeout: time.Second}
	defer client.CloseIdleConnections()
	response, err := client.Get(endpoint)
	if err != nil {
		return 0
	}
	defer response.Body.Close()
	return response.StatusCode
}

func postResilienceSnapshot(t *testing.T, endpoint string, snapshot contractSnapshot) {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{}, Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	post := func(path string, message proto.Message) {
		payload, err := proto.Marshal(message)
		require.NoError(t, err)
		response, err := client.Post(endpoint+path, "application/x-protobuf", bytes.NewReader(payload))
		require.NoError(t, err)
		body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		require.Equal(t, http.StatusOK, response.StatusCode, "synthetic OTLP request must be accepted")
		switch path {
		case "/v1/traces":
			result := new(collectortrace.ExportTraceServiceResponse)
			require.NoError(t, proto.Unmarshal(body, result))
			require.Zero(t, result.GetPartialSuccess().GetRejectedSpans())
		case "/v1/metrics":
			result := new(collectormetric.ExportMetricsServiceResponse)
			require.NoError(t, proto.Unmarshal(body, result))
			require.Zero(t, result.GetPartialSuccess().GetRejectedDataPoints())
		case "/v1/logs":
			result := new(collectorlog.ExportLogsServiceResponse)
			require.NoError(t, proto.Unmarshal(body, result))
			require.Zero(t, result.GetPartialSuccess().GetRejectedLogRecords())
		}
	}
	for _, request := range snapshot.traces {
		post("/v1/traces", request)
	}
	for _, request := range snapshot.metrics {
		post("/v1/metrics", request)
	}
	for _, request := range snapshot.logs {
		post("/v1/logs", request)
	}
}

func resilienceFixture(name string, identifier byte, duration time.Duration, failed bool) contractSnapshot {
	resource := &resourcepb.Resource{Attributes: []*commonpb.KeyValue{
		resilienceAttribute("service.name", name), resilienceAttribute("service.instance.id", "sender-owned"),
	}}
	traceID, spanID := make([]byte, 16), make([]byte, 8)
	traceID[15], spanID[7] = identifier, identifier
	end := time.Now()
	start := end.Add(-duration)
	span := &tracepb.Span{Name: "synthetic-operation", TraceId: traceID, SpanId: spanID,
		Kind: tracepb.Span_SPAN_KIND_INTERNAL, StartTimeUnixNano: uint64(start.UnixNano()), EndTimeUnixNano: uint64(end.UnixNano()),
		Status: &tracepb.Status{Code: tracepb.Status_STATUS_CODE_OK}, Flags: 1}
	if failed {
		span.Status.Code = tracepb.Status_STATUS_CODE_ERROR
	}
	metric := &metricpb.Metric{
		Name: "collector.contract.completed",
		Data: &metricpb.Metric_Sum{Sum: &metricpb.Sum{
			AggregationTemporality: metricpb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
			IsMonotonic:            true,
			DataPoints: []*metricpb.NumberDataPoint{{
				StartTimeUnixNano: uint64(start.UnixNano()), TimeUnixNano: uint64(end.UnixNano()),
				Value: &metricpb.NumberDataPoint_AsInt{AsInt: 1},
			}},
		}},
	}
	log := &logpb.LogRecord{
		TimeUnixNano: uint64(end.UnixNano()), ObservedTimeUnixNano: uint64(end.UnixNano()),
		TraceId: traceID, SpanId: spanID, Flags: 1,
		Body: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "synthetic operation completed"}},
	}
	scope := &commonpb.InstrumentationScope{Name: "collector-contract"}
	return contractSnapshot{
		traces: []*collectortrace.ExportTraceServiceRequest{{ResourceSpans: []*tracepb.ResourceSpans{{
			Resource: resource, ScopeSpans: []*tracepb.ScopeSpans{{Scope: scope, Spans: []*tracepb.Span{span}}},
		}}}},
		metrics: []*collectormetric.ExportMetricsServiceRequest{{ResourceMetrics: []*metricpb.ResourceMetrics{{
			Resource: resource, ScopeMetrics: []*metricpb.ScopeMetrics{{Scope: scope, Metrics: []*metricpb.Metric{metric}}},
		}}}},
		logs: []*collectorlog.ExportLogsServiceRequest{{ResourceLogs: []*logpb.ResourceLogs{{
			Resource: resource, ScopeLogs: []*logpb.ScopeLogs{{Scope: scope, LogRecords: []*logpb.LogRecord{log}}},
		}}}},
	}
}

func resilienceAttribute(name, value string) *commonpb.KeyValue {
	return &commonpb.KeyValue{Key: name, Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: value}}}
}

func selectResilienceService(snapshot contractSnapshot, name string) contractSnapshot {
	var result contractSnapshot
	for _, request := range snapshot.traces {
		for _, resource := range request.ResourceSpans {
			if contractAttribute(resource.Resource.GetAttributes(), "service.name") == name {
				result.traces = append(result.traces, &collectortrace.ExportTraceServiceRequest{ResourceSpans: []*tracepb.ResourceSpans{resource}})
			}
		}
	}
	for _, request := range snapshot.metrics {
		for _, resource := range request.ResourceMetrics {
			if contractAttribute(resource.Resource.GetAttributes(), "service.name") == name {
				result.metrics = append(result.metrics, &collectormetric.ExportMetricsServiceRequest{ResourceMetrics: []*metricpb.ResourceMetrics{resource}})
			}
		}
	}
	for _, request := range snapshot.logs {
		for _, resource := range request.ResourceLogs {
			if contractAttribute(resource.Resource.GetAttributes(), "service.name") == name {
				result.logs = append(result.logs, &collectorlog.ExportLogsServiceRequest{ResourceLogs: []*logpb.ResourceLogs{resource}})
			}
		}
	}
	return result
}

func assertResilienceResource(t *testing.T, snapshot contractSnapshot, name, namespace, environment string) {
	t.Helper()
	resource := func(value *resourcepb.Resource) {
		assert.Equal(t, name, contractAttribute(value.Attributes, "service.name"))
		assert.Equal(t, namespace, contractAttribute(value.Attributes, "service.namespace"))
		assert.Equal(t, environment, contractAttribute(value.Attributes, "deployment.environment.name"))
		assert.Equal(t, "sender-owned", contractAttribute(value.Attributes, "service.instance.id"))
	}
	for _, request := range snapshot.traces {
		for _, group := range request.ResourceSpans {
			resource(group.Resource)
		}
	}
	for _, request := range snapshot.metrics {
		for _, group := range request.ResourceMetrics {
			resource(group.Resource)
		}
	}
	for _, request := range snapshot.logs {
		for _, group := range request.ResourceLogs {
			resource(group.Resource)
		}
	}
}
