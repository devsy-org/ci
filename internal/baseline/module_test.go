package baseline

import (
	"fmt"
	"strings"
	"testing"
)

func TestDirectiveWorkspaceEdits(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ kind, text string }{
		{"use", "./sub"},
		{"replace", "example.com/x => ./sub"},
		{"replace", "example.com/x => example.com/fork v1.0.0"},
	} {
		for _, block := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%s/%t", tc.kind, tc.text, block), func(t *testing.T) {
				t.Parallel()
				root := t.TempDir()
				line, added, suffix := 2, tc.kind+" "+tc.text, ""
				source := "go 1.26.9\n"
				if block {
					line, added, suffix = 3, "\t"+tc.text, ")\n"
					source += tc.kind + " (\n"
				}
				fixtureFile(t, root, "go.work", source+added+"\n"+suffix)
				diff := fmt.Sprintf(
					"--- a/go.work\n+++ b/go.work\n@@ -%d,0 +%d @@\n+%s\n",
					line-1,
					line,
					added,
				)
				input := directiveTest{args: []string{rootFlag, root, guardFlag}, diff: diff}
				want := fmt.Sprintf("\"go.work\" %d: adds a go.work %s directive\n", line, tc.kind)
				assertDirectives(t, input, want+failed, 1)
				input.args = append(input.args, "--allow-new-modules")
				assertDirectives(t, input, passed, 0)
			})
		}
	}
}

func TestDirectiveLocalReplacements(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, source, added string
		fails               bool
	}{
		{"relative", "replace example.com/lib => ./lib\n", "replace example.com/lib => ./lib", true},
		{"block", "replace (\n\texample.com/lib => \"../local lib\"\n)\n", "\texample.com/lib => \"../local lib\"", true},
		{"absolute", "replace example.com/lib => /lib\n", "replace example.com/lib => /lib", true},
		{
			"versioned", "replace example.com/lib => example.com/fork v1.0.0\n",
			"replace example.com/lib => example.com/fork v1.0.0", false,
		},
		{"unchanged", "replace example.com/lib => ./lib\n// comment\n", "// comment", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			const file = "b/go.mod"
			directiveFixture(t, root, file, productMod+tc.source)
			fixtureFile(t, root, moduleFile, productMod)
			line := "2"
			if strings.Contains(tc.source, "(\n") || tc.name == "unchanged" {
				line = "3"
			}
			input := directiveTest{
				args: []string{rootFlag, root, guardFlag, modDirFlag, "."},
				diff: "--- a/b/go.mod\n+++ b/b/go.mod\n@@ -1,0 +" + line + " @@\n+" + tc.added + "\n",
			}
			if tc.fails {
				assertDirectives(
					t,
					input,
					`"b/go.mod" `+line+": adds a local replace directive\n"+failed,
					1,
				)
			} else {
				assertDirectives(t, input, passed, 0)
			}
			input.args = append(input.args, "--allow-new-modules")
			assertDirectives(t, input, passed, 0)
		})
	}
}
