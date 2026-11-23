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
	ref := "origin/" + baseRef
	base, err := git("merge-base", ref, "HEAD")
	if err != nil {
		return totals{}, fmt.Errorf("merge base for %s unavailable; check local ref: %w", ref, err)
	}
	revision := strings.TrimSpace(base) + "...HEAD"
	rawStats, err := git("diff", "-z", "--numstat", "-M", revision)
	if err != nil {
		return totals{}, err
	}
	rawStatus, err := git("diff", "-z", "--name-status", "-M", revision)
	if err != nil {
		return totals{}, err
	}
	status, err := parseStatus(rawStatus)
	if err != nil {
		return totals{}, err
	}
	return tally(git, rawStats, status)
}

func tally(git gitFunc, raw string, statuses map[string]string) (totals, error) {
	var result totals
	for fields := diffFields(raw); len(fields) > 0; {
		change, remaining, err := parseChange(fields)
		if err != nil {
			return totals{}, err
		}
		fields = remaining
		status := statuses[change.name]
		content, err := headContent(git, change.name, status)
		if err != nil {
			return totals{}, err
		}
		bucket := &result[classify(change.name, content)]
		bucket.files++
		if status != "D" && status != "R100" {
			bucket.lines += change.lines
		}
	}
	return result, nil
}

func headContent(git gitFunc, name, status string) (string, error) {
	if status == "D" || generatedName(path.Base(name)) {
		return "", nil
	}
	content, err := git("cat-file", "blob", "HEAD:"+name)
	if err != nil {
		entry, _ := git("ls-tree", "HEAD", "--", ":(literal)"+name)
		if strings.HasPrefix(entry, "160000 commit ") {
			return "", nil
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
	name  string
	lines int
}

func parseChange(fields []string) (change, []string, error) {
	parts := strings.SplitN(fields[0], "\t", 3)
	if len(parts) != 3 {
		return change{}, nil, fmt.Errorf("git numstat: invalid record %q", fields[0])
	}
	name, rest := parts[2], fields[1:]
	if name == "" {
		if len(rest) < 2 {
			return change{}, nil, fmt.Errorf("git numstat: missing rename paths")
		}
		name, rest = rest[1], rest[2:]
	}
	lines, err := lineCount(parts[0], parts[1])
	if err != nil {
		return change{}, nil, fmt.Errorf("git numstat: invalid line count: %w", err)
	}
	return change{name: name, lines: lines}, rest, nil
}

func lineCount(added, removed string) (int, error) {
	if added == "-" && removed == "-" {
		return 0, nil
	}
	a, addErr := strconv.Atoi(added)
	r, removeErr := strconv.Atoi(removed)
	return a + r, errors.Join(addErr, removeErr)
}
