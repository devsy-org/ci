package baseline

import (
	"cmp"
	"encoding/json"
	"fmt"
	"go/token"
	"io"
	"maps"
	"path/filepath"
	"slices"
)

type issue struct {
	FromLinter string
	Text       string
	Pos        token.Position
}

type findingValues struct {
	count  int
	metric int
}

// FromJSON decodes golangci-lint output and groups findings by baseline key.
func FromJSON(r io.Reader, root string) ([]Entry, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read lint JSON: %w", err)
	}
	var report struct{ Issues []issue }
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("decode lint JSON: %w", err)
	}
	return collectEntries(report.Issues, root)
}

func collectEntries(issues []issue, root string) ([]Entry, error) {
	patterns := metricPatterns()
	cache := scopeCache{root: root, files: map[string][]funcScope{}}
	grouped := make(map[Entry]findingValues)
	for i, finding := range issues {
		linter, metric := measure(patterns, finding.FromLinter, finding.Text)
		file := filepath.ToSlash(finding.Pos.Filename)
		scope := cache.scope(file, finding.Pos.Line)
		entry := Entry{Linter: linter, File: file, Scope: scope, Value: 1}
		if err := validateEntry(entry); err != nil {
			return nil, fmt.Errorf("lint finding %d: %w", i+1, err)
		}
		values := grouped[entry]
		values.count++
		values.metric = max(values.metric, metric)
		grouped[entry] = values
	}
	entries := make([]Entry, 0, len(grouped))
	for _, key := range slices.SortedFunc(maps.Keys(grouped), compareEntries) {
		values := grouped[key]
		key.Value = cmp.Or(values.metric, values.count)
		entries = append(entries, key)
	}
	return entries, nil
}
