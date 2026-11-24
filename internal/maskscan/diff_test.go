package maskscan

import (
	"slices"
	"strings"
	"testing"
)

func TestScanDiff(t *testing.T) {
	t.Parallel()
	mask := strings.Repeat("*", 6)
	tests := []struct {
		name  string
		diff  string
		allow []string
		want  []hit
	}{
		{
			name: "number added lines across hunks and files",
			diff: patchHeader("one.txt") +
				"@@ -2 +2,2 @@\n-old\n+plain\n+" + mask + "\n" +
				"@@ -8 +9 @@\n-old\n+++ " + mask + "\n\\ No newline at end of file\n" +
				patchHeader("two.txt") +
				"@@ -0,0 +1 @@\n+" + strings.Repeat("*", 7) + "\n",
			want: []hit{{"one.txt", 3}, {"one.txt", 9}, {"two.txt", 1}},
		},
		{
			name: "ignore headers context removed lines and short runs",
			diff: patchHeader(mask+".txt") + "@@ -1,2 +1,3 @@ " + mask +
				"\n " + mask + "\n-" + mask + "\n+short " + strings.Repeat("*", 5) +
				"\n+" + mask + "\n",
			want: []hit{{mask + ".txt", 3}},
		},
		{
			name: "quoted unicode path with space",
			diff: "+++ \"b/spaced \\303\\251.txt\"\n@@ -0,0 +1 @@\n+" + mask + "\n",
			want: []hit{{"spaced \u00e9.txt", 1}},
		},
		{
			name: "keep a path git quoted in a way Go cannot unquote",
			diff: "+++ \"b/odd\\q.txt\"\n@@ -0,0 +1 @@\n+" + mask + "\n",
			want: []hit{{"odd\\q.txt\"", 1}},
		},
		{
			name: "deleted and binary files",
			diff: "+++ /dev/null\n@@ -1 +0,0 @@\n-" + mask + "\n" +
				"diff --git a/image.bin b/image.bin\nBinary files a/image.bin and b/image.bin differ\n",
		},
		{
			name: "allow glob and prefix",
			diff: maskedAddition("fixtures/sample.txt", mask) +
				maskedAddition("fixtures/deep/sample.txt", mask) +
				maskedAddition("docs/examples/deep/sample.txt", mask) +
				maskedAddition("docs/examples-extra/sample.txt", mask),
			allow: []string{"fixtures/*.txt", "docs/examples/"},
			want:  []hit{{"fixtures/deep/sample.txt", 1}, {"docs/examples-extra/sample.txt", 1}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := scanDiff(tt.diff, tt.allow); !slices.Equal(got, tt.want) {
				t.Errorf("scanDiff() = %v; want %v", got, tt.want)
			}
		})
	}
}

func maskedAddition(name, mask string) string {
	return patchHeader(name) + "@@ -0,0 +1 @@\n+" + mask + "\n"
}

func patchHeader(name string) string {
	return "diff --git a/" + name + " b/" + name + "\n--- a/" + name + "\n+++ b/" + name + "\n"
}
