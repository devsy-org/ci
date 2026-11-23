package baseline

import (
	"fmt"
	"slices"
)

type problem struct {
	entry   Entry
	message string
}

const staleMessage = "stale baseline entry"

func compare(current, baseline, base []Entry, checkBase bool) []problem {
	var problems []problem
	for _, entry := range current {
		switch allowed := valueOf(baseline, entry); {
		case allowed == 0:
			problems = append(problems, problem{entry, "new finding"})
		case entry.Value > allowed:
			message := fmt.Sprintf("grew from %d to %d", allowed, entry.Value)
			problems = append(problems, problem{entry, message})
		}
	}
	for _, entry := range baseline {
		if checkBase && entry.Value > valueOf(base, entry) {
			problems = append(problems, problem{entry, "baseline may only shrink"})
		}
		if entry.Value > valueOf(current, entry) {
			problems = append(problems, problem{entry, staleMessage})
		}
	}
	slices.SortStableFunc(
		problems,
		func(a, b problem) int { return compareEntries(a.entry, b.entry) },
	)
	return problems
}

type updateResult struct {
	entries          []Entry
	removed, lowered int
}

func shrink(baseline, current []Entry) updateResult {
	var result updateResult
	for _, entry := range baseline {
		switch value := valueOf(current, entry); {
		case value == 0:
			result.removed++
			continue
		case value < entry.Value:
			result.lowered++
			entry.Value = value
		}
		result.entries = append(result.entries, entry)
	}
	return result
}

func valueOf(entries []Entry, key Entry) int {
	if i, ok := slices.BinarySearchFunc(entries, key, compareEntries); ok {
		return entries[i].Value
	}
	return 0
}
