package baseline

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
)

func TestRoundTrip(t *testing.T) {
	t.Parallel()

	const a = "a.go"
	want := []Entry{
		{cyclop, a, "-", 12},
		{cyclop, a, "A", 13},
		{cyclop, "z.go", "-", 20},
		{"unused", a, "-", 1},
	}
	input := slices.Clone(want)
	slices.Reverse(input)
	original := slices.Clone(input)
	var out strings.Builder
	if err := Write(&out, input); err != nil {
		t.Fatal(err)
	}
	const text = "cyclop\ta.go\t-\t12\ncyclop\ta.go\tA\t13\ncyclop\tz.go\t-\t20\nunused\ta.go\t-\t1\n"
	if out.String() != text {
		t.Errorf("Write() = %q, want %q", out.String(), text)
	}
	if !slices.Equal(input, original) {
		t.Errorf("Write() changed input: %#v", input)
	}
	got, err := Parse(strings.NewReader(out.String()))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, want) {
		t.Errorf("Parse() = %#v, want %#v", got, want)
	}
}

func TestParseInvalid(t *testing.T) {
	t.Parallel()

	const first = "cyclop\ta.go\t-\t12\n"
	const line1 = "baseline line 1:"
	const line2 = "baseline line 2:"
	const four = "expected four"
	const canonical = "canonical decimal"
	cases := []struct {
		name, input, line, message string
	}{
		{"blank line", "\n", line1, four},
		{"missing field", "unused\ta.go\t1\n", line1, four},
		{"extra field", "unused\ta.go\tf\t1\textra\n", line1, four},
		{"empty scope", "unused\ta.go\t\t1\n", line1, "must be nonempty"},
		{"noninteger", "unused\ta.go\t-\t1.2\n", line1, "parse value"},
		{"plus sign", "unused\ta.go\t-\t+5\n", line1, canonical},
		{"leading zeroes", "unused\ta.go\t-\t007\n", line1, canonical},
		{"overflow", "unused\ta.go\t-\t99999999999999999999\n", line1, "parse value"},
		{"zero", "unused\ta.go\t-\t0\n", line1, "must be positive"},
		{"negative", "unused\ta.go\t-\t-1\n", line1, "must be positive"},
		{"unsorted", first + "cyclop\tA.go\t-\t12\n", line2, "sorted and unique"},
		{"duplicate", first + "cyclop\ta.go\t-\t22\n", line2, "sorted and unique"},
		{"unterminated", first + "unused\ta.go\t-\t1", line2, "read newline-terminated entry"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse(strings.NewReader(tc.input))
			if err == nil || !strings.HasPrefix(err.Error(), tc.line) ||
				!strings.Contains(err.Error(), tc.message) {
				t.Errorf(
					"Parse() error = %v, want line %q and message %q",
					err,
					tc.line,
					tc.message,
				)
			}
		})
	}
}

func TestParseSizeLimit(t *testing.T) {
	t.Parallel()

	const suffix = "\ta.go\t-\t1\n"
	key := strings.Repeat("x", maxBaselineBytes-len(suffix))
	line := io.MultiReader(strings.NewReader(key), strings.NewReader(suffix))
	if _, err := Parse(line); err != nil {
		t.Errorf("Parse() at limit: %v", err)
	}
	line = io.MultiReader(strings.NewReader("x"), strings.NewReader(key), strings.NewReader(suffix))
	_, err := Parse(line)
	if err == nil || !strings.Contains(err.Error(), "baseline exceeds") {
		t.Errorf("Parse() above limit = %v, want size error", err)
	}
}

func TestParseReadError(t *testing.T) {
	t.Parallel()

	_, err := Parse(iotest.ErrReader(io.ErrUnexpectedEOF))
	if !errors.Is(err, io.ErrUnexpectedEOF) || !strings.HasPrefix(err.Error(), "baseline line 1:") {
		t.Errorf("Parse() error = %v, want unexpected EOF on line 1", err)
	}
}

func TestWriteInvalid(t *testing.T) {
	t.Parallel()

	const unused = "unused"
	const file = "a.go"
	const delimiters = "no tabs or newlines"
	valid := Entry{unused, file, "-", 1}
	cases := []struct {
		name    string
		entries []Entry
		message string
	}{
		{"file tab", []Entry{{unused, "a\tb.go", "-", 1}}, delimiters},
		{"scope tab", []Entry{{unused, file, "A\tB", 1}}, delimiters},
		{"scope newline", []Entry{{unused, file, "A\nB", 1}}, delimiters},
		{"duplicate key", []Entry{valid, valid}, "duplicate key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var out strings.Builder
			err := Write(&out, tc.entries)
			if err == nil || !strings.Contains(err.Error(), tc.message) {
				t.Errorf("Write() error = %v, want %q", err, tc.message)
			}
			if out.Len() != 0 {
				t.Errorf("Write() wrote invalid baseline: %q", out.String())
			}
		})
	}
}
