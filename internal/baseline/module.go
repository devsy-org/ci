package baseline

import (
	"errors"
	"fmt"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

func (rules directiveRules) replaceFindings(name string, added addedLines) (map[int]string, error) {
	source := readSource(rules.root, filepath.FromSlash(name))
	if source == nil {
		return nil, errors.New("expected a readable regular go.mod no larger than 4 MiB")
	}
	file, err := modfile.Parse(name, source, nil)
	if err != nil {
		return nil, fmt.Errorf("parse go.mod: %w", err)
	}
	findings := make(map[int]string)
	for _, replace := range file.Replace {
		line := replace.Syntax.Start.Line
		if added[line] && modfile.IsDirectoryPath(replace.New.Path) {
			findings[line] = "adds a local replace directive"
		}
	}
	return findings, nil
}
