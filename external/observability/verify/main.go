// Command verify runs shared observability checks against a host's local assets.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

//go:embed python/*.py
var sources embed.FS

var suites = []string{"collector-compose", "collector-render", "production-render", "dashboards", "production-logs"}

type options struct {
	root, profile, suite, python, foundation string
	timeout                                  time.Duration
}

func main() {
	var o options
	flag.StringVar(&o.root, "root", ".", "host repository root")
	flag.StringVar(&o.profile, "profile", "", "JSON verification profile")
	flag.StringVar(&o.suite, "suite", "all", "suite name or all")
	flag.StringVar(&o.python, "python", "python3", "Python interpreter with PyYAML installed")
	flag.StringVar(&o.foundation, "foundation-dir", "", "optional source checkout for Collector asset comparison")
	flag.DurationVar(&o.timeout, "timeout", 10*time.Minute, "deadline for each suite")
	flag.Parse()
	if err := run(context.Background(), o, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "observability verification:", err)
		os.Exit(1)
	}
}

func selectedSuites(name string) ([]string, error) {
	if name == "all" {
		return append([]string(nil), suites...), nil
	}
	for _, suite := range suites {
		if name == suite {
			return []string{suite}, nil
		}
	}
	return nil, errors.New("unknown suite; use collector-compose, collector-render, production-render, dashboards, production-logs, or all")
}

func run(ctx context.Context, o options, stdout, stderr io.Writer) error {
	selected, err := selectedSuites(o.suite)
	if err != nil {
		return err
	}
	if o.timeout <= 0 {
		return errors.New("timeout must be positive")
	}
	root, err := filepath.Abs(o.root)
	if err != nil {
		return errors.New("cannot resolve host root")
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return errors.New("host root is not accessible")
	}
	stat, err := os.Stat(root)
	if err != nil || !stat.IsDir() {
		return errors.New("host root must be a directory")
	}
	data, err := os.ReadFile(o.profile)
	if err != nil {
		return errors.New("cannot read verification profile")
	}
	p, err := parseProfile(data)
	if err != nil {
		return err
	}
	if err = validatePaths(root, p); err != nil {
		return err
	}
	if o.foundation != "" {
		o.foundation, err = filepath.Abs(o.foundation)
		if err != nil {
			return errors.New("cannot resolve foundation directory")
		}
		stat, err = os.Stat(o.foundation)
		if err != nil || !stat.IsDir() {
			return errors.New("foundation directory is not accessible")
		}
	}
	scratch, err := os.MkdirTemp("", "ghatd-observability-verify-")
	if err != nil {
		return errors.New("cannot create verification workspace")
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	entries, err := sources.ReadDir("python")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		content, readErr := sources.ReadFile("python/" + entry.Name())
		if readErr != nil {
			return readErr
		}
		if err = os.WriteFile(filepath.Join(scratch, entry.Name()), content, 0o600); err != nil {
			return err
		}
	}
	// Execute exactly the validated profile, even if its source changes mid-run.
	canonical, err := json.Marshal(p)
	if err != nil {
		return err
	}
	profile := filepath.Join(scratch, "profile.json")
	if err = os.WriteFile(profile, canonical, 0o600); err != nil {
		return err
	}
	for _, suite := range selected {
		if _, err = fmt.Fprintf(stdout, "Checking %s\n", suite); err != nil {
			return err
		}
		deadline, cancel := context.WithTimeout(ctx, o.timeout)
		args := []string{filepath.Join(scratch, suite+".py"), "--root", root, "--profile", profile}
		if o.foundation != "" && suite == "collector-compose" {
			args = append(args, "--foundation-dir", o.foundation)
		}
		cmd := exec.CommandContext(deadline, o.python, args...)
		cmd.Dir = root
		cmd.Env = toolEnvironment()
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		// Give fixture finally blocks a chance to remove their containers on timeout.
		cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) }
		cmd.WaitDelay = 5 * time.Second
		err = cmd.Run()
		timedOut := deadline.Err() != nil
		cancel()
		if timedOut {
			return fmt.Errorf("%s exceeded its deadline or was cancelled", suite)
		}
		if err != nil {
			return fmt.Errorf("%s failed; see the check output", suite)
		}
	}
	return nil
}

func toolEnvironment() []string {
	allowed := map[string]bool{"PATH": true, "HOME": true, "DOCKER_HOST": true, "DOCKER_CONTEXT": true, "DOCKER_CONFIG": true, "ASDF_HELM_VERSION": true, "TMPDIR": true, "SYSTEMROOT": true}
	var result []string
	for _, value := range os.Environ() {
		key, _, ok := strings.Cut(value, "=")
		if ok && allowed[key] {
			result = append(result, value)
		}
	}
	return append(result, "PYTHONDONTWRITEBYTECODE=1")
}
