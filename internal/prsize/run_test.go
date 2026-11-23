package prsize

import (
	"os"
	"strings"
	"testing"
)

func TestRunKeepsBinaryPathsInSummary(t *testing.T) {
	git, dir := initFixture(t)
	base := commitBase(t, git)
	const binary = "summary-only.png"
	writeFixture(t, dir, binary, "\x00pixels\n")
	writeFixture(t, dir, "counted.txt", "one line\n")
	gitOK(t, git, "add", ".")
	commitFixture(t, git, base)

	t.Chdir(t.TempDir())
	const path = "summary.md"
	t.Setenv("GITHUB_STEP_SUMMARY", path)
	t.Setenv("GITHUB_EVENT_PATH", "")
	var stdout strings.Builder
	code, err := run(t.Context(), Options{BaseRef: "main", Repo: dir}, &stdout)
	if code != 1 || err != nil {
		t.Fatalf("run = %d, %v; want exit 1", code, err)
	}
	const log = `| Class | Files | Changed lines | Counted |
| --- | ---: | ---: | --- |
| Generated | 0 | 0 | no |
| Tests | 0 | 0 | no |
| Docs | 0 | 0 | no |
| Config | 0 | 0 | no |
| Production | 2 | 1 | yes |

FAIL: 1 production lines (limit 0).
`
	if stdout.String() != log {
		t.Errorf("stdout should only contain the table and verdict:\n%s", stdout.String())
	}

	summary, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const suffix = "\nBinary files not counted:\n\n- `" + binary + "`\n"
	between, ok := strings.CutPrefix(string(summary), log)
	if !ok || !strings.Contains(between, "To request an exception") ||
		!strings.HasSuffix(between, suffix) {
		t.Errorf("summary should place the binary list after the verdict and hint:\n%s", summary)
	}
}
