package repository

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type mockRunner struct {
	lookPathFunc    func(string) (string, error)
	runCommandFunc  func(ctx context.Context, name string, args ...string) error
	lookPathCalls   []string
	runCommandCalls []runCommandCall
}

type runCommandCall struct {
	Name string
	Args []string
}

func (m *mockRunner) LookPath(file string) (string, error) {
	m.lookPathCalls = append(m.lookPathCalls, file)
	if m.lookPathFunc != nil {
		return m.lookPathFunc(file)
	}
	return "", errors.New("not found")
}

func (m *mockRunner) RunCommand(ctx context.Context, name string, args ...string) error {
	m.runCommandCalls = append(m.runCommandCalls, runCommandCall{Name: name, Args: args})
	if m.runCommandFunc != nil {
		return m.runCommandFunc(ctx, name, args...)
	}
	return nil
}

func resetRunner() {
	runner = defaultRunner{}
}

func TestNormaliseSource(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "Success - short org/repo", input: "ooaklee/ghatd", want: "ooaklee/ghatd"},
		{name: "Success - github.com/org/repo", input: "github.com/ooaklee/ghatd", want: "ooaklee/ghatd"},
		{name: "Success - https github url", input: "https://github.com/ooaklee/ghatd", want: "ooaklee/ghatd"},
		{name: "Success - https github url with .git", input: "https://github.com/ooaklee/ghatd.git", want: "ooaklee/ghatd"},
		{name: "Success - git@ ssh url", input: "git@github.com:ooaklee/ghatd.git", want: "ooaklee/ghatd"},
		{name: "Success - ssh:// url", input: "ssh://git@github.com/ooaklee/ghatd.git", want: "ooaklee/ghatd"},
		{name: "Success - repository name with dot", input: "https://github.com/ooaklee/ghatd.detail.git", want: "ooaklee/ghatd.detail"},
		{name: "Success - short repo trims git suffix", input: "ooaklee/ghatd.git", want: "ooaklee/ghatd"},
		{name: "Success - unknown format passes through", input: "https://gitlab.com/org/repo", want: "https://gitlab.com/org/repo"},
		{name: "Success - unknown short passes through", input: "some/source/extra", want: "some/source/extra"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normaliseSource(tt.input)
			if got != tt.want {
				t.Fatalf("normaliseSource(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsGitHubSource(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "Success - short org/repo", input: "ooaklee/ghatd", want: true},
		{name: "Success - github.com/org/repo", input: "github.com/ooaklee/ghatd", want: true},
		{name: "Success - https with .git", input: "https://github.com/ooaklee/ghatd.git", want: true},
		{name: "Success - git@ ssh", input: "git@github.com:ooaklee/ghatd.git", want: true},
		{name: "Success - ssh://", input: "ssh://git@github.com/ooaklee/ghatd.git", want: true},
		{name: "Failure - gitlab", input: "https://gitlab.com/org/repo", want: false},
		{name: "Failure - extra path", input: "some/source/extra", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isGitHubSource(tt.input)
			if got != tt.want {
				t.Fatalf("isGitHubSource(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestClone_Validation(t *testing.T) {
	tests := []struct {
		name    string
		req     CloneRequest
		wantErr error
	}{
		{name: "Failure - empty source", req: CloneRequest{Source: "", Destination: "/tmp/dest"}, wantErr: errMissingSource},
		{name: "Failure - whitespace source", req: CloneRequest{Source: "   ", Destination: "/tmp/dest"}, wantErr: errMissingSource},
		{name: "Failure - empty destination", req: CloneRequest{Source: "org/repo", Destination: ""}, wantErr: errMissingDestination},
		{name: "Failure - whitespace destination", req: CloneRequest{Source: "org/repo", Destination: "  "}, wantErr: errMissingDestination},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := &mockRunner{}
			SetRunner(mr)
			defer resetRunner()

			err := Clone(context.Background(), tt.req)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Clone() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestClone_CommandSelection(t *testing.T) {
	tests := []struct {
		name          string
		req           CloneRequest
		ghAvailable   bool
		wantCmd       string
		wantArgsStart []string
	}{
		{
			name:          "Success - GitHub source prefers gh",
			req:           CloneRequest{Source: "ooaklee/ghatd", Destination: "/tmp/dest"},
			ghAvailable:   true,
			wantCmd:       "gh",
			wantArgsStart: []string{"repo", "clone", "ooaklee/ghatd", "/tmp/dest"},
		},
		{
			name:          "Success - GitHub HTTPS source prefers gh",
			req:           CloneRequest{Source: "https://github.com/ooaklee/ghatd", Destination: "/tmp/dest"},
			ghAvailable:   true,
			wantCmd:       "gh",
			wantArgsStart: []string{"repo", "clone", "ooaklee/ghatd", "/tmp/dest"},
		},
		{
			name:          "Success - GitHub SSH source prefers gh",
			req:           CloneRequest{Source: "git@github.com:ooaklee/ghatd.git", Destination: "/tmp/dest"},
			ghAvailable:   true,
			wantCmd:       "gh",
			wantArgsStart: []string{"repo", "clone", "ooaklee/ghatd", "/tmp/dest"},
		},
		{
			name:          "Success - GitHub source falls back to git when gh missing",
			req:           CloneRequest{Source: "ooaklee/ghatd", Destination: "/tmp/dest"},
			ghAvailable:   false,
			wantCmd:       "git",
			wantArgsStart: []string{"clone", "https://github.com/ooaklee/ghatd.git", "/tmp/dest"},
		},
		{
			name:          "Success - non-GitHub source uses git",
			req:           CloneRequest{Source: "https://gitlab.com/org/repo", Destination: "/tmp/dest"},
			ghAvailable:   true,
			wantCmd:       "git",
			wantArgsStart: []string{"clone", "https://gitlab.com/org/repo", "/tmp/dest"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := &mockRunner{
				lookPathFunc: func(file string) (string, error) {
					if file == "gh" && tt.ghAvailable {
						return "/usr/bin/gh", nil
					}
					return "", errors.New("not found")
				},
			}
			SetRunner(mr)
			defer resetRunner()

			err := Clone(context.Background(), tt.req)
			if err != nil {
				t.Fatalf("Clone() unexpected error: %v", err)
			}

			if len(mr.runCommandCalls) != 1 {
				t.Fatalf("expected 1 command call, got %d", len(mr.runCommandCalls))
			}

			call := mr.runCommandCalls[0]
			if call.Name != tt.wantCmd {
				t.Fatalf("command = %q, want %q", call.Name, tt.wantCmd)
			}

			if len(call.Args) < len(tt.wantArgsStart) {
				t.Fatalf("args too short: got %v, want at least %v", call.Args, tt.wantArgsStart)
			}

			for i, want := range tt.wantArgsStart {
				if call.Args[i] != want {
					t.Fatalf("args[%d] = %q, want %q", i, call.Args[i], want)
				}
			}
		})
	}
}

func TestClone_GitArguments(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		dest     string
		branch   string
		recurse  bool
		wantArgs []string
	}{
		{
			name:     "Success - git clone without branch or submodules",
			source:   "https://gitlab.com/org/repo",
			dest:     "/tmp/dest",
			branch:   "",
			recurse:  false,
			wantArgs: []string{"clone", "https://gitlab.com/org/repo", "/tmp/dest"},
		},
		{
			name:     "Success - git clone with branch",
			source:   "https://gitlab.com/org/repo",
			dest:     "/tmp/dest",
			branch:   "main",
			recurse:  false,
			wantArgs: []string{"clone", "--branch", "main", "https://gitlab.com/org/repo", "/tmp/dest"},
		},
		{
			name:     "Success - git clone with recurse submodules",
			source:   "https://gitlab.com/org/repo",
			dest:     "/tmp/dest",
			branch:   "",
			recurse:  true,
			wantArgs: []string{"clone", "--recurse-submodules", "https://gitlab.com/org/repo", "/tmp/dest"},
		},
		{
			name:     "Success - git clone with branch and recurse submodules",
			source:   "https://gitlab.com/org/repo",
			dest:     "/tmp/dest",
			branch:   "develop",
			recurse:  true,
			wantArgs: []string{"clone", "--branch", "develop", "--recurse-submodules", "https://gitlab.com/org/repo", "/tmp/dest"},
		},
		{
			name:     "Success - git fallback expands owner repo shorthand",
			source:   "ooaklee/ghatd",
			dest:     "/tmp/dest",
			branch:   "",
			recurse:  false,
			wantArgs: []string{"clone", "https://github.com/ooaklee/ghatd.git", "/tmp/dest"},
		},
		{
			name:     "Success - git fallback expands github.com shorthand",
			source:   "github.com/ooaklee/ghatd",
			dest:     "/tmp/dest",
			branch:   "",
			recurse:  false,
			wantArgs: []string{"clone", "https://github.com/ooaklee/ghatd.git", "/tmp/dest"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := &mockRunner{}
			SetRunner(mr)
			defer resetRunner()

			err := Clone(context.Background(), CloneRequest{
				Source:            tt.source,
				Destination:       tt.dest,
				Branch:            tt.branch,
				RecurseSubmodules: tt.recurse,
			})
			if err != nil {
				t.Fatalf("Clone() unexpected error: %v", err)
			}

			if len(mr.runCommandCalls) != 1 {
				t.Fatalf("expected 1 command call, got %d", len(mr.runCommandCalls))
			}

			got := mr.runCommandCalls[0].Args
			if !reflect.DeepEqual(got, tt.wantArgs) {
				t.Fatalf("args = %v, want %v", got, tt.wantArgs)
			}
		})
	}
}

func TestClone_GitHubArguments(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		dest     string
		branch   string
		recurse  bool
		wantArgs []string
	}{
		{
			name:     "Success - gh clone without extra flags",
			source:   "ooaklee/ghatd",
			dest:     "/tmp/dest",
			wantArgs: []string{"repo", "clone", "ooaklee/ghatd", "/tmp/dest"},
		},
		{
			name:     "Success - gh clone with branch",
			source:   "ooaklee/ghatd",
			dest:     "/tmp/dest",
			branch:   "main",
			wantArgs: []string{"repo", "clone", "ooaklee/ghatd", "/tmp/dest", "--", "--branch", "main"},
		},
		{
			name:     "Success - gh clone with recurse submodules",
			source:   "ooaklee/ghatd",
			dest:     "/tmp/dest",
			recurse:  true,
			wantArgs: []string{"repo", "clone", "ooaklee/ghatd", "/tmp/dest", "--", "--recurse-submodules"},
		},
		{
			name:     "Success - gh clone with branch and recurse submodules",
			source:   "ooaklee/ghatd",
			dest:     "/tmp/dest",
			branch:   "develop",
			recurse:  true,
			wantArgs: []string{"repo", "clone", "ooaklee/ghatd", "/tmp/dest", "--", "--branch", "develop", "--recurse-submodules"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := &mockRunner{
				lookPathFunc: func(file string) (string, error) {
					if file == "gh" {
						return "/usr/bin/gh", nil
					}
					return "", errors.New("not found")
				},
			}
			SetRunner(mr)
			defer resetRunner()

			err := Clone(context.Background(), CloneRequest{
				Source:            tt.source,
				Destination:       tt.dest,
				Branch:            tt.branch,
				RecurseSubmodules: tt.recurse,
			})
			if err != nil {
				t.Fatalf("Clone() unexpected error: %v", err)
			}

			if len(mr.runCommandCalls) != 1 {
				t.Fatalf("expected 1 command call, got %d", len(mr.runCommandCalls))
			}

			got := mr.runCommandCalls[0].Args
			if !reflect.DeepEqual(got, tt.wantArgs) {
				t.Fatalf("args = %v, want %v", got, tt.wantArgs)
			}
		})
	}
}

func TestClone_FailedCommand(t *testing.T) {
	runErr := errors.New("permission denied")
	mr := &mockRunner{
		runCommandFunc: func(ctx context.Context, name string, args ...string) error {
			return runErr
		},
	}
	SetRunner(mr)
	defer resetRunner()

	err := Clone(context.Background(), CloneRequest{
		Source:      "https://gitlab.com/org/repo",
		Destination: "/tmp/dest",
	})
	if err == nil {
		t.Fatal("Clone() expected error, got nil")
	}
	if !strings.Contains(err.Error(), "git clone failed") {
		t.Fatalf("Clone() error = %v, want error containing 'git clone failed'", err)
	}
	if !errors.Is(err, runErr) {
		t.Fatalf("Clone() error = %v, want wrapped %v", err, runErr)
	}
}

func TestClone_GitHubFailedCommand(t *testing.T) {
	runErr := errors.New("network error")
	mr := &mockRunner{
		lookPathFunc: func(file string) (string, error) {
			return "/usr/bin/gh", nil
		},
		runCommandFunc: func(ctx context.Context, name string, args ...string) error {
			return runErr
		},
	}
	SetRunner(mr)
	defer resetRunner()

	err := Clone(context.Background(), CloneRequest{
		Source:      "ooaklee/ghatd",
		Destination: "/tmp/dest",
	})
	if err == nil {
		t.Fatal("Clone() expected error, got nil")
	}
	for _, want := range []string{"gh clone failed", "git clone fallback failed"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Clone() error = %v, want error containing %q", err, want)
		}
	}
	if !errors.Is(err, runErr) {
		t.Fatalf("Clone() error = %v, want wrapped %v", err, runErr)
	}
	if len(mr.runCommandCalls) != 2 {
		t.Fatalf("expected gh and git command calls, got %d", len(mr.runCommandCalls))
	}
	if mr.runCommandCalls[0].Name != "gh" || mr.runCommandCalls[1].Name != "git" {
		t.Fatalf("commands = %v, want gh then git", mr.runCommandCalls)
	}
}

func TestClone_GitHubFallsBackToGitWhenGHFails(t *testing.T) {
	ghErr := errors.New("gh auth expired")
	mr := &mockRunner{
		lookPathFunc: func(file string) (string, error) {
			return "/usr/bin/gh", nil
		},
		runCommandFunc: func(ctx context.Context, name string, args ...string) error {
			if name == "gh" {
				return ghErr
			}
			return nil
		},
	}
	SetRunner(mr)
	defer resetRunner()

	err := Clone(context.Background(), CloneRequest{
		Source:      "ooaklee/ghatd",
		Destination: "/tmp/dest",
	})
	if err != nil {
		t.Fatalf("Clone() unexpected error after git fallback: %v", err)
	}
	if len(mr.runCommandCalls) != 2 {
		t.Fatalf("expected gh and git command calls, got %d", len(mr.runCommandCalls))
	}
	if mr.runCommandCalls[0].Name != "gh" || mr.runCommandCalls[1].Name != "git" {
		t.Fatalf("commands = %v, want gh then git", mr.runCommandCalls)
	}
}
