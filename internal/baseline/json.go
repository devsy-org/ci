package baseline

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"go/token"
	"io"
	"maps"
	"math"
	"path/filepath"
	"slices"
)

const maxJSONBytes = 64 << 20

type issue struct {
	FromLinter string
	Text       string
	Pos        token.Position
}

// FromJSON decodes golangci-lint output and groups findings by baseline key.
func FromJSON(r io.Reader, root string) ([]Entry, error) {
	issues, err := readIssues(r, maxJSONBytes)
	if err != nil {
		return nil, err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve lint root: %w", err)
	}
	return collectEntries(issues, root)
}

func readIssues(r io.Reader, limit int64) ([]issue, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read lint JSON: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("lint JSON exceeds %d bytes", limit)
	}
	var report struct{ Issues *[]issue }
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("decode lint JSON: %w", err)
	}
	if report.Issues == nil {
		return nil, errors.New("lint JSON requires an Issues array")
	}
	return *report.Issues, nil
}

func collectEntries(issues []issue, root string) ([]Entry, error) {
	patterns := metricPatterns()
	cache := scopeCache{root: root, files: map[string][]funcScope{}}
	grouped := make(map[Entry]int)
	for i, finding := range issues {
		linter, metric := measure(patterns, finding.FromLinter, finding.Text)
		file, err := localFilename(root, finding.Pos.Filename)
		if err != nil {
			return nil, fmt.Errorf("lint finding %d: %w", i+1, err)
		}
		scope := cache.scope(file, finding.Pos.Line)
		entry := Entry{Linter: linter, File: file, Scope: scope, Value: 1}
		if err := validateEntry(entry); err != nil {
			return nil, fmt.Errorf("lint finding %d: %w", i+1, err)
		}
		value := cmp.Or(metric, 1)
		if value > math.MaxInt-grouped[entry] {
			return nil, fmt.Errorf("lint finding %d: total value overflows int", i+1)
		}
		grouped[entry] += value
	}
	entries := make([]Entry, 0, len(grouped))
	for _, key := range slices.SortedFunc(maps.Keys(grouped), compareEntries) {
		key.Value = grouped[key]
		entries = append(entries, key)
	}
	return entries, nil
}

func localFilename(root, file string) (string, error) {
	if filepath.IsAbs(file) {
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return "", fmt.Errorf("relativize filename %q: %w", file, err)
		}
		file = rel
	}
	if !filepath.IsLocal(file) {
		return "", fmt.Errorf("filename %q is not local to the lint root", file)
	}
	return filepath.ToSlash(filepath.Clean(file)), nil
}
