package prsize

import (
	"context"
	"errors"
	"fmt"
	"os"
)

type Options struct {
	BaseRef    string
	Limit      int
	ReportOnly bool
	Repo       string
}

func Run(ctx context.Context, opts Options, positional int) int {
	if opts.BaseRef == "" || opts.Limit < 0 || positional != 0 {
		fmt.Fprintln(os.Stderr, "pr-size: expected --base-ref and a nonnegative --limit")
		return 2
	}
	code, err := run(ctx, opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, "pr-size:", err)
	}
	return code
}

func run(ctx context.Context, opts Options) (int, error) {
	result, err := inspect(gitRunner(ctx, opts.Repo), opts.BaseRef)
	if err != nil {
		return 2, err
	}
	passed, message, err := verdict(
		result.totals[production].lines,
		opts.Limit,
		func() (bool, error) {
			return readException(os.Getenv("GITHUB_EVENT_PATH"), os.ReadFile)
		},
	)
	if err != nil {
		return 2, err
	}
	report := renderReport(result, message, passed)
	if _, err := os.Stdout.WriteString(report); err != nil {
		return 2, fmt.Errorf("write size report: %w", err)
	}
	if err := writeSummary(os.Getenv("GITHUB_STEP_SUMMARY"), report); err != nil {
		return 2, err
	}
	if !passed && !opts.ReportOnly {
		return 1, nil
	}
	return 0, nil
}

func writeSummary(path, report string) error {
	if path == "" {
		return nil
	}
	//nolint:gosec // The runner supplies the step summary path; the tool only appends to it.
	summary, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open GitHub step summary: %w", err)
	}
	_, err = summary.WriteString(report)
	if err := errors.Join(err, summary.Close()); err != nil {
		return fmt.Errorf("write GitHub step summary: %w", err)
	}
	return nil
}
