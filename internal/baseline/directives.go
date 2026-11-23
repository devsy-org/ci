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
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

const (
	directivesCommand = "directives"
	goMod             = "go.mod"
)

type directiveRules struct {
	root, goList, moduleDir         string
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
	flags.StringVar(&rules.goList, "go-list", "", "go list -e -json -deps -test package stream")
	flags.StringVar(&rules.moduleDir, "module-dir", ".", "module directory relative to root")
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
	if !filepath.IsLocal(rules.moduleDir) {
		return rules, errors.New("expected --module-dir relative to --root")
	}
	rules.moduleDir = filepath.ToSlash(filepath.Clean(rules.moduleDir))
	return rules, nil
}

func scanDirectives(input io.Reader, rules directiveRules) (string, int, error) {
	files, err := collectAddedLines(input)
	if err != nil {
		return "", 0, err
	}
	root, err := os.OpenRoot(rules.root)
	if err != nil {
		return "", 0, fmt.Errorf("open directive root: %w", err)
	}
	defer func() { _ = root.Close() }()
	loaded, err := readLoaded(root, rules)
	if err != nil {
		return "", 0, err
	}
	var log strings.Builder
	count := loaded.reportImports(&log)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		findings, err := rules.findings(name, files[name], loaded)
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

func (r directiveRules) findings(name string, c *fileChange, l loadedGo) (map[int]string, error) {
	link, err := addedLink(l.tree, name, c.created)
	if err != nil || link {
		return map[int]string{1: "adds a symlink"}, err
	}
	findings, err := r.source(name, c)
	if err != nil {
		return nil, err
	}
	rejected, err := l.rejects(name)
	if rejected && findings[1] == "" {
		findings[1] = "not loaded by any GOOS build"
	}
	return findings, err
}

func addedLink(root *os.Root, name string, created bool) (bool, error) {
	if !created {
		return false, nil
	}
	info, err := root.Lstat(filepath.FromSlash(name))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat destination: %w", err)
	}
	return info.Mode()&os.ModeSymlink != 0, nil
}

func (r directiveRules) source(name string, change *fileChange) (map[int]string, error) {
	if !r.allowNewModules {
		switch path.Base(name) {
		case goMod, "go.work":
			if change.created {
				return map[int]string{1: "new Go module or workspace file"}, nil
			}
			if len(change.lines) != 0 {
				return r.manifest(name, change.lines)
			}
		}
	}
	if path.Ext(name) != goExtension || len(change.lines) == 0 {
		return map[int]string{}, nil
	}
	return r.fileFindings(name, change.lines)
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
