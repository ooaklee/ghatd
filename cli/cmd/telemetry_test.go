package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ooaklee/ghatd/external/observability"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	logglobal "go.opentelemetry.io/otel/log/global"
)

const cliDoctorCanary = "synthetic-cli-private-canary"

func TestTelemetryDoctorDefaultIsSanitizedAndDoesNotExport(t *testing.T) {
	clearDoctorEnvironment(t)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1) }))
	defer server.Close()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", server.URL+"/"+cliDoctorCanary)
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "X-Private-Header="+cliDoctorCanary)
	t.Setenv("OTEL_SERVICE_NAME", cliDoctorCanary)
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "deployment.environment.name="+cliDoctorCanary)
	traces, metrics, logs, propagation := otel.GetTracerProvider(), otel.GetMeterProvider(), logglobal.GetLoggerProvider(), otel.GetTextMapPropagator()
	report, output, err := executeDoctor(t, "doctor")
	require.NoError(t, err)
	assert.Nil(t, report.Probe)
	require.Len(t, report.Configuration.Signals, 3)
	for _, signal := range report.Configuration.Signals {
		assert.True(t, signal.HeadersPresent)
		assert.Equal(t, "http/protobuf", signal.Protocol)
	}
	assert.NotContains(t, output, cliDoctorCanary)
	assert.NotContains(t, output, "X-Private-Header")
	assert.NotContains(t, output, server.URL)
	assert.Zero(t, requests.Load())
	assert.Same(t, traces, otel.GetTracerProvider())
	assert.Same(t, metrics, otel.GetMeterProvider())
	assert.Same(t, logs, logglobal.GetLoggerProvider())
	assert.Equal(t, propagation, otel.GetTextMapPropagator())
}

func TestTelemetryDoctorProbeReportsReceiverAcceptanceAndFailure(t *testing.T) {
	for _, code := range []int{http.StatusOK, http.StatusUnauthorized} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			clearDoctorEnvironment(t)
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/x-protobuf")
				w.WriteHeader(code)
				if code != http.StatusOK {
					_, _ = io.WriteString(w, cliDoctorCanary)
				}
			}))
			defer server.Close()
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", server.URL)
			report, output, err := executeDoctor(t, "doctor", "--probe", "--timeout", "1s")
			want := observability.ProbeAccepted
			if code == http.StatusOK {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, errDoctorCheck)
				want = observability.ProbeAuthentication
			}
			require.NotNil(t, report.Probe)
			assert.Equal(t, "receiver_acceptance", report.Probe.Mode)
			for _, signal := range report.Probe.Signals {
				assert.Equal(t, want, signal.Status)
			}
			assert.EqualValues(t, 3, requests.Load())
			assert.NotContains(t, output, cliDoctorCanary)
			assert.NotContains(t, output, server.URL)
		})
	}
}

func TestTelemetryDoctorInvalidConfigurationAndDisabledSignals(t *testing.T) {
	t.Run("invalid configuration returns an error", func(t *testing.T) {
		clearDoctorEnvironment(t)
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://"+cliDoctorCanary+"@example.invalid/")
		report, output, err := executeDoctor(t, "doctor", "--probe")
		require.ErrorIs(t, err, errDoctorCheck)
		require.NotEmpty(t, report.Configuration.Issues)
		require.NotNil(t, report.Probe)
		for _, signal := range report.Probe.Signals {
			assert.Equal(t, observability.ProbeInvalidConfiguration, signal.Status)
		}
		assert.NotContains(t, output, cliDoctorCanary)
		assert.NotContains(t, output, "example.invalid")
	})
	t.Run("disabled signals succeed without a receiver", func(t *testing.T) {
		clearDoctorEnvironment(t)
		for _, signal := range []string{"TRACES", "METRICS", "LOGS"} {
			t.Setenv("OTEL_"+signal+"_EXPORTER", "none")
		}
		report, _, err := executeDoctor(t, "doctor", "--probe")
		require.NoError(t, err)
		for _, signal := range report.Probe.Signals {
			assert.Equal(t, observability.ProbeDisabled, signal.Status)
		}
	})
}

func TestTelemetryDoctorArgumentErrorsAreSafe(t *testing.T) {
	for _, args := range [][]string{
		{"doctor", cliDoctorCanary}, {"doctor", "--timeout", cliDoctorCanary},
		{"doctor", "--timeout", "0s"}, {"doctor", "--timeout", "2m"}, {"doctor", "--" + cliDoctorCanary},
	} {
		command := NewCommandTelemetry()
		var output bytes.Buffer
		command.SetOut(&output)
		command.SetErr(&output)
		command.SetArgs(args)
		err := command.Execute()
		require.ErrorIs(t, err, errDoctorArguments)
		assert.NotContains(t, output.String(), cliDoctorCanary)
		assert.NotContains(t, err.Error(), cliDoctorCanary)
		assert.Contains(t, output.String(), "invalid arguments")
	}
}

func executeDoctor(t *testing.T, args ...string) (doctorReport, string, error) {
	t.Helper()
	command := NewCommandTelemetry()
	var output, diagnostics bytes.Buffer
	command.SetOut(&output)
	command.SetErr(&diagnostics)
	command.SetArgs(args)
	err := command.ExecuteContext(context.Background())
	var report doctorReport
	require.NoError(t, json.Unmarshal(output.Bytes(), &report), "doctor emits one valid JSON report")
	return report, output.String() + diagnostics.String(), err
}

func clearDoctorEnvironment(t *testing.T) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(key, "OTEL_") {
			t.Setenv(key, "")
		}
	}
}
