package maskscan

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"
)

const hitMessage = "added line has a run of 6 or more asterisks"

func renderLog(hits []hit) string {
	var log strings.Builder
	escape := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A", ":", "%3A", ",", "%2C")
	for _, h := range hits {
		fmt.Fprintf(&log, "mask-scan: %s:%d: %s\n", displayPath(h.path), h.line, hitMessage)
		fmt.Fprintf(
			&log,
			"::error file=%s,line=%d::%s\n",
			escape.Replace(h.path),
			h.line,
			hitMessage,
		)
	}
	if len(hits) == 0 {
		log.WriteString("PASS: no added lines with a run of 6 or more asterisks.\n")
	} else {
		fmt.Fprintf(&log, "FAIL: %d added line(s) with a run of 6 or more asterisks.\n", len(hits))
	}
	return log.String()
}

func displayPath(name string) string {
	if strings.IndexFunc(name, func(r rune) bool { return !unicode.IsGraphic(r) }) >= 0 {
		name = strconv.QuoteToGraphic(name)
	}
	// The runner's legacy command parser matches "##[" anywhere in a line.
	return strings.ReplaceAll(name, "##[", `\x23\x23[`)
}

func renderSummary(hits []hit) string {
	var summary strings.Builder
	fmt.Fprintf(
		&summary,
		"Mask scan: %d added line(s) with a run of 6 or more asterisks.\n",
		len(hits),
	)
	for _, h := range hits {
		name := strings.ReplaceAll(displayPath(h.path), "`", `\x60`)
		fmt.Fprintf(&summary, "\n- `%s`:%d", name, h.line)
	}
	summary.WriteString(
		"\n\nMasked values usually come from writing a tool's redacted view back to a file. " +
			"Restore the real text; if the run is real, add the path to `allow` in the workflow.\n",
	)
	return summary.String()
}

func writeSummary(path string, hits []hit) error {
	if path == "" {
		return nil
	}
	//nolint:gosec // The runner supplies the step summary path; the tool only appends to it.
	summary, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open GitHub step summary: %w", err)
	}
	_, err = summary.WriteString(renderSummary(hits))
	if err := errors.Join(err, summary.Close()); err != nil {
		return fmt.Errorf("write GitHub step summary: %w", err)
	}
	return nil
}
