package maskscan

import (
	"context"
	"fmt"
	"io"
	"os"
)

type Options struct {
	BaseRef    string
	Repo       string
	ReportOnly bool
	Allow      []string
}

func Run(ctx context.Context, opts Options, positional int) int {
	if opts.BaseRef == "" || positional != 0 {
		fmt.Fprintln(os.Stderr, "mask-scan: expected --base-ref and no positional arguments")
		return 2
	}
	code, err := run(ctx, opts, os.Stdout, os.Getenv("GITHUB_STEP_SUMMARY"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "mask-scan:", err)
	}
	return code
}

func run(ctx context.Context, opts Options, stdout io.Writer, summary string) (int, error) {
	diff, err := addedDiff(gitRunner(ctx, opts.Repo), opts.BaseRef)
	if err != nil {
		return 2, err
	}
	hits := scanDiff(diff, opts.Allow)
	if _, err := io.WriteString(stdout, renderLog(hits)); err != nil {
		return 2, fmt.Errorf("write mask report: %w", err)
	}
	if err := writeSummary(summary, hits); err != nil {
		return 2, err
	}
	if len(hits) > 0 && !opts.ReportOnly {
		return 1, nil
	}
	return 0, nil
}
