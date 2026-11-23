package prsize

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

const (
	exceptionLabel  = "size-exception"
	exceptionPrefix = "Size exception:"
	exceptionLength = 20
	summaryLimit    = 64 * 1024
)

type pullRequest struct {
	Labels []label `json:"labels"`
	Body   string  `json:"body"`
}

type label struct {
	Name string `json:"name"`
}

type totals [production + 1]struct {
	files int
	lines int
}

func readException(path string, read func(string) ([]byte, error)) (bool, error) {
	if path == "" {
		return false, nil
	}
	payload, err := read(path)
	if err != nil {
		return false, fmt.Errorf("read GitHub event: %w", err)
	}
	var event struct {
		PullRequest pullRequest `json:"pull_request"`
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		return false, fmt.Errorf("decode GitHub event: %w", err)
	}
	for _, label := range event.PullRequest.Labels {
		if label.Name == exceptionLabel {
			return hasReason(event.PullRequest.Body), nil
		}
	}
	return false, nil
}

func hasReason(body string) bool {
	for line := range strings.SplitSeq(body, "\n") {
		reason, found := strings.CutPrefix(line, exceptionPrefix)
		if found && printable(strings.TrimSpace(reason)) >= exceptionLength {
			return true
		}
	}
	return false
}

func printable(text string) int {
	count := 0
	for _, char := range text {
		if unicode.IsPrint(char) {
			count++
		}
	}
	return count
}

func verdict(lines, limit int, exception func() (bool, error)) (bool, string, error) {
	count := fmt.Sprintf("%d production lines (limit %d)", lines, limit)
	if lines <= limit {
		return true, "PASS: " + count + ".", nil
	}
	allowed, err := exception()
	if err != nil {
		return false, "", err
	}
	if allowed {
		return true, "PASS: " + count + ", size exception accepted.", nil
	}
	return false, "FAIL: " + count + ".", nil
}

func renderLog(result totals, message string) string {
	var report strings.Builder
	report.WriteString("| Class | Files | Changed lines | Counted |\n| --- | ---: | ---: | --- |\n")
	for index, name := range [...]string{"Generated", "Tests", "Docs", "Config", "Production"} {
		bucket := result[index]
		counted := "no"
		if index == int(production) {
			counted = "yes"
		}
		fmt.Fprintf(&report, "| %s | %d | %d | %s |\n", name, bucket.files, bucket.lines, counted)
	}
	fmt.Fprintf(&report, "\n%s\n", message)
	return report.String()
}

func renderSummary(log string, paths []string, passed bool) string {
	var report strings.Builder
	report.WriteString(log)
	if !passed {
		fmt.Fprintf(&report, "\nTo request an exception, add the `%s` label.\n", exceptionLabel)
		const reason = "Start a PR body line with `%s` and a reason of at least %d characters.\n"
		fmt.Fprintf(&report, reason, exceptionPrefix, exceptionLength)
	}
	writeSkippedBinary(&report, paths)
	return report.String()
}

func writeSkippedBinary(report *strings.Builder, paths []string) {
	if len(paths) == 0 {
		return
	}
	report.WriteString("\nBinary files not counted:\n\n")
	const limit = 50
	sorted := slices.Sorted(slices.Values(paths))
	footer := omittedBinary(len(sorted))
	for i, path := range sorted[:min(len(sorted), limit)] {
		quoted := strconv.QuoteToASCII(path)
		safe := strings.ReplaceAll(quoted[1:len(quoted)-1], "`", `\x60`)
		entry := "- `" + safe + "`\n"
		nextFooter := omittedBinary(len(sorted) - i - 1)
		if report.Len()+len(entry)+len(nextFooter) >= summaryLimit {
			break
		}
		report.WriteString(entry)
		footer = nextFooter
	}
	report.WriteString(footer)
}

func omittedBinary(count int) string {
	if count == 0 {
		return ""
	}
	return fmt.Sprintf("- and %d more\n", count)
}
