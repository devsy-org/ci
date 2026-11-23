package baseline

import (
	"reflect"
	"strings"
	"testing"
)

func TestCollectAddedLines(t *testing.T) {
	t.Parallel()
	const patch = "diff --git a/binary.go b/binary.go\nBinary files a/binary.go and b/binary.go differ\n" +
		"diff --git a/one.go b/one.go\nindex ba7..f01 100644\n--- a/one.go\n+++ b/one.go\n" +
		"@@ -1,3 +1,3 @@ func first()\n context\n-old\n+new\n context\n" +
		"@@ -8 +8,2 @@\n-old\n+new\n+more\n" +
		`diff --git "a/::notice::\303\251\nx.go" "b/::notice::\303\251\nx.go"` + "\nnew file mode 100644\n" +
		"--- /dev/null\n" + `+++ "b/::notice::\303\251\nx.go"` + "\n@@ -0,0 +1 @@\n" +
		"+new\n\\ No newline at end of file\n" +
		"diff --git a/with space/go.mod b/with space/go.mod\nnew file mode 100644\nindex 000..e69\n" +
		"diff --git a/deleted.go b/deleted.go\n--- a/deleted.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-removed\n"
	want := map[string]*fileChange{
		"one.go":                 {lines: addedLines{2: true, 8: true, 9: true}},
		"::notice::\u00e9\nx.go": {lines: addedLines{1: true}, created: true},
		"with space/go.mod":      {lines: addedLines{}, created: true},
	}
	got, err := collectAddedLines(strings.NewReader(patch))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("collectAddedLines = %v, %v; want %v", got, err, want)
	}
}

func TestCollectAddedLinesUnifiedCreation(t *testing.T) {
	t.Parallel()
	const patch = "--- /dev/null\n+++ b/sub/go.work\n@@ -0,0 +1 @@\n+go 1.26.0\n"
	got, err := collectAddedLines(strings.NewReader(patch))
	want := map[string]*fileChange{"sub/go.work": {lines: addedLines{1: true}, created: true}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("collectAddedLines = %v, %v; want %v", got, err, want)
	}
}

func TestCollectAddedLinesMalformed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, patch string }{
		{"malformed hunk", "--- a/old.go\n+++ b/old.go\n@@ -1 +bogus @@\n+new\n"},
		{"truncated hunk", "--- /dev/null\n+++ b/new.go\n@@ -0,0 +1,2 @@\n+new\n"},
		{"unexpected text", "::warning::not a diff\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := collectAddedLines(strings.NewReader(tc.patch)); err == nil {
				t.Fatal("collectAddedLines accepted a malformed diff")
			}
		})
	}
}
