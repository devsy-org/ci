package baseline

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"go/token"
	"io"
	"path/filepath"
	"slices"
	"strconv"
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

type collector struct {
	root     string
	patterns []metricPattern
	cache    scopeCache
	grouped  map[Entry]*findingValues
}

// findingValues keeps every metric value of a key so one block cannot get
// worse while another improves by the same amount.
type findingValues struct {
	count   int
	metrics []int
}

func collectEntries(issues []issue, root string) ([]Entry, error) {
	c := collector{
		root:     root,
		patterns: metricPatterns(),
		cache:    scopeCache{root: root, files: map[string][]funcScope{}},
		grouped:  make(map[Entry]*findingValues),
	}
	for i, finding := range issues {
		if err := c.add(finding); err != nil {
			return nil, fmt.Errorf("lint finding %d: %w", i+1, err)
		}
	}
	var entries []Entry
	for key, values := range c.grouped {
		entries = append(entries, values.entries(key)...)
	}
	slices.SortFunc(entries, compareEntries)
	return entries, nil
}

func (c *collector) add(finding issue) error {
	linter, metric := measure(c.patterns, finding.FromLinter, finding.Text)
	file, err := localFilename(c.root, finding.Pos.Filename)
	if err != nil {
		return err
	}
	entry := Entry{
		Linter: linter,
		File:   file,
		Scope:  c.cache.scope(file, finding.Pos.Line),
		Value:  1,
	}
	if err := validateEntry(entry); err != nil {
		return err
	}
	values := c.grouped[entry]
	if values == nil {
		values = &findingValues{}
		c.grouped[entry] = values
	}
	if metric > 0 {
		values.metrics = append(values.metrics, metric)
	} else {
		values.count++
	}
	return nil
}

// entries ranks metric values from highest to lowest; rank n > 1 gets scope
// suffix "@n".
func (v *findingValues) entries(key Entry) []Entry {
	if len(v.metrics) == 0 {
		key.Value = v.count
		return []Entry{key}
	}
	values := slices.Clone(v.metrics)
	for range v.count {
		values = append(values, 1)
	}
	slices.SortFunc(values, func(a, b int) int { return cmp.Compare(b, a) })
	entries := make([]Entry, 0, len(values))
	for i, value := range values {
		entry := key
		entry.Value = value
		if i > 0 {
			entry.Scope += "@" + strconv.Itoa(i+1)
		}
		entries = append(entries, entry)
	}
	return entries
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
