package baseline

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	hit       = `{"FromLinter":"unused","Pos":{"Filename":"source.go","Line":1}}`
	entry     = "unused\tsource.go\t-\t1\n"
	bigEntry  = "unused\tsource.go\t-\t2\n"
	emptyLint = `{"Issues":[]}`
	shrinkMsg = "baseline may only shrink"
	openError = `open "`
)

func TestCheck(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, findings, baseline string
		base                     *string
		want                     []string
		stale                    bool
	}{
		{name: "equal", findings: hit, baseline: entry, base: new(entry)},
		{name: "no base", findings: hit, baseline: entry},
		{name: "new", findings: hit, want: []string{"new finding"}},
		{name: "growing", findings: hit + "," + hit, baseline: entry, want: []string{"grew from 1 to 2"}},
		{
			name: "added to baseline", findings: hit, baseline: entry, base: new(""),
			want: []string{shrinkMsg},
		},
		{
			name: "raised in baseline", findings: hit + "," + hit, baseline: bigEntry, base: new(entry),
			want: []string{shrinkMsg},
		},
		{name: "removed from baseline", base: new(entry)},
		{name: "lowered in baseline", findings: hit, baseline: entry, base: new(bigEntry)},
		{name: "absent current finding", baseline: entry, want: []string{staleMessage}, stale: true},
		{name: "lower current value", findings: hit, baseline: bigEntry, want: []string{staleMessage}, stale: true},
		{
			name: "both baseline violations", findings: hit, baseline: bigEntry, base: new(entry),
			want: []string{shrinkMsg, staleMessage}, stale: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			args := fixtureArgs(t, checkCommand, `{"Issues":[`+tc.findings+`]}`, tc.baseline)
			if tc.base != nil {
				args = append(
					args,
					"--base-baseline",
					fixtureFile(t, filepath.Dir(args[2]), "base.tsv", *tc.base),
				)
			} else {
				args = append(args, "--adopt")
			}
			var stdout, stderr bytes.Buffer
			code := execute(args, &stdout, &stderr, "")
			want, wantCode := expectedCheck(args, tc.want, tc.stale)
			if code != wantCode || stdout.String() != want || stderr.Len() != 0 {
				t.Fatalf("code = %d, stdout = %q, stderr = %q; want code %d and stdout %q",
					code, stdout.String(), stderr.String(), wantCode, want)
			}
		})
	}
}

func expectedCheck(args, problems []string, stale bool) (string, int) {
	var want strings.Builder
	for _, problem := range problems {
		want.WriteString(`"unused" "source.go" "-": ` + problem + "\n")
	}
	if stale {
		want.WriteString("Fix: lint-baseline update --lint-json " + args[2] +
			" --baseline " + args[4] + " --root .\n")
	}
	if len(problems) == 0 {
		return "Lint baseline passed: 0 violations.\n", 0
	}
	fmt.Fprintf(&want, "Lint baseline failed. Violations: %d.\n", len(problems))
	return want.String(), 1
}

func TestCheckSortsAndQuotes(t *testing.T) {
	t.Parallel()
	const tricky = `{"FromLinter":"unused","Pos":{"Filename":"::warning::a\"file.go"}}`
	const other = "unused\tother.go\t-\t1\n"
	args := fixtureArgs(
		t,
		checkCommand,
		`{"Issues":[`+hit+`,`+tricky+`]}`,
		other+"unused\tremoved.go\t-\t1\n",
	)
	args = append(args, "--adopt", "--root", "dir's name\n::warning::")
	var stdout, stderr bytes.Buffer
	if code := execute(args, &stdout, &stderr, ""); code != 1 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	want := []string{
		`"unused" "::warning::a\"file.go" "-": new finding`,
		`"unused" "other.go" "-": stale baseline entry`,
		`"unused" "removed.go" "-": stale baseline entry`,
		`"unused" "source.go" "-": new finding`,
	}
	lines := strings.Split(stdout.String(), "\n")
	if len(lines) != 7 || !slices.Equal(lines[:4], want) ||
		!strings.HasSuffix(lines[4], `--root $'dir\'s name\x0a::warning::'`) {
		t.Fatalf("unexpected report: %q", stdout.String())
	}
}

func TestUpdate(t *testing.T) {
	t.Parallel()
	const finding = `{"FromLinter":"unused","Pos":{"Filename":"kept.go","Line":1}}`
	const added = `{"FromLinter":"unused","Pos":{"Filename":"new.go","Line":1}}`
	const kept = "unused\tkept.go\t-\t1\n"
	const removed = "unused\tremoved.go\t-\t1\n"
	args := fixtureArgs(t, updateCommand,
		`{"Issues":[`+finding+`,`+finding+`,`+added+`,`+hit+`]}`, kept+removed+bigEntry)
	var stdout, stderr bytes.Buffer
	if code := execute(args, &stdout, &stderr, ""); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if want := "Lint baseline updated: 1 removed, 1 lowered.\n"; stdout.String() != want {
		t.Fatalf("stdout = %q; want %q", stdout.String(), want)
	}
	data, err := os.ReadFile(filepath.Clean(args[4]))
	if err != nil || string(data) != kept+entry {
		t.Fatalf("updated baseline = %q, %v; want %q", data, err, kept+entry)
	}
	if _, err := Parse(bytes.NewReader(data)); err != nil {
		t.Fatalf("parse updated baseline: %v", err)
	}
}

func TestRunErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, content, want string
		flag                int
		missing             bool
	}{
		{name: "no command", want: "expected check or update"},
		{name: "missing JSON", flag: 2, missing: true, want: openError},
		{name: "malformed JSON", flag: 2, content: "{", want: "decode lint JSON"},
		{name: "missing baseline", flag: 4, missing: true, want: openError},
		{name: "malformed baseline", flag: 4, content: "invalid\n", want: "expected four tab-separated fields"},
		{name: "missing base", flag: 6, missing: true, want: openError},
		{name: "malformed base", flag: 6, content: "invalid\n", want: "expected four tab-separated fields"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var args []string
			if tc.flag != 0 {
				args = fixtureArgs(t, checkCommand, emptyLint, "")
				dir := filepath.Dir(args[2])
				args = append(args, "--base-baseline", fixtureFile(t, dir, "base.tsv", ""))
				if tc.missing {
					args[tc.flag] = filepath.Join(dir, "missing")
				} else {
					args[tc.flag] = fixtureFile(t, dir, "malformed", tc.content)
				}
			}
			var stdout, stderr bytes.Buffer
			if code := execute(args, &stdout, &stderr, ""); code != 2 ||
				!strings.Contains(stderr.String(), tc.want) ||
				stdout.Len() != 0 {
				t.Fatalf("code = %d, stdout = %q, stderr = %q; want code 2 and error containing %q",
					code, stdout.String(), stderr.String(), tc.want)
			}
		})
	}
}

func TestSummary(t *testing.T) {
	dir := t.TempDir()
	path := fixtureFile(t, dir, "summary.md", "Previous step\n\n")
	t.Setenv("GITHUB_STEP_SUMMARY", path)
	var stdout, stderr bytes.Buffer
	args := fixtureArgs(t, checkCommand, `{"Issues":[`+hit+`]}`, "")
	update := append([]string{updateCommand}, args[1:]...)
	args = append(args, "--adopt")
	if code := Run(args, &stdout, &stderr); code != 1 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if code := Run(update, &stdout, &stderr); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	data, err := os.ReadFile(filepath.Clean(path))
	want := "Previous step\n\n### Lint baseline\n\nLint baseline failed. Violations: 1.\n\n" +
		"### Lint baseline\n\nLint baseline updated: 0 removed, 0 lowered.\n\n"
	if err != nil || string(data) != want {
		t.Fatalf("summary = %q, %v; want %q", data, err, want)
	}
}

func fixtureArgs(t *testing.T, command, lintJSON, baseline string) []string {
	t.Helper()
	dir := t.TempDir()
	return []string{
		command, "--lint-json", fixtureFile(t, dir, "lint.json", lintJSON),
		"--baseline", fixtureFile(t, dir, "baseline.tsv", baseline),
	}
}

func fixtureFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUpdateCreatesMissingBaseline(t *testing.T) {
	t.Parallel()
	args := fixtureArgs(t, updateCommand, `{"Issues":[`+hit+`,`+hit+`]}`, "")
	if err := os.Remove(args[4]); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := execute(args, &stdout, &stderr, ""); code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	if want := "Lint baseline created: 1 entries.\n"; stdout.String() != want {
		t.Fatalf("stdout = %q; want %q", stdout.String(), want)
	}
	data, err := os.ReadFile(filepath.Clean(args[4]))
	if err != nil || string(data) != bigEntry {
		t.Fatalf("created baseline = %q, %v; want %q", data, err, bigEntry)
	}
}

func TestCheckNeedsBaseOrAdopt(t *testing.T) {
	t.Parallel()
	args := fixtureArgs(t, checkCommand, emptyLint, "")
	base := fixtureFile(t, filepath.Dir(args[2]), "base.tsv", "")
	for _, extra := range [][]string{nil, {"--adopt", "--base-baseline", base}} {
		var stdout, stderr bytes.Buffer
		if code := execute(append(slices.Clone(args), extra...), &stdout, &stderr, ""); code != 2 ||
			!strings.Contains(stderr.String(), "exactly one of --base-baseline or --adopt") {
			t.Fatalf("flags %q: code = %d, stderr = %q", extra, code, stderr.String())
		}
	}
}

func TestRejectsSymlinkedInput(t *testing.T) {
	t.Parallel()
	args := fixtureArgs(t, checkCommand, emptyLint, "")
	link := filepath.Join(filepath.Dir(args[2]), "link.tsv")
	if err := os.Symlink(args[4], link); err != nil {
		t.Fatal(err)
	}
	args[4] = link
	var stdout, stderr bytes.Buffer
	if code := execute(append(args, "--adopt"), &stdout, &stderr, ""); code != 2 ||
		!strings.Contains(stderr.String(), "not a regular file") {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
}

func TestReportCapsProblems(t *testing.T) {
	t.Parallel()
	issues := make([]string, maxReported+3)
	for i := range issues {
		issues[i] = fmt.Sprintf(`{"FromLinter":"unused","Pos":{"Filename":"f%03d.go","Line":1}}`, i)
	}
	lintJSON := `{"Issues":[` + strings.Join(issues, ",") + `]}`
	args := fixtureArgs(t, checkCommand, lintJSON, "unused\tzzz.go\t-\t1\n")
	var stdout, stderr bytes.Buffer
	if code := execute(append(args, "--adopt"), &stdout, &stderr, ""); code != 1 {
		t.Fatalf("code = %d, stderr = %q", code, stderr.String())
	}
	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if len(lines) != maxReported+3 || lines[maxReported] != "... and 4 more" ||
		!strings.HasPrefix(lines[maxReported+1], "Fix: ") ||
		strings.Count(stdout.String(), "Fix: ") != 1 {
		t.Fatalf("got %d lines, tail %q", len(lines), lines[len(lines)-3:])
	}
}

func TestShellArgRoundTrips(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	for _, arg := range []string{"plain.tsv", "café.tsv", "it's \\ \"x\"\n\t::warning::", ""} {
		cmd := exec.CommandContext(t.Context(), "bash", "-c", `eval "printf %s $QUOTED"`)
		cmd.Env = append(os.Environ(), "QUOTED="+shellArg(arg))
		out, err := cmd.Output()
		if err != nil || string(out) != arg {
			t.Fatalf("shellArg(%q) = %s; bash printed %q, %v", arg, shellArg(arg), out, err)
		}
	}
}
