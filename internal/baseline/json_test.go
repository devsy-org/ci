package baseline

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
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
			"Pos":{"Filename":"source.go","Line":17}}
	],"Report":{"Linters":[]}}`
	got, err := FromJSON(strings.NewReader(input), "testdata")
	if err != nil {
		t.Fatal(err)
	}
	const source = "source.go"
	const outer = "Outer"
	const unused = "unused"
	want := []Entry{
		{nestif, source, outer, 8},
		{nestif, source, outer + "@2", 4},
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
revive:argument-limit	big.go	Big	6
revive:cognitive-complexity	big.go	Big	300
revive:cyclomatic	big.go	Big	121
revive:function-length:lines	big.go	Big	272
revive:function-length:lines	res.go	L	22
revive:function-length:statements	big.go	Big	152
revive:function-length:statements	res.go	L	22
revive:function-result-limit	res.go	R	3
revive:max-control-nesting	big.go	Big	30
`
	want, err := Parse(strings.NewReader(baseline))
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, Entry{nestif, "big.go", "Big", 6})
	for rank := 2; rank <= 30; rank++ {
		want = append(want, Entry{nestif, "big.go", fmt.Sprintf("Big@%d", rank), 6})
	}
	slices.SortFunc(want, compareEntries)
	if !slices.Equal(got, want) {
		t.Errorf("FromJSON() = %#v, want %#v", got, want)
	}
}

func TestFromJSONInvalid(t *testing.T) {
	t.Parallel()

	cases := []struct{ name, input, message string }{
		{"malformed", `{"Issues":[}`, "decode lint JSON"},
		{"missing issues", `{}`, "requires an Issues array"},
		{"null issues", `{"Issues":null}`, "requires an Issues array"},
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

func TestFromJSONEmptyIssues(t *testing.T) {
	t.Parallel()

	got, err := FromJSON(strings.NewReader(`{"Issues":[]}`), "testdata")
	if err != nil || len(got) != 0 {
		t.Errorf("FromJSON() = (%v, %v), want no entries or error", got, err)
	}
}

func TestReadIssuesSizeLimit(t *testing.T) {
	t.Parallel()

	const report = `{"Issues":[]}`
	if _, err := readIssues(strings.NewReader(report), int64(len(report))); err != nil {
		t.Errorf("readIssues() at limit: %v", err)
	}
	_, err := readIssues(strings.NewReader(report+" "), int64(len(report)))
	if err == nil || !strings.Contains(err.Error(), "lint JSON exceeds") {
		t.Errorf("readIssues() above limit = %v, want size error", err)
	}
}

func TestFromJSONPaths(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	const source = "source.go"
	cases := []struct {
		name, path string
		reject     bool
	}{
		{"relative", "./sub/../" + source, false},
		{"absolute in root", filepath.Join(root, source), false},
		{"absolute outside root", filepath.Join(filepath.Dir(root), "x.go"), true},
		{"relative outside root", "../x.go", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			const report = `{"Issues":[{"FromLinter":"unused","Pos":{"Filename":%q,"Line":15}}]}`
			got, err := FromJSON(strings.NewReader(fmt.Sprintf(report, tc.path)), "testdata")
			if tc.reject {
				if err == nil || !strings.Contains(err.Error(), "not local to the lint root") {
					t.Errorf("FromJSON() error = %v, want nonlocal path error", err)
				}
				return
			}
			want := []Entry{{"unused", source, "Outer", 1}}
			if err != nil || !slices.Equal(got, want) {
				t.Errorf("FromJSON() = (%v, %v), want %v", got, err, want)
			}
		})
	}
}

func TestFromJSONRepeatedInit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	const source = "init.go"
	if err := os.WriteFile(
		filepath.Join(root, source),
		[]byte("package p\nfunc init() {}\nfunc init() {}\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	const input = `{"Issues":[
		{"FromLinter":"cyclop","Text":"calculated cyclomatic complexity for function init is 9, max is 8",
			"Pos":{"Filename":"init.go","Line":3}},
		{"FromLinter":"cyclop","Text":"calculated cyclomatic complexity for function init is 12, max is 8",
			"Pos":{"Filename":"init.go","Line":2}}
	]}`
	got, err := FromJSON(strings.NewReader(input), root)
	want := []Entry{{cyclop, source, "init", 12}, {cyclop, source, "init#2", 9}}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("FromJSON() = (%v, %v), want %v", got, err, want)
	}
}

func TestFromJSONRanksMetricValues(t *testing.T) {
	t.Parallel()

	report := func(first, second int) string {
		const finding = `{"FromLinter":"nestif","Text":"` + "`if x`" + ` has complex nested blocks (complexity: %d)",` +
			`"Pos":{"Filename":"source.go","Line":17}}`
		return `{"Issues":[` + fmt.Sprintf(
			finding,
			first,
		) + "," + fmt.Sprintf(
			finding,
			second,
		) + `]}`
	}
	before, err := FromJSON(strings.NewReader(report(8, 6)), "testdata")
	if err != nil {
		t.Fatal(err)
	}
	after, err := FromJSON(strings.NewReader(report(5, 9)), "testdata")
	if err != nil {
		t.Fatal(err)
	}
	const source = "source.go"
	const outer = "Outer"
	wantBefore := []Entry{{nestif, source, outer, 8}, {nestif, source, outer + "@2", 6}}
	wantAfter := []Entry{{nestif, source, outer, 9}, {nestif, source, outer + "@2", 5}}
	if !slices.Equal(before, wantBefore) || !slices.Equal(after, wantAfter) {
		t.Errorf("FromJSON() = %v then %v, want %v then %v", before, after, wantBefore, wantAfter)
	}
}

func TestFromJSONRejectsUnknownMetricMessage(t *testing.T) {
	t.Parallel()

	for _, finding := range []string{
		`{"FromLinter":"cyclop","Text":"unknown format","Pos":{"Filename":"source.go","Line":17}}`,
		`{"FromLinter":"funlen","Text":"Function 'Outer' changed (75 > 60)","Pos":{"Filename":"source.go","Line":17}}`,
		`{"FromLinter":"revive","Text":"argument-limit: version 2","Pos":{"Filename":"source.go","Line":17}}`,
	} {
		_, err := FromJSON(strings.NewReader(`{"Issues":[`+finding+`]}`), "testdata")
		if err == nil || !strings.Contains(err.Error(), "unrecognized metric message") {
			t.Errorf("FromJSON(%s) error = %v, want unrecognized metric message", finding, err)
		}
	}
}

func TestFromJSONCyclopPackageAverage(t *testing.T) {
	t.Parallel()

	const input = `{"Issues":[{"FromLinter":"cyclop",` +
		`"Text":"the average complexity for the package fix is 41.006000, max is 1.000000",` +
		`"Pos":{"Filename":"source.go","Line":1}}]}`
	got, err := FromJSON(strings.NewReader(input), "testdata")
	if err != nil {
		t.Fatal(err)
	}
	want := []Entry{{"cyclop:package-average", "source.go", "-", 4100}}
	if !slices.Equal(got, want) {
		t.Errorf("FromJSON() = %v, want %v", got, want)
	}
}
