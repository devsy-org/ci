package prsize

import (
	"path"
	"slices"
	"strings"
)

type class int

const (
	generated class = iota
	tests
	docs
	config
	production
)

func classify(name, content string) class {
	base, ext := path.Base(name), path.Ext(name)
	root, _, nested := strings.Cut(name, "/")
	switch {
	case generatedName(base) || hasGeneratedHeader(content):
		return generated
	case testPath(name):
		return tests
	case slices.Contains([]string{".md", ".mdx"}, ext) || slices.Contains([]string{"docs", "sites"}, root):
		return docs
	case configPath(name, root, ext, nested):
		return config
	default:
		return production
	}
}

func generatedName(base string) bool {
	switch base {
	case "go.sum", "package-lock.json", "mise.lock", "THIRD_PARTY_LICENSES.md", "CHANGELOG.md":
		return true
	default:
		return strings.HasSuffix(base, ".pb.go")
	}
}

func hasGeneratedHeader(content string) bool {
	for i := 0; i < 40 && content != ""; i++ {
		line, tail, _ := strings.Cut(content, "\n")
		rest, found := strings.CutPrefix(line, "// Code generated ")
		if found && strings.HasSuffix(strings.TrimSuffix(rest, "\r"), " DO NOT EDIT.") {
			return true
		}
		content = tail
	}
	return false
}

func testPath(name string) bool {
	if strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, ".test.ts") {
		return true
	}
	for segment := range strings.SplitSeq(name, "/") {
		if slices.Contains([]string{"__tests__", "e2e", "testdata", "test"}, segment) {
			return true
		}
	}
	return false
}

func configPath(name, root, ext string, nested bool) bool {
	switch {
	case strings.HasPrefix(name, ".github/"), !nested && strings.HasPrefix(name, ".golangci"):
		return true
	case !nested, slices.Contains([]string{"charts", "deploy", "hack"}, root):
		return slices.Contains([]string{".yaml", ".yml", ".toml", ".json"}, ext)
	default:
		return false
	}
}
