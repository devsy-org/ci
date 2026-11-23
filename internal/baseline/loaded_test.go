package baseline

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

const goListFlag = "--go-list"

func TestDirectivesLoadedFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	stream := fmt.Sprintf(`{"Dir":%q,"GoFiles":["regular.go"],"CgoFiles":["cgo.go"],
"TestGoFiles":["unit_test.go"],"XTestGoFiles":["external_test.go"]}
{"Dir":%q,"GoFiles":["w_windows.go"]}
`, root, filepath.Join(root, "sub"))
	list := fixtureFile(t, root, "golist.json", stream)
	for _, tc := range []struct {
		file, source string
		moved, fails bool
	}{
		{file: "regular.go", moved: true},
		{file: "cgo.go"},
		{file: "unit_test.go"},
		{file: "external_test.go"},
		{file: "sub/w_windows.go"},
		{file: "w_windows.go", fails: true},
		{file: "skip.go", source: "//go:build skip\n\n", fails: true},
		{file: "_impl/x.go", fails: true},
		{file: "renamed.go", moved: true, fails: true},
		{file: "sub/testdata/x.go"},
		{file: "sub/testdata-extra/x.go", fails: true},
	} {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			source := tc.source + sourcePkg
			directiveFixture(t, root, tc.file, source)
			diff := fmt.Sprintf(
				"--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@\n+%s\n",
				tc.file,
				strings.Count(source, "\n"),
				strings.ReplaceAll(strings.TrimSuffix(source, "\n"), "\n", "\n+"),
			)
			if tc.moved {
				diff = fmt.Sprintf(
					"diff --git a/old.go b/%s\nsimilarity index 100%%\nrename from old.go\nrename to %s\n",
					tc.file,
					tc.file,
				)
			}
			input := directiveTest{
				args: []string{rootFlag, root, guardFlag, goListFlag, list},
				diff: diff,
			}
			if tc.fails {
				want := fmt.Sprintf("%q 1: not loaded by any GOOS build\n", tc.file) + failed
				assertDirectives(t, input, want, 1)
			} else {
				assertDirectives(t, input, passed, 0)
			}
		})
	}
}

func TestDirectivesUnlintedImports(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"Imports", "TestImports", "XTestImports"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			record := fmt.Sprintf(`{"Dir":%q,"ImportPath":"example.com/product","GoFiles":["x.go"],
%q:["example.com/product/testdata/x","example.com/product/_impl","example.com/product/other_testdata"]}`,
				root, field)
			list := fixtureFile(t, root, "imports.json", record+"\n"+record)
			input := directiveTest{args: []string{rootFlag, root, guardFlag, goListFlag, list}}
			const want = `"example.com/product" imports unlinted code "example.com/product/_impl"
"example.com/product" imports unlinted code "example.com/product/testdata/x"
Lint directives failed. Violations: 2.
`
			assertDirectives(t, input, want, 1)
		})
	}
}

func TestDirectiveGoListErrors(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	malformed := fixtureFile(t, root, "malformed.json", "{}\n{")
	args := []string{rootFlag, root, guardFlag, goListFlag}
	assertDirectiveError(
		t,
		directiveTest{args: append(args, malformed)},
		"read go list stream: decode package",
	)
	assertDirectiveError(
		t,
		directiveTest{args: append(args, filepath.Join(root, "missing"))},
		"open go list stream",
	)
}
