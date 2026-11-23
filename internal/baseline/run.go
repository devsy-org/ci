package baseline

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type options struct {
	command, lintJSON, baseline, baseBaseline, root string
	adopt                                           bool
}

const (
	checkCommand  = "check"
	updateCommand = "update"
	maxReported   = 200
)

// Run reports lint baseline violations as exit 1, and usage or I/O errors as exit 2.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		switch args[0] {
		case directivesCommand:
			return executeDirectives(args[1:], os.Stdin, stdout, stderr)
		case configCheckCommand:
			return executeConfigCheck(args[1:], stdout, stderr)
		}
	}
	return execute(args, stdout, stderr, os.Getenv("GITHUB_STEP_SUMMARY"))
}

func execute(args []string, stdout, stderr io.Writer, summaryPath string) int {
	opts, err := parseOptions(args)
	if err == nil {
		var code int
		code, err = run(opts, stdout, summaryPath)
		if err == nil {
			return code
		}
	}
	return runError(stderr, err)
}

func runError(stderr io.Writer, err error) int {
	message := strings.NewReplacer("\r", `\r`, "\n", `\n`).Replace(err.Error())
	_, _ = fmt.Fprintln(stderr, "lint-baseline:", message)
	return 2
}

func parseOptions(args []string) (options, error) {
	if len(args) == 0 || (args[0] != checkCommand && args[0] != updateCommand) {
		return options{}, errors.New("expected check or update (or directives)")
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
		flags.BoolVar(&opts.adopt, "adopt", false, "the merge base has no baseline")
	}
	if err := flags.Parse(args); err != nil {
		return opts, fmt.Errorf("parse flags: %w", err)
	}
	if opts.lintJSON == "" || opts.baseline == "" || flags.NArg() != 0 {
		return opts, errors.New("expected --lint-json and --baseline, with no positional arguments")
	}
	// Without a merge base baseline a PR could regenerate its own, so skipping
	// that rule must be an explicit choice.
	if command == checkCommand && opts.adopt == (opts.baseBaseline != "") {
		return opts, errors.New("check needs exactly one of --base-baseline or --adopt")
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
	if !opts.adopt {
		entries, err := readEntries(opts.baseBaseline, Parse)
		if err != nil {
			return 2, err
		}
		base = entries
	}
	problems := compare(input.current, input.baseline, base, !opts.adopt)
	if len(problems) == 0 {
		return 0, report(stdout, summaryPath, "", "Lint baseline passed: 0 violations.")
	}
	verdict := fmt.Sprintf("Lint baseline failed. Violations: %d.", len(problems))
	return 1, report(stdout, summaryPath, renderProblems(opts, problems), verdict)
}

func renderProblems(opts options, problems []problem) string {
	var log strings.Builder
	stale := false
	for i, p := range problems {
		if i < maxReported {
			fmt.Fprintf(
				&log,
				"%q %q %q: %s\n",
				p.entry.Linter,
				p.entry.File,
				p.entry.Scope,
				p.message,
			)
		}
		stale = stale || p.message == staleMessage
	}
	if len(problems) > maxReported {
		fmt.Fprintf(&log, "... and %d more\n", len(problems)-maxReported)
	}
	if stale {
		fmt.Fprintf(&log, "Fix: lint-baseline update --lint-json %s --baseline %s --root %s\n",
			shellArg(opts.lintJSON), shellArg(opts.baseline), shellArg(opts.root))
	}
	return log.String()
}

// shellArg quotes for bash with $'...', escaping every byte outside printable
// ASCII as \xNN so UTF-8 paths survive bash 3.2.
func shellArg(arg string) string {
	const safe = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_./:-"
	if arg != "" && strings.Trim(arg, safe) == "" {
		return arg
	}
	var quoted strings.Builder
	quoted.WriteString("$'")
	for _, c := range []byte(arg) {
		quoteByte(&quoted, c)
	}
	quoted.WriteByte('\'')
	return quoted.String()
}

func quoteByte(quoted *strings.Builder, c byte) {
	switch {
	case c == '\'' || c == '\\':
		quoted.WriteByte('\\')
		quoted.WriteByte(c)
	case c < ' ' || c > '~':
		fmt.Fprintf(quoted, `\x%02x`, c)
	default:
		quoted.WriteByte(c)
	}
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
	dir, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("open GitHub step summary: %w", err)
	}
	defer func() { _ = dir.Close() }()
	summary, err := dir.OpenFile(filepath.Base(path), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
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
	path = filepath.Clean(path)
	info, err := os.Lstat(path)
	if err == nil && !info.Mode().IsRegular() {
		err = errors.New("not a regular file")
	}
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	file, err := os.Open(path)
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
	err = errors.Join(Write(file, entries), file.Chmod(0o644), file.Close())
	if err != nil {
		return fmt.Errorf("prepare baseline: %w", err)
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("replace baseline %q: %w", path, err)
	}
	return nil
}
