package baseline

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestFromJSON(t *testing.T) {
	t.Parallel()

	const input = `{"Issues":[
		{"FromLinter":"unused","Text":"outer 1","Pos":{"Filename":"source.go","Line":15,"Column":1}},
		{"FromLinter":"unused","Text":"outer 2","Pos":{"Filename":"source.go","Line":17}},
		{"FromLinter":"unused","Text":"package variable","Pos":{"Filename":"source.go","Line":3}},
		{"FromLinter":"unused","Text":"pointer receiver","Pos":{"Filename":"source.go","Line":8}},
		{"FromLinter":"unused","Text":"generic receiver","Pos":{"Filename":"source.go","Line":13}},
		{"FromLinter":"unused","Text":"non-Go file","Pos":{"Filename":"README.md","Line":8}},
		{"FromLinter":"unused","Text":"failed parse","Pos":{"Filename":"missing.go","Line":8}},
		{"FromLinter":"nestif","Text":"` + "`if x`" + ` has complex nested blocks (complexity: 8)",
			"Pos":{"Filename":"source.go","Line":17}},
		{"FromLinter":"nestif","Text":"` + "`if y`" + ` has complex nested blocks (complexity: 4)",
			"Pos":{"Filename":"source.go","Line":17}},
		{"FromLinter":"cyclop","Text":"unknown format","Pos":{"Filename":"source.go","Line":17}},
		{"FromLinter":"cyclop","Text":"unknown format","Pos":{"Filename":"source.go","Line":17}}
	],"Report":{"Linters":[]}}`
	got, err := FromJSON(strings.NewReader(input), "testdata")
	if err != nil {
		t.Fatal(err)
	}
	const source = "source.go"
	const outer = "Outer"
	const unused = "unused"
	want := []Entry{
		{"cyclop", source, outer, 2},
		{"nestif", source, outer, 8},
		{unused, "README.md", "-", 1},
		{unused, "missing.go", "-", 1},
		{unused, source, "-", 1},
		{unused, source, "Counter.Add", 1},
		{unused, source, outer, 2},
		{unused, source, "Pair.Reset", 1},
	}
	if !slices.Equal(got, want) {
		t.Errorf("FromJSON() = %#v, want %#v", got, want)
	}
}

func TestFromGolangCILintJSON(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/golangci.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err := FromJSON(bytes.NewReader(data), "testdata")
	if err != nil {
		t.Fatal(err)
	}
	const baseline = `cyclop	big.go	Big	121
funlen:statements	big.go	Big	152
funlen:statements	res.go	L	22
gocognit	big.go	Big	300
nestif	big.go	Big	6
revive:argument-limit	big.go	Big	6
revive:cognitive-complexity	big.go	Big	300
revive:cyclomatic	big.go	Big	121
revive:function-length	big.go	Big	272
revive:function-length	res.go	L	22
revive:function-result-limit	res.go	R	3
revive:max-control-nesting	big.go	Big	30
`
	want, err := Parse(strings.NewReader(baseline))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("FromJSON() = %#v, want %#v", got, want)
	}
}

func TestFromJSONInvalid(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, input, message string }{
		{"malformed", `{"Issues":[}`, "decode lint JSON"},
		{
			"tab in filename",
			`{"Issues":[{"FromLinter":"unused","Pos":{"Filename":"tab\tname.go"}}]}`,
			"lint finding 1",
		},
		{
			"newline in filename",
			`{"Issues":[{"FromLinter":"unused","Pos":{"Filename":"new\nline.go"}}]}`,
			"lint finding 1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := FromJSON(strings.NewReader(tc.input), "testdata")
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Errorf("FromJSON() error = %v, want %q", err, tc.message)
			}
		})
	}
}
