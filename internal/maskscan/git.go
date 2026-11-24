package maskscan

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type gitFunc func(...string) (string, error)

func gitRunner(ctx context.Context, repo string) gitFunc {
	return func(args ...string) (string, error) {
		//nolint:gosec // Invoke Git directly with arguments built for specific subcommands.
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = repo
		output, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("git %q: %w", args, err)
		}
		return string(output), nil
	}
}

func addedDiff(git gitFunc, baseRef string) (string, error) {
	ref := "refs/remotes/origin/" + baseRef
	remote, err := git("show-ref", "--verify", "--hash", ref)
	if err != nil {
		return "", fmt.Errorf("remote-tracking ref %s unavailable: %w", ref, err)
	}
	base, err := git("merge-base", strings.TrimSpace(remote), "HEAD")
	if err != nil {
		return "", fmt.Errorf("merge base for %s unavailable; check local ref: %w", ref, err)
	}
	return git(
		"-c", "core.quotePath=false", "diff", "--no-ext-diff", "--no-textconv",
		"--no-color", "--text", "--src-prefix=a/", "--dst-prefix=b/", "--no-renames", "-U0",
		strings.TrimSpace(base), "HEAD", "--",
	)
}
