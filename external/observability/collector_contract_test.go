package observability_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	collectorlog "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	collectormetric "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

const collectorContractImage = "otel/opentelemetry-collector-contrib:0.160.0"

// TestCollectorContract checks the actual pinned SDK and Collector together.
// Pull collectorContractImage, then opt in with GHATD_TEST_COLLECTOR=1.
// The ordinary unit suite does not require Docker; an opted-in run must not skip
// missing Docker, a missing image, failed ingestion, or incomplete correlation.
func TestCollectorContract(t *testing.T) {
	if os.Getenv("GHATD_TEST_COLLECTOR") != "1" {
		t.Skip("set GHATD_TEST_COLLECTOR=1 to run the real Collector contract")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("Collector contract requires the Docker CLI")
	}
	collectorDocker(t, "connect to Docker", "info", "--format", "{{.ServerVersion}}")
	collectorDocker(t, "find pinned image; run docker pull "+collectorContractImage, "image", "inspect", collectorContractImage)

	for _, protocol := range []string{"http/protobuf", "grpc"} {
		t.Run(protocol, func(t *testing.T) {
			collector := startContractCollector(t)
			port := "4318/tcp"
			if protocol == "grpc" {
				port = "4317/tcp"
			}
			configureContract(t, protocol, "http://"+collector.addresses[port])
			exerciseContract(t)

			// Receiver acceptance precedes batch/file completion. Graceful stop
			// drains the Collector before its final files are copied and decoded.
			collectorDocker(t, "stop Collector", "stop", "--time", "5", collector.name)
			var state struct {
				Running  bool
				ExitCode int
			}
			decodeCollectorJSON(t, collectorDocker(t, "inspect stopped Collector", "inspect", "--format", "{{json .State}}", collector.name), &state)
			if state.Running || state.ExitCode != 0 {
				t.Fatal("Collector contract requires a successful graceful Collector shutdown")
			}
			received := filepath.Join(t.TempDir(), "received")
			collectorDocker(t, "copy exported telemetry", "cp", collector.name+":/output", received)
			assertContract(t, contractSnapshot{
				traces: readCollectorJSONL(t, filepath.Join(received, "traces.jsonl"), func() *collectortrace.ExportTraceServiceRequest {
					return new(collectortrace.ExportTraceServiceRequest)
				}),
				metrics: readCollectorJSONL(t, filepath.Join(received, "metrics.jsonl"), func() *collectormetric.ExportMetricsServiceRequest {
					return new(collectormetric.ExportMetricsServiceRequest)
				}),
				logs: readCollectorJSONL(t, filepath.Join(received, "logs.jsonl"), func() *collectorlog.ExportLogsServiceRequest {
					return new(collectorlog.ExportLogsServiceRequest)
				}),
			})
		})
	}
}

type contractCollector struct {
	name      string
	addresses map[string]string
}

func startContractCollector(t *testing.T) contractCollector {
	t.Helper()
	collector := contractCollector{name: "ghatd-otel-contract-" + uuid.NewString()}
	collectorDocker(t, "create Collector", "create", "--pull=never", "--name", collector.name,
		"-p", "127.0.0.1::4317", "-p", "127.0.0.1::4318", "-p", "127.0.0.1::13133",
		collectorContractImage, "--config=/config.yaml")
	t.Cleanup(func() {
		// The test context is canceled before cleanup. Give this test-owned
		// container an independent, bounded removal even after a failed check.
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := exec.CommandContext(ctx, "docker", "rm", "--force", collector.name).Run(); err != nil {
			t.Error("Collector contract could not remove its test container")
		}
	})

	output := filepath.Join(t.TempDir(), "output")
	if err := os.Mkdir(output, 0o777); err != nil {
		t.Fatal("Collector contract could not create its output directory")
	}
	// Override the host umask for the isolated copied directory. The pinned
	// image retains its default nonroot UID 10001; it needs no root override.
	if err := os.Chmod(output, 0o777); err != nil {
		t.Fatal("Collector contract could not prepare output directory permissions")
	}
	collectorDocker(t, "copy Collector configuration", "cp", "testdata/collector.yaml", collector.name+":/config.yaml")
	collectorDocker(t, "copy Collector output directory", "cp", output, collector.name+":/output")
	collectorDocker(t, "start Collector", "start", collector.name)

	var ports map[string][]struct {
		HostIP   string `json:"HostIp"`
		HostPort string `json:"HostPort"`
	}
	decodeCollectorJSON(t, collectorDocker(t, "inspect Collector ports", "inspect", "--format", "{{json .NetworkSettings.Ports}}", collector.name), &ports)
	collector.addresses = make(map[string]string)
	for _, port := range []string{"4317/tcp", "4318/tcp", "13133/tcp"} {
		bindings := ports[port]
		if len(bindings) != 1 || bindings[0].HostIP != "127.0.0.1" {
			t.Fatal("Collector contract requires one loopback binding per receiver")
		}
		number, err := strconv.Atoi(bindings[0].HostPort)
		if err != nil || number < 1 || number > 65535 {
			t.Fatal("Collector contract received an invalid published port")
		}
		collector.addresses[port] = net.JoinHostPort(bindings[0].HostIP, bindings[0].HostPort)
	}
	waitForContractCollector(t, "http://"+collector.addresses["13133/tcp"]+"/healthz")
	return collector
}

func waitForContractCollector(t *testing.T, endpoint string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	transport := &http.Transport{}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport, Timeout: time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			t.Fatal("Collector contract could not construct its health request")
		}
		response, err := client.Do(request)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("Collector contract timed out waiting for Collector startup")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func collectorDocker(t *testing.T, phase string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "docker", args...).Output()
	if err != nil {
		// Subprocess output and errors can include endpoint paths or payloads.
		// Report only a static phase; never dump exported canaries on failure.
		t.Fatalf("Collector contract: %s failed", phase)
	}
	return output
}

func decodeCollectorJSON(t *testing.T, data []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatal("Collector contract received invalid container metadata")
	}
}

func readCollectorJSONL[T proto.Message](t *testing.T, path string, create func() T) []T {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal("Collector contract is missing a signal output file")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() == 0 || info.Size() > 16<<20 {
		t.Fatal("Collector contract signal output is empty or exceeds its size limit")
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	var messages []T
	for scanner.Scan() {
		// Preserve JSON integers exactly while converting only OTLP identifier
		// fields: Collector JSON uses hex, generic protojson uses base64 bytes.
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.UseNumber()
		var value any
		if err := decoder.Decode(&value); err != nil {
			t.Fatal("Collector contract contains an invalid JSONL record")
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			t.Fatal("Collector contract contains multiple objects in one JSONL record")
		}
		if !normalizeCollectorIDs(value) {
			t.Fatal("Collector contract contains an invalid OTLP identifier")
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal("Collector contract could not normalize its JSONL record")
		}
		message := create()
		if err := protojson.Unmarshal(encoded, message); err != nil {
			t.Fatal("Collector contract output does not match the pinned OTLP schema")
		}
		messages = append(messages, message)
	}
	if scanner.Err() != nil || len(messages) == 0 {
		t.Fatal("Collector contract could not read complete signal output")
	}
	return messages
}

func normalizeCollectorIDs(value any) bool {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			if key == "traceId" || key == "spanId" || key == "parentSpanId" {
				identifier, ok := child.(string)
				if !ok {
					return false
				}
				if identifier == "" {
					continue
				}
				length := 16
				if key == "traceId" {
					length = 32
				}
				decoded, err := hex.DecodeString(identifier)
				if err != nil || len(identifier) != length {
					return false
				}
				value[key] = base64.StdEncoding.EncodeToString(decoded)
			} else if !normalizeCollectorIDs(child) {
				return false
			}
		}
	case []any:
		for _, child := range value {
			if !normalizeCollectorIDs(child) {
				return false
			}
		}
	}
	return true
}
