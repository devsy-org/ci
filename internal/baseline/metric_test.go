package baseline

import "testing"

const (
	cyclop = "cyclop"
	nestif = "nestif"
)

func TestMeasure(t *testing.T) {
	t.Parallel()

	const revive = "revive"
	const funlen = "funlen"
	const gocognit = "gocognit"
	cases := []struct {
		name, linter, message, wantLinter string
		wantValue                         int
	}{
		{
			cyclop,
			cyclop,
			"calculated cyclomatic complexity for function Big is 121, max is 8",
			cyclop,
			121,
		},
		{"lines", funlen, "Function 'Big' is too long (75 > 60)", "funlen:lines", 75},
		{
			"statements",
			funlen,
			"Function 'Big' has too many statements (152 > 40)",
			"funlen:statements",
			152,
		},
		{
			"gocognit",
			gocognit,
			"cognitive complexity 300 of func `Big` is high (> 15)",
			gocognit,
			300,
		},
		{nestif, nestif, "`if len(s) >= 4` has complex nested blocks (complexity: 3)", nestif, 3},
		{
			"arguments",
			revive,
			"argument-limit: maximum number of arguments per function exceeded; max 4 but got 6",
			"revive:argument-limit",
			6,
		},
		{
			"results",
			revive,
			"function-result-limit: maximum number of return results per function exceeded; max 3 but got 5",
			"revive:function-result-limit",
			5,
		},
		{
			"revive statements",
			revive,
			"function-length: maximum number of statements per function exceeded; max 40 but got 52",
			"revive:function-length:statements",
			52,
		},
		{
			"revive lines",
			revive,
			"function-length: maximum number of lines per function exceeded; max 60 but got 70",
			"revive:function-length:lines",
			70,
		},
		{
			"revive cognitive",
			revive,
			"cognitive-complexity: function Big has cognitive complexity 22 (> max enabled 15)",
			"revive:cognitive-complexity",
			22,
		},
		{
			"revive cyclomatic",
			revive,
			"cyclomatic: function (*Type).Big has cyclomatic complexity 19 (> max enabled 8)",
			"revive:cyclomatic",
			19,
		},
		{
			"revive nesting",
			revive,
			"max-control-nesting: control flow nesting exceeds 4",
			"revive:max-control-nesting",
			0,
		},
		{
			"other revive rule",
			revive,
			"early-return: early return recommended",
			"revive:early-return",
			0,
		},
		{"unknown revive message", revive, "argument-limit: version 2", "revive:argument-limit", 0},
		{"unknown funlen message", funlen, "Function 'Big' changed (75 > 60)", funlen, 0},
		{"unknown cyclop message", cyclop, "unexpected 42", cyclop, 0},
	}
	patterns := metricPatterns()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			linter, value := measure(patterns, tc.linter, tc.message)
			if linter != tc.wantLinter || value != tc.wantValue {
				t.Errorf(
					"measure() = (%q, %d), want (%q, %d)",
					linter,
					value,
					tc.wantLinter,
					tc.wantValue,
				)
			}
		})
	}
}
