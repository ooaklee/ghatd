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

type Runner interface {
	LookPath(file string) (string, error)
	RunCommand(ctx context.Context, name string, args ...string) error
}

type defaultRunner struct{}

func (defaultRunner) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}

func (defaultRunner) RunCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

var runner Runner = defaultRunner{}

func SetRunner(r Runner) {
	if r == nil {
		runner = defaultRunner{}
		return
	}
	runner = r
}

type CloneRequest struct {
	Source            string
	Destination       string
	Branch            string
	RecurseSubmodules bool
}

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

func gitCloneSource(source string) string {
	source = strings.TrimSpace(source)

	if githubPlainRegex.MatchString(source) || shortRepoRegex.MatchString(source) {
		return fmt.Sprintf("https://github.com/%s.git", normaliseSource(source))
	}

	return source
}

func normaliseOwnerRepo(ownerRepo string) string {
	return strings.TrimSuffix(ownerRepo, ".git")
}

func isGitHubSource(source string) bool {
	source = strings.TrimSpace(source)
	return githubSSHRegex.MatchString(source) ||
		githubHTTPSRegex.MatchString(source) ||
		githubPlainRegex.MatchString(source) ||
		shortRepoRegex.MatchString(source)
}
