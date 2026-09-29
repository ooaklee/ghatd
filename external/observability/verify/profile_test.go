package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func example(t *testing.T) profile {
	t.Helper()
	data, err := os.ReadFile("examples/example.json")
	if err != nil {
		t.Fatal(err)
	}
	p, err := parseProfile(data)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func encode(t *testing.T, p profile) []byte {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func assets(t *testing.T, p profile) string {
	t.Helper()
	root := t.TempDir()
	files := append([]string(nil), p.Dashboards.Paths...)
	files = append(files, p.Compose.Base...)
	files = append(files, p.Compose.Monitor, p.Compose.Collector, p.Helm.RenderScript)
	for _, name := range []string{p.Helm.BaseValues, p.Helm.ServiceValues, p.Helm.CollectorValues} {
		files = append(files, filepath.Join(p.Helm.ChartDir, name))
	}
	for _, path := range files {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, p.Compose.AssetsDir), 0o700); err != nil {
		t.Fatal(err)
	}
	return root
}
func TestProfileSupportsDifferentHostTopology(t *testing.T) {
	p := example(t)
	p.Service.Name = "another-api"
	p.Helm.Port = 9191
	p.Helm.Migrator = false
	p.Helm.Sidekick = true
	p.Helm.Workers = []workerProfile{{ValuesKey: "reportWorker", Suffix: "report-worker"}, {ValuesKey: "mailWorker", Suffix: "mail-worker"}}
	p.Dashboards.Panels = map[string]int{"worker_rate": 91, "worker_latency": 92}
	got, err := parseProfile(encode(t, p))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Helm.Workers) != 2 || got.Helm.Workers[1].Suffix != "mail-worker" || got.Dashboards.Panels["worker_rate"] != 91 {
		t.Fatal("topology or panel overrides lost")
	}
	if err = validatePaths(assets(t, p), got); err != nil {
		t.Fatal(err)
	}
}
func TestInvalidProfileDoesNotEchoValues(t *testing.T) {
	p := example(t)
	cases := [][]byte{[]byte(`{"private-token":"synthetic-private-canary"}`), []byte(`{"version": "synthetic-private-canary"}`), append(encode(t, p), []byte(` {}`)...)}
	p.Helm.Workers = append(p.Helm.Workers, p.Helm.Workers[0])
	cases = append(cases, encode(t, p))
	for _, data := range cases {
		_, err := parseProfile(data)
		if err == nil || strings.Contains(err.Error(), "synthetic-private-canary") {
			t.Fatalf("expected fixed validation error, got %v", err)
		}
	}
}
func TestProductionTraceProfileValidation(t *testing.T) {
	p := example(t)
	p.Helm.ProductionTrace = &productionTraceProfile{
		Endpoint: "https://traces.example.com/v1/traces", HeaderName: "x-example-team",
		CredentialEnv: "TRACE_API_KEY", SecretName: "example-telemetry",
		SecretKey: "api-key", RemoteKey: "/example/live/TRACE_API_KEY",
	}
	if _, err := parseProfile(encode(t, p)); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*productionTraceProfile){
		func(p *productionTraceProfile) { p.Endpoint = "https://private-canary@example.com/v1/traces" },
		func(p *productionTraceProfile) { p.Endpoint = "https://example.com?key=private-canary" },
		func(p *productionTraceProfile) { p.Endpoint = "http://example.com/v1/traces" },
		func(p *productionTraceProfile) { p.CredentialEnv = "private-canary" },
		func(p *productionTraceProfile) { p.HeaderName = "header\nprivate-canary" },
		func(p *productionTraceProfile) { p.SecretName = "" },
		func(p *productionTraceProfile) { p.SecretKey = "" },
		func(p *productionTraceProfile) { p.RemoteKey = "private-canary\n" },
	} {
		copy := *p.Helm.ProductionTrace
		mutate(&copy)
		bad := p
		bad.Helm.ProductionTrace = &copy
		if _, err := parseProfile(encode(t, bad)); err == nil || strings.Contains(err.Error(), "private-canary") {
			t.Fatal("expected fixed validation error for invalid production trace profile")
		}
	}
}

func TestAssetPathConfinement(t *testing.T) {
	p := example(t)
	root := assets(t, p)
	for _, path := range []string{"", "../outside", "a/../outside", "/private/path", "missing.yaml", "a\\b"} {
		if err := safePath(root, path, true, false); err == nil {
			t.Fatalf("accepted invalid path %q", path)
		}
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := safePath(root, "escape/output.yaml", false, false); err == nil {
		t.Fatal("accepted output through escaping symlink")
	}
	if err := safePath(root, "new/nested/output.yaml", false, false); err != nil {
		t.Fatal(err)
	}
	p.Helm.ServiceValues = "../service.yaml"
	if err := validatePaths(root, p); err == nil {
		t.Fatal("accepted traversal in chart value path")
	}
}
func TestValidationPrecedesToolExecution(t *testing.T) {
	p := example(t)
	root := assets(t, p)
	p.Compose.Monitor = "missing.yaml"
	path := filepath.Join(t.TempDir(), "profile.json")
	if err := os.WriteFile(path, encode(t, p), 0o600); err != nil {
		t.Fatal(err)
	}
	err := run(context.Background(), options{root: root, profile: path, suite: "all", python: "must-not-execute", timeout: time.Second}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "profile asset path") {
		t.Fatalf("validation did not fail before interpreter launch: %v", err)
	}
	if _, err = selectedSuites("private-canary"); err == nil || strings.Contains(err.Error(), "private-canary") {
		t.Fatal("unknown suite diagnostic must be fixed")
	}
}
func TestEmbeddedSuiteCompleteness(t *testing.T) {
	for _, suite := range suites {
		if _, err := sources.ReadFile("python/" + suite + ".py"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := sources.ReadFile("python/common.py"); err != nil {
		t.Fatal(err)
	}
}
func TestToolEnvironmentExcludesCallerSecrets(t *testing.T) {
	t.Setenv("GHATD_PRIVATE_CANARY", "not-for-tools")
	t.Setenv("ASDF_HELM_VERSION", "3.18.4")
	joined := strings.Join(toolEnvironment(), "\n")
	if strings.Contains(joined, "not-for-tools") || !strings.Contains(joined, "ASDF_HELM_VERSION=3.18.4") {
		t.Fatal("unsafe or incomplete tool environment")
	}
}
