package maskscan

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

type hit struct {
	path string
	line int
}

type diffPosition struct {
	path string
	line int
}

func scanDiff(diff string, allow []string) []hit {
	mask := regexp.MustCompile(`\*{6,}`)
	var pos diffPosition
	var hits []hit
	for line := range strings.SplitSeq(diff, "\n") {
		if added := pos.advance(line, allow); added && mask.MatchString(line[1:]) {
			hits = append(hits, hit{path: pos.path, line: pos.line - 1})
		}
	}
	return hits
}

func (p *diffPosition) advance(line string, allow []string) bool {
	switch {
	case strings.HasPrefix(line, "diff --git "):
		*p = diffPosition{}
	case strings.HasPrefix(line, "@@ "):
		p.line = hunkStart(line)
	case p.line == 0:
		if name, ok := strings.CutPrefix(line, "+++ "); ok {
			p.path = addedPath(name, allow)
		}
	case strings.HasPrefix(line, "+"):
		p.line++
		return p.path != ""
	case strings.HasPrefix(line, " "):
		p.line++
	}
	return false
}

func hunkStart(line string) int {
	_, newRange, ok := strings.Cut(line, " +")
	if !ok {
		return 0
	}
	start, _, _ := strings.Cut(newRange, " ")
	start, _, _ = strings.Cut(start, ",")
	n, _ := strconv.Atoi(start)
	return n
}

func addedPath(name string, allow []string) string {
	name = strings.TrimSuffix(name, "\t")
	if strings.HasPrefix(name, `"`) {
		name, _ = strconv.Unquote(name)
	}
	name, ok := strings.CutPrefix(name, "b/")
	if !ok || allowed(name, allow) {
		return ""
	}
	return name
}

func allowed(name string, allow []string) bool {
	for _, pattern := range allow {
		if strings.HasSuffix(pattern, "/") {
			if strings.HasPrefix(name, pattern) {
				return true
			}
			continue
		}
		if matched, _ := path.Match(pattern, name); matched {
			return true
		}
	}
	return false
}
