package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// profile is the parsed JSON verification profile covering service identity,
// dashboards, compose and Helm sections.
type profile struct {
	Version    int              `json:"version"`
	Service    serviceProfile   `json:"service"`
	Dashboards dashboardProfile `json:"dashboards"`
	Compose    composeProfile   `json:"compose"`
	Helm       helmProfile      `json:"helm"`
}

// serviceProfile names the service and namespace under verification.
type serviceProfile struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// dashboardProfile selects dashboard paths, expected metric names and queue
// operation, plus optional panel ID overrides.
type dashboardProfile struct {
	Paths             []string       `json:"paths"`
	CacheMetric       string         `json:"cache_metric"`
	QueueCountMetric  string         `json:"queue_count_metric"`
	QueueBucketMetric string         `json:"queue_bucket_metric"`
	QueueOperation    string         `json:"queue_operation"`
	Panels            map[string]int `json:"panels,omitempty"`
}

// composeProfile selects compose files, roles and fixture environment for
// collector verification.
type composeProfile struct {
	Base             []string          `json:"base"`
	Monitor          string            `json:"monitor"`
	Collector        string            `json:"collector"`
	Roles            []string          `json:"roles"`
	ApplicationRoles []string          `json:"application_roles"`
	FixtureEnv       map[string]string `json:"fixture_env"`
	AssetsDir        string            `json:"assets_dir"`
}

// helmProfile selects chart paths, values files, render script and output,
// port, worker definitions and optional production trace settings.
type helmProfile struct {
	ChartDir        string                  `json:"chart_dir"`
	BaseValues      string                  `json:"base_values"`
	ServiceValues   string                  `json:"service_values"`
	CollectorValues string                  `json:"collector_values"`
	RenderScript    string                  `json:"render_script"`
	RenderOutput    string                  `json:"render_output"`
	Port            int                     `json:"port"`
	Migrator        bool                    `json:"migrator"`
	Sidekick        bool                    `json:"sidekick"`
	Workers         []workerProfile         `json:"workers"`
	ProductionTrace *productionTraceProfile `json:"production_trace,omitempty"`
}

// productionTraceProfile describes the verified HTTPS OTLP endpoint and its
// credential plumbing for production trace checks.
type productionTraceProfile struct {
	Endpoint      string `json:"endpoint"`
	HeaderName    string `json:"header_name"`
	CredentialEnv string `json:"credential_env"`
	SecretName    string `json:"secret_name"`
	SecretKey     string `json:"secret_key"`
	RemoteKey     string `json:"remote_key"`
}

// workerProfile maps a worker's values key to its name suffix.
type workerProfile struct {
	ValuesKey string `json:"values_key"`
	Suffix    string `json:"suffix"`
}

var (
	identity   = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	metric     = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]{0,127}$`)
	operation  = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
	chartKey   = regexp.MustCompile(`^[a-z][a-zA-Z0-9]{0,63}$`)
	envKey     = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,127}$`)
	headerName = regexp.MustCompile(`^[A-Za-z0-9-]{1,128}$`)
	secretKey  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,252}$`)
)

// parseProfile decodes at most 1 MiB of strict JSON, rejecting unknown fields,
// trailing data, wrong versions, and out-of-bounds names, metrics, panels,
// roles, ports, workers, endpoints, secrets and environment entries with a
// single generic error. Panel overrides must remain unique.
func parseProfile(data []byte) (profile, error) {
	var p profile
	invalid := errors.New("invalid verification profile; check the documented version, fields and bounded values")
	if len(data) > 1<<20 {
		return p, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return p, invalid
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return p, invalid
	}
	if p.Version != 1 || !identity.MatchString(p.Service.Name) || !identity.MatchString(p.Service.Namespace) {
		return p, invalid
	}
	if len(p.Dashboards.Paths) != 2 || !metric.MatchString(p.Dashboards.CacheMetric) || !metric.MatchString(p.Dashboards.QueueCountMetric) || !metric.MatchString(p.Dashboards.QueueBucketMetric) || !operation.MatchString(p.Dashboards.QueueOperation) {
		return p, invalid
	}
	panels := map[string]int{"requests": 2, "errors": 3, "database_total": 5, "request_rate": 6, "latency": 7, "database_rate": 9, "traces": 10, "full_logs": 11, "error_ratio": 12, "freshness": 13, "age": 14, "database_latency": 18, "database_errors": 19, "outbound_latency": 20, "outbound_errors": 21, "runtime_memory": 22, "runtime_allocations": 23, "runtime_goroutines": 24, "cache_rate": 25, "cache_ratio": 26, "outbound_rate": 33, "request_summary": 34, "browser_intake": 35, "worker_rate": 36, "worker_latency": 37}
	for key, id := range p.Dashboards.Panels {
		if _, ok := panels[key]; !ok || id < 1 || id > 100000 {
			return p, invalid
		}
		panels[key] = id
	}
	panelIDs := map[int]bool{}
	for _, id := range panels {
		if panelIDs[id] {
			return p, invalid
		}
		panelIDs[id] = true
	}

	if len(p.Compose.Base) == 0 || len(p.Compose.Base) > 8 || len(p.Compose.Roles) == 0 || len(p.Compose.Roles) > 32 || len(p.Compose.ApplicationRoles) == 0 {
		return p, invalid
	}
	roles := map[string]bool{}
	for _, role := range p.Compose.Roles {
		if !identity.MatchString(role) || roles[role] {
			return p, invalid
		}
		roles[role] = true
	}
	seen := map[string]bool{}
	for _, role := range p.Compose.ApplicationRoles {
		if !roles[role] || seen[role] {
			return p, invalid
		}
		seen[role] = true
	}
	if p.Helm.Port < 1 || p.Helm.Port > 65535 || len(p.Helm.Workers) > 16 {
		return p, invalid
	}
	if trace := p.Helm.ProductionTrace; trace != nil {
		endpoint, err := url.Parse(trace.Endpoint)
		if err != nil || len(trace.Endpoint) > 2048 || endpoint.Scheme != "https" || endpoint.Hostname() == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" ||
			!headerName.MatchString(trace.HeaderName) || !envKey.MatchString(trace.CredentialEnv) || !identity.MatchString(trace.SecretName) || !secretKey.MatchString(trace.SecretKey) ||
			trace.RemoteKey == "" || len(trace.RemoteKey) > 1024 || strings.ContainsAny(trace.RemoteKey, "\x00\r\n") {
			return p, invalid
		}
	}
	keys, suffixes := map[string]bool{}, map[string]bool{}
	for _, worker := range p.Helm.Workers {
		if !chartKey.MatchString(worker.ValuesKey) || !identity.MatchString(worker.Suffix) || keys[worker.ValuesKey] || suffixes[worker.Suffix] {
			return p, invalid
		}
		keys[worker.ValuesKey] = true
		suffixes[worker.Suffix] = true
	}
	if len(p.Compose.FixtureEnv) > 32 {
		return p, invalid
	}
	for key, value := range p.Compose.FixtureEnv {
		if !envKey.MatchString(key) || len(value) > 256 || strings.ContainsAny(value, "\x00\r\n") {
			return p, invalid
		}
	}
	return p, nil
}

// All host paths are relative and must resolve inside the chosen root. Output
// paths may not exist yet; their existing ancestors are checked as well.
func safePath(root, path string, exists bool, directory bool) error {
	invalid := errors.New("profile asset path is missing, invalid, or outside the host root")
	canonicalRoot, rootErr := filepath.EvalSymlinks(root)
	if rootErr != nil {
		return invalid
	}
	root = canonicalRoot
	if path == "" || filepath.IsAbs(path) || strings.ContainsAny(path, "\\\x00\r\n") || filepath.Clean(path) != path || path == "." || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return invalid
	}
	full := filepath.Join(root, path)
	stat, err := os.Stat(full)
	if (exists && err != nil) || (err == nil && stat.IsDir() != directory) {
		return invalid
	}
	probe := full
	for {
		resolved, resolveErr := filepath.EvalSymlinks(probe)
		if resolveErr == nil {
			relative, relErr := filepath.Rel(root, resolved)
			if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return invalid
			}
			return nil
		}
		if exists || !errors.Is(resolveErr, os.ErrNotExist) {
			return invalid
		}
		next := filepath.Dir(probe)
		if next == probe {
			return invalid
		}
		probe = next
	}
}

// validatePaths checks every profile-referenced file and directory against the
// host root, requiring existence for inputs and validating Helm value filenames
// before joining them under the chart directory to prevent traversal.
func validatePaths(root string, p profile) error {
	files := append([]string(nil), p.Dashboards.Paths...)
	files = append(files, p.Compose.Base...)
	files = append(files, p.Compose.Monitor, p.Compose.Collector, p.Helm.RenderScript)
	for _, path := range files {
		if err := safePath(root, path, true, false); err != nil {
			return err
		}
	}
	for _, path := range []string{p.Helm.ChartDir, p.Compose.AssetsDir} {
		if err := safePath(root, path, true, true); err != nil {
			return err
		}
	}
	// Validate the value filenames before joining, so a clean join cannot hide traversal.
	for _, path := range []string{p.Helm.BaseValues, p.Helm.ServiceValues, p.Helm.CollectorValues} {
		if err := safePath(filepath.Join(root, p.Helm.ChartDir), path, true, false); err != nil {
			return err
		}
	}
	return safePath(root, p.Helm.RenderOutput, false, false)
}
