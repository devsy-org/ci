package baseline

import (
	"strings"
	"testing"
)

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
