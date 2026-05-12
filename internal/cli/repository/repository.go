package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
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
	req.Source = strings.TrimSpace(req.Source)
	req.Destination = strings.TrimSpace(req.Destination)
	req.Branch = strings.TrimSpace(req.Branch)

	if strings.TrimSpace(req.Source) == "" {
		return errMissingSource
	}
	if strings.TrimSpace(req.Destination) == "" {
		return errMissingDestination
	}

	if isGitHubSource(req.Source) {
		if _, err := runner.LookPath("gh"); err == nil {
			if err := cloneWithGH(ctx, req); err == nil {
				return nil
			} else if gitErr := cloneWithGit(ctx, req); gitErr != nil {
				return fmt.Errorf("repository: gh clone failed: %w; git clone fallback failed: %w", err, gitErr)
			}

			return nil
		}
	}

	return cloneWithGit(ctx, req)
}

func cloneWithGH(ctx context.Context, req CloneRequest) error {
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
		return fmt.Errorf("repository: gh clone failed: %w", err)
	}
	return nil
}

func cloneWithGit(ctx context.Context, req CloneRequest) error {
	args := []string{"clone"}
	if req.Branch != "" {
		args = append(args, "--branch", req.Branch)
	}
	if req.RecurseSubmodules {
		args = append(args, "--recurse-submodules")
	}
	args = append(args, gitCloneSource(req.Source), req.Destination)

	if err := runner.RunCommand(ctx, "git", args...); err != nil {
		return fmt.Errorf("repository: git clone failed: %w", err)
	}
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
