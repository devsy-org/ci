package baseline

import (
	"regexp"
	"strconv"
	"strings"
)

type metricPattern struct {
	linter string
	regex  *regexp.Regexp
}

func metricPatterns() []metricPattern {
	const exceeded = ` per function exceeded; max \d+ but got (\d+)$`
	const complexity = ` complexity (\d+) \(> max enabled \d+\)$`
	specs := [...]struct{ linter, expr string }{
		{"cyclop", `^calculated cyclomatic complexity for function .+ is (\d+), max is \d+$`},
		{"funlen:lines", `^Function '[^']+' is too long \((\d+) > \d+\)$`},
		{"funlen:statements", `^Function '[^']+' has too many statements \((\d+) > \d+\)$`},
		{"gocognit", "^cognitive complexity (\\d+) of func `[^`]+` is high \\(> \\d+\\)$"},
		{"nestif", "(?s)^`if .+` has complex nested blocks \\(complexity: (\\d+)\\)$"},
		{"revive:argument-limit", `^maximum number of arguments` + exceeded},
		{"revive:function-result-limit", `^maximum number of return results` + exceeded},
		{"revive:function-length:lines", `^maximum number of lines` + exceeded},
		{"revive:function-length:statements", `^maximum number of statements` + exceeded},
		{"revive:cognitive-complexity", `^function .+ has cognitive` + complexity},
		{"revive:cyclomatic", `^function .+ has cyclomatic` + complexity},
	}
	patterns := make([]metricPattern, 0, len(specs))
	for _, spec := range specs {
		patterns = append(patterns, metricPattern{spec.linter, regexp.MustCompile(spec.expr)})
	}
	return patterns
}

func measure(patterns []metricPattern, linter, message string) (string, int) {
	if rule, body, ok := strings.Cut(message, ": "); linter == "revive" && ok && rule != "" {
		linter, message = linter+":"+rule, body
	}
	for _, pattern := range patterns {
		if !pattern.matchesLinter(linter) {
			continue
		}
		if match := pattern.regex.FindStringSubmatch(message); match != nil {
			value, err := strconv.Atoi(match[1])
			if err != nil {
				return pattern.linter, 0
			}
			return pattern.linter, value
		}
	}
	return linter, 0
}

func (p metricPattern) matchesLinter(linter string) bool {
	switch linter {
	case "funlen", "revive:function-length":
		return strings.HasPrefix(p.linter, linter+":")
	default:
		return p.linter == linter
	}
}
