package prsize

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestReadException(t *testing.T) {
	t.Parallel()

	const eventStart = `{"pull_request":{"labels":[{"name":"size-exception"}],"body":`
	cases := []struct {
		name    string
		payload string
		want    bool
		bad     bool
	}{
		{
			name:    "both conditions",
			payload: eventStart + `"Details\nSize exception:  01234567890123456789  "}}`,
			want:    true,
		},
		{
			name:    "short reason",
			payload: eventStart + `"Size exception:       0123456789012345678  "}}`,
		},
		{
			name:    "invisible characters",
			payload: eventStart + `"Size exception: abc` + strings.Repeat(`\u200b`, 20) + `def"}}`,
		},
		{
			name:    "must start line",
			payload: eventStart + `" Size exception: 01234567890123456789"}}`,
		},
		{name: "null body", payload: eventStart + `null}}`},
		{
			name:    "reason without label",
			payload: `{"pull_request":{"body":"Size exception: 01234567890123456789"}}`,
		},
		{name: "malformed", payload: "{", bad: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := readException("event.json", func(path string) ([]byte, error) {
				if path != "event.json" {
					t.Fatalf("read path = %q, want event.json", path)
				}
				return []byte(tc.payload), nil
			})
			if got != tc.want || (err != nil) != tc.bad {
				t.Errorf("readException = %t, %v; want %t, error: %t", got, err, tc.want, tc.bad)
			}
		})
	}
}

func TestReadExceptionIOError(t *testing.T) {
	t.Parallel()

	unreadable := errors.New("unreadable")
	_, err := readException("event.json", func(string) ([]byte, error) { return nil, unreadable })
	if !errors.Is(err, unreadable) {
		t.Errorf("readException = %v, want unreadable error", err)
	}
}

func TestVerdict(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		lines     int
		exception bool
		passed    bool
		message   string
	}{
		{
			name:    "below limit",
			lines:   399,
			passed:  true,
			message: "PASS: 399 production lines (limit 400).",
		},
		{
			name:    "at limit",
			lines:   400,
			passed:  true,
			message: "PASS: 400 production lines (limit 400).",
		},
		{name: "over limit", lines: 401, message: "FAIL: 401 production lines (limit 400)."},
		{
			name: "exception", lines: 401, exception: true, passed: true,
			message: "PASS: 401 production lines (limit 400), size exception accepted.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			passed, message, err := verdict(tc.lines, 400, func() (bool, error) {
				return tc.exception, nil
			})
			if err != nil || passed != tc.passed || message != tc.message {
				t.Errorf(
					"verdict = %t, %q, %v; want %t, %q",
					passed,
					message,
					err,
					tc.passed,
					tc.message,
				)
			}
		})
	}
}

func TestVerdictUnreadableEvent(t *testing.T) {
	t.Parallel()
	unreadable := errors.New("unreadable")
	read := func() (bool, error) { return false, unreadable }
	passed, _, err := verdict(400, 400, read)
	if err != nil || !passed {
		t.Errorf("at limit: passed = %t, error = %v; want pass without reading event", passed, err)
	}
	if _, _, err := verdict(401, 400, read); !errors.Is(err, unreadable) {
		t.Errorf("above limit: error = %v; want unreadable event error", err)
	}
}

func TestRenderFailure(t *testing.T) {
	t.Parallel()

	got := renderReport(
		inspection{totals: totals{production: {files: 1, lines: 401}}},
		"FAIL",
		false,
	)
	for _, want := range []string{
		"| Config | 0 | 0 | no |", "| Production | 1 | 401 | yes |",
		"\nFAIL\n", "add the `size-exception` label", "`Size exception:`", "20 characters",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("report does not contain %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Binary files not counted:") {
		t.Errorf("report includes an empty binary section:\n%s", got)
	}
}

func TestRenderSkippedBinary(t *testing.T) {
	t.Parallel()

	result := inspection{skippedBinary: []string{"z.png", "a`<br>\n\x1b\xff.png"}}
	got := renderReport(result, "PASS", true)
	want := "| Production | 0 | 0 | yes |\n\nBinary files not counted:\n\n" +
		"- `a\\x60<br>\\n\\x1b\\xff.png`\n- `z.png`\n\nPASS\n"
	if !strings.HasSuffix(got, want) {
		t.Errorf("report = %q, want suffix %q", got, want)
	}
}

func TestRenderSkippedBinaryCap(t *testing.T) {
	t.Parallel()

	var result inspection
	for i := 52; i > 0; i-- {
		result.skippedBinary = append(result.skippedBinary, fmt.Sprintf("image-%02d.png", i))
	}
	got := renderReport(result, "PASS", true)
	if count := strings.Count(got, "\n- `"); count != 50 {
		t.Errorf("report lists %d binary files, want 50", count)
	}
	if !strings.Contains(got, "- `image-50.png`\n- and 2 more\n") ||
		strings.Contains(got, "- `image-51.png`") || strings.Contains(got, "- `image-52.png`") {
		t.Errorf("report does not keep the first 50 paths in sorted order:\n%s", got)
	}
}
