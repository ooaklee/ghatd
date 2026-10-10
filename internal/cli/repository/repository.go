package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/ooaklee/ghatd/external/logger"
	"go.uber.org/zap"
)

var errMissingSource = errors.New("repository: source is required")
var errMissingDestination = errors.New("repository: destination is required")

var (
	githubSSHRegex   = regexp.MustCompile(`^(?:ssh://)?git@github\.com[:/]([^/]+/[^/]+?)(?:\.git)?$`)
	githubHTTPSRegex = regexp.MustCompile(`^https?://github\.com/([^/]+/[^/]+?)(?:\.git)?$`)
	githubPlainRegex = regexp.MustCompile(`^github\.com/([^/]+/[^/]+?)(?:\.git)?$`)
	shortRepoRegex   = regexp.MustCompile(`^([^/]+/[^/]+)$`)
)

// Runner abstracts executable lookup and command execution so tests can
// substitute a fake runner.
type Runner interface {
	// LookPath resolves file to an executable path, mirroring exec.LookPath
	// semantics for the Runner abstraction that supports fake runners in tests.
	LookPath(file string) (string, error)
	// RunCommand executes the named command with args under ctx for the Runner
	// abstraction, forwarding output to this process's streams and returning any
	// run error.
	RunCommand(ctx context.Context, name string, args ...string) error
}

// defaultRunner is the Runner implementation backed by the real os/exec
// package.
type defaultRunner struct{}

// LookPath resolves an executable via the process PATH using exec.LookPath.
func (defaultRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

// RunCommand executes the command with the caller's context, forwarding stdout
// and stderr to this process's streams; it returns only the run error.
func (defaultRunner) RunCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

var runner Runner = defaultRunner{}

// SetRunner replaces the package runner used for clones, restoring the default
// exec-backed runner when nil.
func SetRunner(r Runner) {
	if r == nil {
		runner = defaultRunner{}
		return
	}
	runner = r
}

// CloneRequest describes one repository clone: source location, destination
// path, optional branch and whether submodules are included.
type CloneRequest struct {
	Source            string
	Destination       string
	Branch            string
	RecurseSubmodules bool
}

// Clone trims and validates source and destination, then clones with gh for
// GitHub sources when available, falling back to git; if both GitHub attempts
// fail both errors are joined. Missing arguments return typed errors.
func Clone(ctx context.Context, req CloneRequest) error {
	logger := logger.AcquireOperationFrom(ctx, "internal/cli/repository", "clone")
	req.Source = strings.TrimSpace(req.Source)
	req.Destination = strings.TrimSpace(req.Destination)
	req.Branch = strings.TrimSpace(req.Branch)

	if strings.TrimSpace(req.Source) == "" {
		logger.Warn("cli-repository-clone-missing-source")
		return errMissingSource
	}
	if strings.TrimSpace(req.Destination) == "" {
		logger.Warn("cli-repository-clone-missing-destination", zap.String("source", normaliseSource(req.Source)))
		return errMissingDestination
	}

	logger.Info("cli-repository-clone-started", zap.String("source", normaliseSource(req.Source)), zap.Bool("github-source", isGitHubSource(req.Source)), zap.Bool("branch-set", req.Branch != ""), zap.Bool("recurse-submodules", req.RecurseSubmodules))
	if isGitHubSource(req.Source) {
		if _, err := runner.LookPath("gh"); err == nil {
			if err := cloneWithGH(ctx, req); err == nil {
				logger.Info("cli-repository-clone-completed", zap.String("tool", "gh"), zap.String("source", normaliseSource(req.Source)))
				return nil
			} else if gitErr := cloneWithGit(ctx, req); gitErr != nil {
				logger.Error("cli-repository-clone-failed", zap.String("source", normaliseSource(req.Source)), zap.Error(gitErr))
				return fmt.Errorf("repository: gh clone failed: %w; git clone fallback failed: %w", err, gitErr)
			}

			logger.Info("cli-repository-clone-completed", zap.String("tool", "git"), zap.String("source", normaliseSource(req.Source)))
			return nil
		}
	}

	if err := cloneWithGit(ctx, req); err != nil {
		logger.Error("cli-repository-clone-failed", zap.String("source", normaliseSource(req.Source)), zap.Error(err))
		return err
	}
	logger.Info("cli-repository-clone-completed", zap.String("tool", "git"), zap.String("source", normaliseSource(req.Source)))
	return nil
}

// cloneWithGH clones ownerRepo via the gh CLI, forwarding optional branch and
// submodule flags after a -- separator.
func cloneWithGH(ctx context.Context, req CloneRequest) error {
	logger := logger.AcquireOperationFrom(ctx, "internal/cli/repository", "clone-with-gh")
	ownerRepo := normaliseSource(req.Source)

	args := []string{"repo", "clone", ownerRepo, req.Destination}

	var gitFlags []string
	if req.Branch != "" {
		gitFlags = append(gitFlags, "--branch", req.Branch)
	}
	if req.RecurseSubmodules {
		gitFlags = append(gitFlags, "--recurse-submodules")
	}
	if len(gitFlags) > 0 {
		args = append(args, "--")
		args = append(args, gitFlags...)
	}

	if err := runner.RunCommand(ctx, "gh", args...); err != nil {
		logger.Error("cli-repository-gh-clone-command-failed", zap.String("source", ownerRepo), zap.Error(err))
		return fmt.Errorf("repository: gh clone failed: %w", err)
	}
	logger.Debug("cli-repository-gh-clone-command-completed", zap.String("source", ownerRepo))
	return nil
}

// cloneWithGit clones the source via the git CLI, expanding shorthand GitHub
// sources to https URLs and forwarding optional branch and submodule flags.
func cloneWithGit(ctx context.Context, req CloneRequest) error {
	logger := logger.AcquireOperationFrom(ctx, "internal/cli/repository", "clone-with-git")
	args := []string{"clone"}
	if req.Branch != "" {
		args = append(args, "--branch", req.Branch)
	}
	if req.RecurseSubmodules {
		args = append(args, "--recurse-submodules")
	}
	args = append(args, gitCloneSource(req.Source), req.Destination)

	if err := runner.RunCommand(ctx, "git", args...); err != nil {
		logger.Error("cli-repository-git-clone-command-failed", zap.String("source", normaliseSource(req.Source)), zap.Error(err))
		return fmt.Errorf("repository: git clone failed: %w", err)
	}
	logger.Debug("cli-repository-git-clone-command-completed", zap.String("source", normaliseSource(req.Source)))
	return nil
}

// normaliseSource reduces GitHub SSH, HTTPS, plain and shorthand source forms
// to owner/repo, trimming any .git suffix; other sources are returned trimmed.
func normaliseSource(source string) string {
	source = strings.TrimSpace(source)

	if m := githubSSHRegex.FindStringSubmatch(source); m != nil {
		return normaliseOwnerRepo(m[1])
	}
	if m := githubHTTPSRegex.FindStringSubmatch(source); m != nil {
		return normaliseOwnerRepo(m[1])
	}
	if m := githubPlainRegex.FindStringSubmatch(source); m != nil {
		return normaliseOwnerRepo(m[1])
	}
	if m := shortRepoRegex.FindStringSubmatch(source); m != nil {
		return normaliseOwnerRepo(m[1])
	}
	return source
}

// gitCloneSource expands plain or shorthand GitHub sources to a full https
// clone URL, leaving other sources as-is.
func gitCloneSource(source string) string {
	source = strings.TrimSpace(source)

	if githubPlainRegex.MatchString(source) || shortRepoRegex.MatchString(source) {
		return fmt.Sprintf("https://github.com/%s.git", normaliseSource(source))
	}

	return source
}

// normaliseOwnerRepo strips a trailing .git suffix from an owner/repo string.
func normaliseOwnerRepo(ownerRepo string) string {
	return strings.TrimSuffix(ownerRepo, ".git")
}

// isGitHubSource reports whether the source matches any known GitHub SSH,
// HTTPS, plain or shorthand form.
func isGitHubSource(source string) bool {
	source = strings.TrimSpace(source)
	return githubSSHRegex.MatchString(source) ||
		githubHTTPSRegex.MatchString(source) ||
		githubPlainRegex.MatchString(source) ||
		shortRepoRegex.MatchString(source)
}
