package baseline

import (
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/scanner"
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
	result := directiveFile{added: added, findings: make(map[int]string)}
	result.scanComments(name, src, rules)
	if result.generated && !result.headerAdded {
		first := slices.Min(slices.Collect(maps.Keys(added)))
		result.findings[first] = "adds code to a generated file"
	}
	return result.findings, nil
}

type directiveFile struct {
	added                  addedLines
	findings               map[int]string
	generated, headerAdded bool
}

func (f *directiveFile) scanComments(name string, src []byte, rules directiveRules) {
	file := token.NewFileSet().AddFile(name, -1, len(src))
	var lex scanner.Scanner
	lex.Init(file, src, nil, scanner.ScanComments)
	// PackageClauseOnly also sees comments through the package clause's semicolon.
	phase := 0
	for pos, tok, literal := lex.Scan(); tok != token.EOF; pos, tok, literal = lex.Scan() {
		if tok == token.COMMENT {
			line := file.PositionFor(pos, false).Line
			f.markComment(line, literal, rules)
			if !rules.allowGenerated && phase < 4 {
				f.markGeneratedComment(line, literal, phase == 0)
			}
		} else {
			phase = packagePhase(tok, phase)
		}
	}
}

func packagePhase(tok token.Token, phase int) int {
	switch {
	case tok == token.PACKAGE && phase == 0, phase == 1, phase == 2 && tok == token.SEMICOLON:
		return phase + 1
	case phase >= 2:
		return 4
	default:
		return phase
	}
}

func (f *directiveFile) markComment(line int, text string, rules directiveRules) {
	if f.added[line] {
		if reason := rules.commentReason(text); reason != "" {
			f.findings[line] = reason
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

func (f *directiveFile) markGeneratedComment(line int, text string, beforePackage bool) {
	if block, ok := strings.CutPrefix(text, "/*"); ok {
		for offset, part := range strings.Split(strings.TrimSuffix(block, "*/"), "\n") {
			f.markGenerated(line+offset, laxGenerated(part))
		}
		return
	}
	doc := (&ast.CommentGroup{List: []*ast.Comment{{Text: text}}}).Text()
	f.markGenerated(line, laxGenerated(doc) || (beforePackage && strictGenerated(text)))
}

func (f *directiveFile) markGenerated(line int, matches bool) {
	f.generated = f.generated || matches
	if matches && f.added[line] {
		f.headerAdded = true
		f.findings[line] = "new Go generated-code header"
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
