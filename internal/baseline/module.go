package baseline

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

func (r directiveRules) manifest(name string, added addedLines) (map[int]string, error) {
	source := readSource(r.root, filepath.FromSlash(name))
	if source == nil {
		return nil, errors.New("expected a readable regular Go manifest no larger than 4 MiB")
	}
	if path.Base(name) == goMod {
		file, err := modfile.Parse(name, source, nil)
		if err != nil {
			return nil, fmt.Errorf("parse go.mod: %w", err)
		}
		return replacements(file.Replace, added, false), nil
	}
	work, err := modfile.ParseWork(name, source, nil)
	if err != nil {
		return nil, fmt.Errorf("parse go.work: %w", err)
	}
	findings := replacements(work.Replace, added, true)
	for _, use := range work.Use {
		if line := use.Syntax.Start.Line; added[line] {
			findings[line] = "adds a go.work use directive"
		}
	}
	return findings, nil
}

func replacements(entries []*modfile.Replace, added addedLines, work bool) map[int]string {
	findings := make(map[int]string)
	reason := "adds a local replace directive"
	if work {
		reason = "adds a go.work replace directive"
	}
	for _, replace := range entries {
		line := replace.Syntax.Start.Line
		if added[line] && (work || modfile.IsDirectoryPath(replace.New.Path)) {
			findings[line] = reason
		}
	}
	return findings
}
