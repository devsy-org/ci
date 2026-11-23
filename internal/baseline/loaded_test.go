package baseline

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	goListFlag = "--go-list"
	modDirFlag = "--module-dir"
	moduleFile = "go.mod"
	productMod = "module example.com/product\n"
	missingGo  = "not loaded by any GOOS build"
)

func TestDirectivesLoadedFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fixtureFile(t, root, moduleFile, productMod)
	directiveFixture(t, root, "sub/file", "")
	stream := fmt.Sprintf(
		`{"Match":["./..."],"Dir":%q,"GoFiles":["regular.go"],"CgoFiles":["cgo.go"],
"TestGoFiles":["unit_test.go"],"XTestGoFiles":["external_test.go"]}
{"Match":["./..."],"Dir":%q,"GoFiles":["w_windows.go"]}
`,
		root,
		filepath.Join(root, "sub"),
	)
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
				want := fmt.Sprintf("%q 1: %s\n", tc.file, missingGo) + failed
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
			fixtureFile(t, root, moduleFile, productMod)
			record := fmt.Sprintf(
				`{"Match":["./..."],"Dir":%q,"ImportPath":"example.com/product/sub","GoFiles":["x.go"],
"Module":{"Path":"example.com/product","Main":true},
%q:["example.com/product","example.com/product/pkg","example.com/product/loaded","example.com/product/v2"]}`,
				root,
				field,
			)
			list := fixtureFile(t, root, "imports.json", record+"\n"+record+`
{"Match":["./..."],"ImportPath":"example.com/product/loaded"}
{"ImportPath":"example.com/product/v2","DepOnly":true,"Module":{"Path":"example.com/product/v2"}}
{"ImportPath":"example.com/product/pkg","DepOnly":true,"Module":{"Path":"example.com/product","Main":true}}`)
			input := directiveTest{args: []string{rootFlag, root, guardFlag, goListFlag, list}}
			const want = `"example.com/product/sub" imports a package outside ./...: "example.com/product"
"example.com/product/sub" imports a package outside ./...: "example.com/product/pkg"
Lint directives failed. Violations: 2.
`
			assertDirectives(t, input, want, 1)
		})
	}
}

func TestDirectivePhysicalPaths(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	physical := filepath.Join(parent, "real", "repo")
	link := filepath.Join(parent, "alias")
	directiveFixture(t, parent, "real/repo/go.mod", productMod)
	fixtureFile(t, physical, "new.go", sourcePkg)
	makeDirectiveLink(t, filepath.Join(parent, "real"), link)
	aliased := filepath.Join(link, "repo")
	cases := []struct{ name, root, dir string }{
		{"physical root", physical, aliased},
		{"aliased root", aliased, physical},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			list := fixtureFile(
				t,
				t.TempDir(),
				"golist.json",
				fmt.Sprintf(`{"Match":["./..."],"Dir":%q,"GoFiles":["new.go"]}`, tc.dir),
			)
			input := directiveTest{
				args: []string{rootFlag, tc.root, guardFlag, goListFlag, list},
				diff: addedHunk + "+package main\n",
			}
			assertDirectives(t, input, passed, 0)
		})
	}
}

func TestDirectiveModuleDirectory(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fixtureFile(t, root, moduleFile, productMod)
	directiveFixture(t, root, "a/go.mod", "module example.com/a\n")
	directiveFixture(t, root, "b/go.mod", "module example.com/b\n")
	const file = "b/x.go"
	directiveFixture(t, root, file, sourcePkg+"var x int\n")
	list := fixtureFile(t, root, "golist.json", "")
	input := directiveTest{
		args: []string{rootFlag, root, guardFlag, goListFlag, list},
		diff: "--- a/b/x.go\n+++ b/b/x.go\n@@ -1,0 +2 @@\n+var x int\n",
	}
	const otherModuleFlag = "--other-modules"
	assertDirectives(
		t,
		input,
		`"b/x.go" 1: belongs to module "b", which this run does not lint`+"\n"+failed,
		1,
	)
	input.args = append(input.args, otherModuleFlag, otherModulesSkip)
	assertDirectives(t, input, passed, 0)
	input.args = append(input.args, modDirFlag, "b")
	assertDirectives(t, input, `"b/x.go" 1: `+missingGo+"\n"+failed, 1)
	input.args = append(input.args, modDirFlag, "./a/")
	assertDirectives(t, input, passed, 0)
	fixtureFile(t, root, file, sourcePkg+"//nolint\n")
	input.diff = strings.ReplaceAll(input.diff, "var x int", "//nolint")
	assertDirectives(t, input, `"b/x.go" 2: `+bare+"\n"+failed, 1)
}

func TestNestedRunRejectsRootEdit(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fixtureFile(t, root, moduleFile, productMod)
	fixtureFile(t, root, "new.go", sourcePkg)
	directiveFixture(t, root, "nested/go.mod", "module example.com/nested\n")
	input := directiveTest{
		args: []string{rootFlag, root, guardFlag, modDirFlag, "nested"},
		diff: addedHunk + "+package main\n",
	}
	assertDirectives(
		t,
		input,
		`"new.go" 1: belongs to module ".", which this run does not lint`+"\n"+failed,
		1,
	)
	input.args = append(input.args, "--other-modules", "warn")
	assertDirectiveError(t, input, "expected --other-modules to be fail or skip")
}

func TestDirectiveTestdataModule(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	const moduleDir = "fixtures/testdata/fixture"
	const file = moduleDir + "/x.go"
	fixtureFile(t, root, moduleFile, productMod)
	directiveFixture(t, root, moduleDir+"/go.mod", "module example.com/fixture\n")
	directiveFixture(t, root, file, sourcePkg)
	input := directiveTest{
		args: []string{rootFlag, root, guardFlag, goListFlag},
		diff: "--- /dev/null\n+++ b/" + file + "\n@@ -0,0 +1 @@\n+package main\n",
	}
	emptyList := fixtureFile(t, root, "empty.json", "")
	input.args = append(input.args, emptyList)
	assertDirectives(t, input, passed, 0)

	record := fmt.Sprintf(
		`{"ImportPath":"example.com/fixture/dep","Module":{"Path":"example.com/fixture","Dir":%q}}`,
		filepath.Join(root, moduleDir),
	)
	input.args[len(input.args)-1] = fixtureFile(t, root, "dependency.json", record)
	want := fmt.Sprintf(
		"%q 1: belongs to module %q, which this run does not lint\n",
		file,
		moduleDir,
	)
	assertDirectives(t, input, want+failed, 1)

	input.args[len(input.args)-1] = emptyList
	if err := os.MkdirAll(filepath.Join(root, "notes", "go.work"), 0o750); err != nil {
		t.Fatal(err)
	}
	assertDirectives(t, input, passed, 0)

	workspace := "go 1.26\n\nuse ../" + moduleDir + "\n"
	directiveFixture(t, root, "tools/go.work", workspace)
	assertDirectives(t, input, want+failed, 1)
}

func TestDirectiveAddedSymlink(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fixtureFile(t, root, moduleFile, productMod)
	directiveFixture(t, root, "testdata/impl/x.go", sourcePkg)
	makeDirectiveLink(t, "testdata/impl", filepath.Join(root, "source-link"))
	initDirectiveRepo(t, root)
	makeDirectiveLink(t, "testdata/impl", filepath.Join(root, "pkg"))
	directiveGit(t, root, exec.CommandContext(t.Context(), "git", "add", "-A"))
	input := directiveTest{args: []string{rootFlag, root, guardFlag}}
	input.diff = directiveGit(t, root, directiveDiffCommand(t, "no renames"))
	assertDirectives(t, input, "\"pkg\" 1: adds a symlink\n"+failed, 1)
	input.diff = directiveGit(t, root, directiveDiffCommand(t, copyCase))
	if !strings.Contains(input.diff, "copy to pkg") ||
		strings.Contains(input.diff, "new file mode") {
		t.Fatalf("expected a copy without a mode header: %s", input.diff)
	}
	assertDirectives(t, input, "\"pkg\" 1: adds a symlink\n"+failed, 1)
}

func TestDeletionOnlyLoadedCheck(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fixtureFile(t, root, moduleFile, productMod)
	fixtureFile(t, root, "old.go", sourcePkg)
	list := fixtureFile(t, root, "golist.json", "")
	input := directiveTest{
		args: []string{rootFlag, root, guardFlag, goListFlag, list},
		diff: "--- a/old.go\n+++ b/old.go\n@@ -2 +1,0 @@\n-var x int\n",
	}
	assertDirectives(t, input, `"old.go" 1: `+missingGo+"\n"+failed, 1)
	input.diff = strings.ReplaceAll(input.diff, "+++ b/old.go", "+++ /dev/null")
	assertDirectives(t, input, passed, 0)
}

func makeDirectiveLink(t *testing.T, target, name string) {
	t.Helper()
	if err := os.Symlink(target, name); err != nil {
		t.Fatal(err)
	}
}

func TestDirectiveGoListErrors(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	fixtureFile(t, root, moduleFile, productMod)
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
	assertDirectiveError(
		t,
		directiveTest{args: []string{rootFlag, root, guardFlag, modDirFlag, "../other"}},
		"expected --module-dir relative to --root",
	)
}
