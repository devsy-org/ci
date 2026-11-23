// Package baseline turns lint findings into sortable baseline entries.
package baseline

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
)

const maxBaselineBytes = 16 << 20

// Entry records a finding count or a metric value for a linter, file, and scope.
type Entry struct {
	Linter string
	File   string
	Scope  string
	Value  int
}

// Parse reads baseline entries in strictly increasing key order.
func Parse(r io.Reader) ([]Entry, error) {
	limited := &io.LimitedReader{R: r, N: maxBaselineBytes + 1}
	entries, err := parseEntries(limited)
	if limited.N == 0 {
		return nil, fmt.Errorf("baseline exceeds %d bytes", maxBaselineBytes)
	}
	return entries, err
}

// Write emits entries in key order without changing the input slice.
func Write(w io.Writer, entries []Entry) error {
	sorted := slices.Clone(entries)
	slices.SortFunc(sorted, compareEntries)
	for i, entry := range sorted {
		if err := validateEntry(entry); err != nil {
			return fmt.Errorf("baseline entry %d: %w", i+1, err)
		}
		if i > 0 && compareEntries(sorted[i-1], entry) == 0 {
			return fmt.Errorf("baseline entry %d: duplicate key", i+1)
		}
	}
	for _, entry := range sorted {
		fields := []string{entry.Linter, entry.File, entry.Scope, strconv.Itoa(entry.Value)}
		if _, err := io.WriteString(w, strings.Join(fields, "\t")+"\n"); err != nil {
			return fmt.Errorf("write baseline: %w", err)
		}
	}
	return nil
}

func parseEntries(r io.Reader) ([]Entry, error) {
	reader := bufio.NewReader(r)
	var entries []Entry
	for n := 1; ; n++ {
		line, err := reader.ReadString('\n')
		if errors.Is(err, io.EOF) && line == "" {
			return entries, nil
		}
		if err != nil {
			return nil, fmt.Errorf("baseline line %d: read newline-terminated entry: %w", n, err)
		}
		entry, err := parseEntry(strings.TrimSuffix(line, "\n"))
		if err != nil {
			return nil, fmt.Errorf("baseline line %d: %w", n, err)
		}
		if len(entries) > 0 && compareEntries(entries[len(entries)-1], entry) >= 0 {
			return nil, fmt.Errorf("baseline line %d: keys must be sorted and unique", n)
		}
		entries = append(entries, entry)
	}
}

func parseEntry(line string) (Entry, error) {
	fields := strings.Split(line, "\t")
	if len(fields) != 4 {
		return Entry{}, errors.New("expected four tab-separated fields")
	}
	value, err := strconv.Atoi(fields[3])
	if err != nil {
		return Entry{}, fmt.Errorf("parse value: %w", err)
	}
	if strconv.Itoa(value) != fields[3] {
		return Entry{}, errors.New("value must be canonical decimal")
	}
	entry := Entry{Linter: fields[0], File: fields[1], Scope: fields[2], Value: value}
	return entry, validateEntry(entry)
}

func validateEntry(entry Entry) error {
	for _, field := range [...]string{entry.Linter, entry.File, entry.Scope} {
		if field == "" || strings.ContainsAny(field, "\t\r\n") {
			return errors.New("key fields must be nonempty and contain no tabs or newlines")
		}
	}
	if entry.Value <= 0 {
		return errors.New("value must be positive")
	}
	return nil
}

func compareEntries(a, b Entry) int {
	linter, file := cmp.Compare(a.Linter, b.Linter), cmp.Compare(a.File, b.File)
	return cmp.Or(linter, file, cmp.Compare(a.Scope, b.Scope))
}
