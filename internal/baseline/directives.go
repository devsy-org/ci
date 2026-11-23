package baseline

import (
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"io"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

const directivesCommand = "directives"

type directiveRules struct {
	root                            string
	guarded                         map[string]bool
	allowGenerated, allowNewModules bool
}

func executeDirectives(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	rules, err := parseDirectiveFlags(args)
	if err != nil {
		return runError(stderr, err)
	}
	log, count, err := scanDirectives(stdin, rules)
	if err != nil {
		return runError(stderr, err)
	}
	verdict, code := "Lint directives passed: 0 violations.", 0
	if count > 0 {
		verdict, code = fmt.Sprintf("Lint directives failed. Violations: %d.", count), 1
	}
	if err := report(stdout, "", log, verdict); err != nil {
		return runError(stderr, err)
	}
	return code
}

func parseDirectiveFlags(args []string) (directiveRules, error) {
	var rules directiveRules
	flags := flag.NewFlagSet(directivesCommand, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&rules.root, "root", "", "destination source tree")
	flags.BoolVar(&rules.allowGenerated, "allow-generated", false, "allow generated headers")
	flags.BoolVar(
		&rules.allowNewModules,
		"allow-new-modules",
		false,
		"allow new Go modules and workspaces",
	)
	flags.Func("guarded", "comma-separated guarded linters", func(value string) error {
		rules.guarded = make(map[string]bool)
		for name := range strings.SplitSeq(value, ",") {
			rules.guarded[strings.ToLower(strings.TrimSpace(name))] = true
		}
		delete(rules.guarded, "")
		return nil
	})
	if err := flags.Parse(args); err != nil {
		return rules, fmt.Errorf("parse flags: %w", err)
	}
	if rules.root == "" || rules.guarded == nil || flags.NArg() != 0 {
		return rules, errors.New("expected --root and --guarded with no positional arguments")
	}
	return rules, nil
}

func scanDirectives(input io.Reader, rules directiveRules) (string, int, error) {
	files, err := collectAddedLines(input)
	if err != nil {
		return "", 0, err
	}
	var log strings.Builder
	count := 0
	for _, name := range slices.Sorted(maps.Keys(files)) {
		findings, err := rules.pathFindings(name, files[name])
		if err != nil {
			return "", 0, fmt.Errorf("check directives in %q: %w", name, err)
		}
		for _, number := range slices.Sorted(maps.Keys(findings)) {
			fmt.Fprintf(&log, "%q %d: %s\n", name, number, findings[number])
		}
		count += len(findings)
	}
	return log.String(), count, nil
}

func (rules directiveRules) pathFindings(name string, change *fileChange) (map[int]string, error) {
	switch path.Base(name) {
	case "go.mod", "go.work":
		if change.created && !rules.allowNewModules {
			return map[int]string{1: "new Go module or workspace file"}, nil
		}
	}
	if path.Ext(name) != ".go" || len(change.lines) == 0 {
		return map[int]string{}, nil
	}
	return rules.fileFindings(name, change.lines)
}

func (rules directiveRules) fileFindings(name string, added addedLines) (map[int]string, error) {
	src := readSource(rules.root, filepath.FromSlash(name))
	if src == nil {
		return nil, errors.New("expected a readable regular source file no larger than 4 MiB")
	}
	fset := token.NewFileSet()
	const mode = parser.ParseComments | parser.SkipObjectResolution
	file, err := parser.ParseFile(fset, name, src, mode)
	if err != nil {
		return nil, fmt.Errorf("parse source: %w", err)
	}
	result := directiveFile{fset: fset, added: added, findings: make(map[int]string)}
	result.markComments(file, rules)
	if rules.allowGenerated {
		return result.findings, nil
	}
	header, err := parser.ParseFile(fset, name, src, mode|parser.PackageClauseOnly)
	if err != nil {
		return nil, fmt.Errorf("parse package clause: %w", err)
	}
	for _, group := range header.Comments {
		for _, comment := range group.List {
			result.markGeneratedComment(comment, header.Package)
		}
	}
	return result.findings, nil
}

type directiveFile struct {
	fset     *token.FileSet
	added    addedLines
	findings map[int]string
}

func (f directiveFile) markComments(file *ast.File, rules directiveRules) {
	for _, group := range file.Comments {
		for _, comment := range group.List {
			line := f.fset.PositionFor(comment.Slash, false).Line
			if !f.added[line] {
				continue
			}
			if reason := rules.commentReason(comment.Text); reason != "" {
				f.findings[line] = reason
			}
		}
	}
}

func (rules directiveRules) commentReason(text string) string {
	if strings.HasPrefix(text, "//line ") || strings.HasPrefix(text, "/*line ") {
		return "new Go line directive"
	}
	if expr, err := constraint.Parse(text); err == nil && hasIgnoreTag(expr) {
		return "new Go build constraint mentions ignore"
	}
	return rules.nolintComment(text)
}

func hasIgnoreTag(expr constraint.Expr) bool {
	switch e := expr.(type) {
	case *constraint.TagExpr:
		return e.Tag == "ignore"
	case *constraint.NotExpr:
		return hasIgnoreTag(e.X)
	case *constraint.AndExpr:
		return hasIgnoreTag(e.X) || hasIgnoreTag(e.Y)
	case *constraint.OrExpr:
		return hasIgnoreTag(e.X) || hasIgnoreTag(e.Y)
	default:
		return false
	}
}

func (f directiveFile) markGeneratedComment(comment *ast.Comment, pkg token.Pos) {
	line := f.fset.PositionFor(comment.Slash, false).Line
	block, isBlock := strings.CutPrefix(comment.Text, "/*")
	lax := (&ast.CommentGroup{List: []*ast.Comment{comment}}).Text()
	if isBlock {
		// Keep blank block-comment lines so the marker retains its physical line number.
		lax = strings.TrimSuffix(block, "*/")
	}
	laxLines := strings.Split(lax, "\n")
	for i, text := range strings.Split(comment.Text, "\n") {
		if f.added[line+i] &&
			(laxGenerated(laxLines[i]) || (comment.Slash < pkg && strictGenerated(text))) {
			f.findings[line+i] = "new Go generated-code header"
		}
	}
}

func strictGenerated(line string) bool {
	rest, ok := strings.CutPrefix(line, "// Code generated ")
	return ok && strings.HasSuffix(rest, " DO NOT EDIT.")
}

func laxGenerated(line string) bool {
	line = strings.ToLower(strings.TrimRight(line, " \t\r\n"))
	for _, marker := range []string{
		"code generated", "do not edit", "autogenerated file", "* generated by: swagger codegen ",
	} {
		if strings.Contains(line, marker) {
			return true
		}
	}
	return false
}

func (rules directiveRules) nolintComment(text string) string {
	if !strings.HasPrefix(text, "//") {
		return ""
	}
	text = strings.TrimLeft(text, "/ ")
	const keyword = "nolint"
	if len(text) < len(keyword) || !strings.EqualFold(text[:len(keyword)], keyword) {
		return ""
	}
	return rules.nolintReason(text[len(keyword):])
}

func (rules directiveRules) nolintReason(directive string) string {
	if directive == "" || strings.HasPrefix(directive, " ") {
		return "bare //nolint directive"
	}
	list, ok := strings.CutPrefix(directive, ":")
	if !ok {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(list), "all") {
		return "//nolint suppresses all linters"
	}
	names, _, _ := strings.Cut(list, "//")
	for name := range strings.SplitSeq(names, ",") {
		name = leadingLinterName(name)
		if name == "all" {
			return "//nolint suppresses all linters"
		}
		if rules.guarded[name] {
			return fmt.Sprintf("//nolint suppresses guarded linter %q", name)
		}
	}
	return ""
}

func leadingLinterName(item string) string {
	item = strings.TrimSpace(item)
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"
	if end := strings.IndexFunc(
		item,
		func(r rune) bool { return !strings.ContainsRune(chars, r) },
	); end >= 0 {
		item = item[:end]
	}
	return strings.ToLower(item)
}
