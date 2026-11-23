package baseline

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type options struct {
	command, lintJSON, baseline, baseBaseline, root string
}

const (
	checkCommand  = "check"
	updateCommand = "update"
)

// Run reports lint baseline violations as exit 1, and usage or I/O errors as exit 2.
func Run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseOptions(args)
	if err == nil {
		var code int
		code, err = run(opts, stdout, os.Getenv("GITHUB_STEP_SUMMARY"))
		if err == nil {
			return code
		}
	}
	message := strings.NewReplacer("\r", `\r`, "\n", `\n`).Replace(err.Error())
	_, _ = fmt.Fprintln(stderr, "lint-baseline:", message)
	return 2
}

func parseOptions(args []string) (options, error) {
	if len(args) == 0 || (args[0] != checkCommand && args[0] != updateCommand) {
		return options{}, errors.New("expected check or update")
	}
	return parseFlags(args[0], args[1:])
}

func parseFlags(command string, args []string) (options, error) {
	opts := options{command: command}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&opts.lintJSON, "lint-json", "", "golangci-lint JSON path")
	flags.StringVar(&opts.baseline, "baseline", "", "lint baseline path")
	flags.StringVar(&opts.root, "root", ".", "lint root directory")
	if command == checkCommand {
		flags.StringVar(&opts.baseBaseline, "base-baseline", "", "merge base baseline path")
	}
	if err := flags.Parse(args); err != nil {
		return opts, fmt.Errorf("parse flags: %w", err)
	}
	if opts.lintJSON == "" || opts.baseline == "" || flags.NArg() != 0 {
		return opts, errors.New("expected --lint-json and --baseline, with no positional arguments")
	}
	return opts, nil
}

func run(opts options, stdout io.Writer, summaryPath string) (int, error) {
	current, err := readEntries(
		opts.lintJSON,
		func(r io.Reader) ([]Entry, error) { return FromJSON(r, opts.root) },
	)
	if err != nil {
		return 2, err
	}
	if opts.command == updateCommand {
		return update(opts, stdout, summaryPath, current)
	}
	baseline, err := readEntries(opts.baseline, Parse)
	if err != nil {
		return 2, err
	}
	return check(opts, stdout, summaryPath, compareInputs{current, baseline})
}

// update creates a missing baseline from every current finding; the check's
// merge base rule still rejects added keys once a baseline exists on the base.
func update(opts options, stdout io.Writer, summaryPath string, current []Entry) (int, error) {
	baseline, err := readEntries(opts.baseline, Parse)
	created := errors.Is(err, fs.ErrNotExist)
	if err != nil && !created {
		return 2, err
	}
	result := shrink(baseline, current)
	verdict := fmt.Sprintf(
		"Lint baseline updated: %d removed, %d lowered.",
		result.removed,
		result.lowered,
	)
	if created {
		result.entries = current
		verdict = fmt.Sprintf("Lint baseline created: %d entries.", len(current))
	}
	if err := replaceBaseline(opts.baseline, result.entries); err != nil {
		return 2, err
	}
	return 0, report(stdout, summaryPath, "", verdict)
}

type compareInputs struct{ current, baseline []Entry }

func check(opts options, stdout io.Writer, summaryPath string, input compareInputs) (int, error) {
	var base []Entry
	checkBase := opts.baseBaseline != ""
	if checkBase {
		entries, err := readEntries(opts.baseBaseline, Parse)
		if err != nil {
			return 2, err
		}
		base = entries
	}
	problems := compare(input.current, input.baseline, base, checkBase)
	if len(problems) == 0 {
		return 0, report(stdout, summaryPath, "", "Lint baseline passed: 0 violations.")
	}
	verdict := fmt.Sprintf("Lint baseline failed. Violations: %d.", len(problems))
	return 1, report(stdout, summaryPath, renderProblems(opts, problems), verdict)
}

func renderProblems(opts options, problems []problem) string {
	var log strings.Builder
	stale := false
	for _, p := range problems {
		fmt.Fprintf(&log, "%q %q %q: %s\n", p.entry.Linter, p.entry.File, p.entry.Scope, p.message)
		stale = stale || p.message == staleMessage
	}
	if stale {
		fmt.Fprintf(&log, "Fix: lint-baseline update --lint-json %s --baseline %s --root %s\n",
			shellArg(opts.lintJSON), shellArg(opts.baseline), shellArg(opts.root))
	}
	return log.String()
}

func shellArg(arg string) string {
	const safe = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./:-"
	if arg != "" && strings.Trim(arg, safe) == "" {
		return arg
	}
	quoted := strconv.QuoteToASCII(arg)
	return "$'" + strings.ReplaceAll(quoted[1:len(quoted)-1], "'", `\'`) + "'"
}

func report(stdout io.Writer, summaryPath, log, verdict string) error {
	if _, err := io.WriteString(stdout, log+verdict+"\n"); err != nil {
		return fmt.Errorf("write lint baseline report: %w", err)
	}
	return writeSummary(summaryPath, verdict)
}

func writeSummary(path, verdict string) error {
	if path == "" {
		return nil
	}
	//nolint:gosec // The runner supplies the step summary path; the tool only appends to it.
	summary, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open GitHub step summary: %w", err)
	}
	_, err = summary.WriteString("### Lint baseline\n\n" + verdict + "\n\n")
	if err := errors.Join(err, summary.Close()); err != nil {
		return fmt.Errorf("write GitHub step summary: %w", err)
	}
	return nil
}

func readEntries(path string, decode func(io.Reader) ([]Entry, error)) ([]Entry, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	entries, err := decode(file)
	if err := errors.Join(err, file.Close()); err != nil {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}
	return entries, nil
}

func replaceBaseline(path string, entries []Entry) error {
	path = filepath.Clean(path)
	file, err := os.CreateTemp(filepath.Dir(path), ".lint-baseline-*")
	if err != nil {
		return fmt.Errorf("create temporary baseline: %w", err)
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if err := errors.Join(Write(file, entries), file.Close()); err != nil {
		return fmt.Errorf("prepare baseline: %w", err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replace baseline %q: %w", path, err)
	}
	return nil
}
