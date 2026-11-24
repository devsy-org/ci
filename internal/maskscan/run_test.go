package maskscan

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	fixtureBranch = "main"
	summaryName   = "summary.md"
)

func TestRun(t *testing.T) {
	t.Parallel()
	dir := initFixture(t)
	tests := []struct {
		name       string
		base       string
		allow      []string
		reportOnly bool
		want       int
	}{
		{name: "reject a hit", base: fixtureBranch, want: 1},
		{name: "report a hit", base: fixtureBranch, reportOnly: true},
		{name: "allow a hit", base: fixtureBranch, allow: []string{"*.txt"}},
		{name: "missing base ref", base: "missing", want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stdout strings.Builder
			summaryDir := t.TempDir()
			opts := Options{BaseRef: tt.base, Repo: dir, ReportOnly: tt.reportOnly, Allow: tt.allow}
			code, err := run(t.Context(), opts, &stdout, filepath.Join(summaryDir, summaryName))
			if code != tt.want || (err != nil) != (tt.want == 2) {
				t.Fatalf("run = %d, %v; want code %d", code, err, tt.want)
			}
			if err != nil || len(tt.allow) > 0 {
				return
			}
			assertHitReport(t, stdout.String(), summaryDir)
		})
	}
}

func TestRunRequiresBaseRef(t *testing.T) {
	t.Parallel()
	if code := Run(t.Context(), Options{}, 0); code != 2 {
		t.Errorf("Run with no base ref = %d; want 2", code)
	}
}

func TestRenderLogEscapesAnnotation(t *testing.T) {
	t.Parallel()
	log := renderLog([]hit{{path: "a%,:\r\n.txt", line: 12}})
	const want = "::error file=a%25%2C%3A%0D%0A.txt,line=12::" + hitMessage
	if !strings.Contains(log, want) || strings.Count(log, "\n") != 3 {
		t.Errorf("log should contain the escaped annotation on its own line: %q", log)
	}
}

func assertHitReport(t *testing.T, stdout, summaryDir string) {
	t.Helper()
	const want = "with space.txt:2: " + hitMessage + "\n" +
		"::error file=with space.txt,line=2::" + hitMessage + "\n" +
		"FAIL: 1 added line(s) with a run of 6 or more asterisks.\n"
	if stdout != want {
		t.Errorf("unexpected stdout:\n%s", stdout)
	}
	root, err := os.OpenRoot(summaryDir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := root.Close(); err != nil {
			t.Error(err)
		}
	}()
	summary, err := root.ReadFile(summaryName)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []string{"Mask scan: 1", "`with space.txt`:2", "add the path to `allow`"} {
		if !strings.Contains(string(summary), item) {
			t.Errorf("summary missing %q:\n%s", item, summary)
		}
	}
}

func initFixture(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is unavailable")
	}
	dir := t.TempDir()
	git := gitRunner(t.Context(), dir)
	gitOutput(t, git, "init", "-b", fixtureBranch)
	const name = "with space.txt"
	writeFixture(t, dir, name, "original\n")
	base := commitFixture(t, git, "")
	gitOutput(t, git, "update-ref", "refs/remotes/origin/"+fixtureBranch, base)
	writeFixture(t, dir, name, "original\nfixture "+strings.Repeat("*", 6)+"\n")
	commitFixture(t, git, base)
	return dir
}

func gitOutput(t *testing.T, git gitFunc, args ...string) string {
	t.Helper()
	out, err := git(args...)
	if err != nil {
		t.Fatalf("git %q: %v", args, err)
	}
	return strings.TrimSpace(out)
}

func commitFixture(t *testing.T, git gitFunc, parent string) string {
	t.Helper()
	gitOutput(t, git, "add", ".")
	tree := gitOutput(t, git, "write-tree")
	commit := "tree " + tree + "\n"
	if parent != "" {
		commit += "parent " + parent + "\n"
	}
	const stamp = "Fixture <fixture@example.invalid> 1 +0000"
	commit += fmt.Sprintf("author %s\ncommitter %s\n\nTest snapshot\n", stamp, stamp)
	dir := t.TempDir()
	writeFixture(t, dir, "commit", commit)
	hash := gitOutput(t, git, "hash-object", "-w", "-t", "commit", filepath.Join(dir, "commit"))
	gitOutput(t, git, "update-ref", "HEAD", hash)
	return hash
}

func writeFixture(t *testing.T, dir, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}
