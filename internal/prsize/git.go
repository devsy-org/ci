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

type totals [production + 1]struct {
	files int
	lines int
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

func inspect(git gitFunc, baseRef string) (totals, error) {
	ref := "refs/remotes/origin/" + baseRef
	remote, err := git("show-ref", "--verify", "--hash", ref)
	if err != nil {
		return totals{}, fmt.Errorf("remote-tracking ref %s unavailable: %w", ref, err)
	}
	base, err := git("merge-base", strings.TrimSpace(remote), headRev)
	if err != nil {
		return totals{}, fmt.Errorf("merge base for %s unavailable; check local ref: %w", ref, err)
	}
	base = strings.TrimSpace(base)
	diff := []string{"--attr-source=" + base, "diff", "--no-ext-diff", "--no-textconv", "-z", "-M"}
	rawStats, err := git(append(diff, "--numstat", base, headRev, "--")...)
	if err != nil {
		return totals{}, err
	}
	rawStatus, err := git(append(diff, "--name-status", base, headRev, "--")...)
	if err != nil {
		return totals{}, err
	}
	status, err := parseStatus(rawStatus)
	if err != nil {
		return totals{}, err
	}
	return tally(git, base, rawStats, status)
}

func tally(git gitFunc, base, raw string, statuses map[string]string) (totals, error) {
	var result totals
	for fields := diffFields(raw); len(fields) > 0; {
		change, remaining, err := parseChange(fields)
		if err != nil {
			return totals{}, err
		}
		fields = remaining
		status := statuses[change.name]
		kind, err := change.classify(git, base, status)
		if err != nil {
			return totals{}, err
		}
		bucket := &result[kind]
		bucket.files++
		if status != "D" && status != "R100" {
			bucket.lines += change.lines
		}
	}
	return result, nil
}

func classifyAt(git gitFunc, rev, name string) (class, error) {
	if path.Ext(name) != ".go" || generatedName(path.Base(name)) {
		return classify(name, ""), nil
	}
	content, err := git("cat-file", "blob", rev+":"+name)
	if err != nil {
		entry, _ := git("ls-tree", rev, "--", ":(literal)"+name)
		if strings.HasPrefix(entry, "160000 commit ") {
			err = nil
		}
	}
	return classify(name, content), err
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
}

func (c change) classify(git gitFunc, base, status string) (class, error) {
	if status == "D" {
		return classify(c.name, ""), nil
	}
	dest, err := classifyAt(git, headRev, c.name)
	if err != nil || dest == production || status == "R100" || !strings.HasPrefix(status, "R") {
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
