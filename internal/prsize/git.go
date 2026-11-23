package prsize

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path"
	"strconv"
	"strings"
)

const headRev = "HEAD"

type gitFunc func(...string) (string, error)

type inspection struct {
	totals        totals
	skippedBinary []string
}

func gitRunner(ctx context.Context, repo string) gitFunc {
	return func(args ...string) (string, error) {
		//nolint:gosec // Invoke Git directly with arguments built for specific subcommands.
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = repo
		output, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("git %q: %w", args, err)
		}
		return string(output), nil
	}
}

func inspect(git gitFunc, baseRef string) (inspection, error) {
	ref := "refs/remotes/origin/" + baseRef
	remote, err := git("show-ref", "--verify", "--hash", ref)
	if err != nil {
		return inspection{}, fmt.Errorf("remote-tracking ref %s unavailable: %w", ref, err)
	}
	base, err := git("merge-base", strings.TrimSpace(remote), headRev)
	if err != nil {
		return inspection{}, fmt.Errorf(
			"merge base for %s unavailable; check local ref: %w",
			ref,
			err,
		)
	}
	base = strings.TrimSpace(base)
	diff := []string{"--attr-source=" + base, "diff", "--no-ext-diff", "--no-textconv", "-z", "-M"}
	rawStats, err := git(append(diff, "--numstat", base, headRev, "--")...)
	if err != nil {
		return inspection{}, err
	}
	rawStatus, err := git(append(diff, "--name-status", base, headRev, "--")...)
	if err != nil {
		return inspection{}, err
	}
	status, err := parseStatus(rawStatus)
	if err != nil {
		return inspection{}, err
	}
	return tally(git, base, rawStats, status)
}

func tally(git gitFunc, base, raw string, statuses map[string]string) (inspection, error) {
	var result inspection
	for fields := diffFields(raw); len(fields) > 0; {
		change, remaining, err := parseChange(fields)
		if err != nil {
			return inspection{}, err
		}
		fields = remaining
		status := statuses[change.name]
		skipped, err := change.resolveLines(git, base, status)
		if err != nil {
			return inspection{}, err
		}
		if skipped {
			result.skippedBinary = append(result.skippedBinary, change.name)
		}
		kind, err := change.classify(git, base, status)
		if err != nil {
			return inspection{}, err
		}
		bucket := &result.totals[kind]
		bucket.files++
		bucket.lines += change.lines
	}
	return result, nil
}

func classifyAt(git gitFunc, rev, name string) (class, error) {
	if path.Ext(name) != ".go" || generatedName(path.Base(name)) {
		return classify(name, ""), nil
	}
	content, err := readBlob(git, rev, name)
	return classify(name, content), err
}

func readBlob(git gitFunc, rev, name string) (string, error) {
	content, err := git("cat-file", "blob", rev+":"+name)
	if err != nil {
		entry, _ := git("ls-tree", rev, "--", ":(literal)"+name)
		if strings.HasPrefix(entry, "160000 commit ") {
			err = nil
		}
	}
	return content, err
}

func diffFields(raw string) []string {
	fields := strings.Split(raw, "\x00")
	return fields[:len(fields)-1]
}

func parseStatus(raw string) (map[string]string, error) {
	statuses := make(map[string]string)
	fields := diffFields(raw)
	for len(fields) >= 2 {
		code, name := fields[0], fields[1]
		fields = fields[2:]
		if strings.HasPrefix(code, "R") || strings.HasPrefix(code, "C") {
			if len(fields) == 0 {
				return nil, fmt.Errorf("git name-status: missing destination for %q", name)
			}
			name, fields = fields[0], fields[1:]
		}
		statuses[name] = code
	}
	if len(fields) != 0 {
		return nil, fmt.Errorf("git name-status: missing path for %q", fields[0])
	}
	return statuses, nil
}

type change struct {
	name   string
	source string
	lines  int
	binary bool
}

func (c *change) resolveLines(git gitFunc, base, status string) (bool, error) {
	switch {
	case status == "D":
		c.lines = 0
	case c.binary:
		if knownBinary(c.name) {
			return true, nil
		}
		patch, err := c.textPatch(git, base)
		if err != nil {
			return false, err
		}
		c.lines = countPatchLines(patch)
	}
	return false, nil
}

func (c change) textPatch(git gitFunc, base string) (string, error) {
	args := []string{
		"--attr-source=" + base,
		"diff",
		"--text",
		"--no-ext-diff",
		"--no-textconv",
		"--no-color",
		"-U0",
	}
	if c.source != "" {
		// Compare the reported pair without redetecting renames on a subset of paths.
		return git(append(args, base+":"+c.source, headRev+":"+c.name, "--")...)
	}
	return git(
		append(
			args,
			"--no-renames",
			"--diff-filter=d",
			base,
			headRev,
			"--",
			":(literal)"+c.name,
		)...)
}

func countPatchLines(patch string) int {
	count, inHunk := 0, false
	for line := range strings.SplitSeq(patch, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			inHunk = false
		case strings.HasPrefix(line, "@@ "):
			inHunk = true
		case inHunk && (strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-")):
			count++
		}
	}
	return count
}

func knownBinary(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".png",
		".jpg",
		".jpeg",
		".gif",
		".ico",
		".webp",
		".pdf",
		".woff",
		".woff2",
		".ttf",
		".otf",
		".zip",
		".gz",
		".tgz",
		".jar",
		".exe",
		".dll",
		".so",
		".dylib",
		".wasm",
		".bin",
		".a",
		".o",
		".class",
		".mp3",
		".mp4",
		".mov",
		".tar",
		".xz",
		".bz2",
		".7z":
		return true
	default:
		return false
	}
}

func (c change) classify(git gitFunc, base, status string) (class, error) {
	if status == "D" {
		return classifyAt(git, base, c.name)
	}
	dest, err := classifyAt(git, headRev, c.name)
	if err != nil || dest == production || c.lines == 0 || !strings.HasPrefix(status, "R") {
		return dest, err
	}
	source, err := classifyAt(git, base, c.source)
	if source == production {
		return production, err
	}
	return dest, err
}

func parseChange(fields []string) (change, []string, error) {
	parts := strings.SplitN(fields[0], "\t", 3)
	if len(parts) != 3 {
		return change{}, nil, fmt.Errorf("git numstat: invalid record %q", fields[0])
	}
	c, rest := change{name: parts[2]}, fields[1:]
	if c.name == "" {
		if len(rest) < 2 {
			return change{}, nil, fmt.Errorf("git numstat: missing rename paths")
		}
		c.source, c.name, rest = rest[0], rest[1], rest[2:]
	}
	if parts[0] == "-" && parts[1] == "-" {
		c.binary = true
		return c, rest, nil
	}
	added, addErr := strconv.Atoi(parts[0])
	removed, removeErr := strconv.Atoi(parts[1])
	if err := errors.Join(addErr, removeErr); err != nil {
		return change{}, nil, fmt.Errorf("git numstat: invalid line count: %w", err)
	}
	c.lines = added + removed
	return c, rest, nil
}
