package baseline

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
)

const maxSourceBytes = 4 << 20

type scopeCache struct {
	root  string
	files map[string][]funcScope
}

type funcScope struct {
	name       string
	start, end int
}

func (c scopeCache) scope(file string, line int) string {
	scopes, ok := c.files[file]
	if !ok {
		scopes = readScopes(c.root, filepath.FromSlash(file))
		c.files[file] = scopes
	}
	for _, scope := range scopes {
		if line >= scope.start && line <= scope.end {
			return scope.name
		}
	}
	return "-"
}

func readScopes(root, file string) []funcScope {
	if filepath.Ext(file) != ".go" || !filepath.IsLocal(file) {
		return nil
	}
	path := filepath.Join(root, file)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxSourceBytes {
		return nil
	}
	fset := token.NewFileSet()
	tree, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	return collectScopes(fset, tree)
}

func collectScopes(fset *token.FileSet, tree *ast.File) []funcScope {
	var scopes []funcScope
	counts := make(map[string]int)
	for _, decl := range tree.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		name := funcName(fn)
		counts[name]++
		if count := counts[name]; count > 1 {
			name += "#" + strconv.Itoa(count)
		}
		start := fset.PositionFor(fn.Pos(), false).Line
		end := fset.PositionFor(fn.End(), false).Line
		scopes = append(scopes, funcScope{name, start, end})
	}
	return scopes
}

func funcName(fn *ast.FuncDecl) string {
	if fn.Recv != nil && len(fn.Recv.List) > 0 {
		if recv := receiverName(fn.Recv.List[0].Type); recv != "" {
			return recv + "." + fn.Name.Name
		}
	}
	return fn.Name.Name
}

func receiverName(expr ast.Expr) string {
	switch recv := ast.Unparen(expr).(type) {
	case *ast.Ident:
		return recv.Name
	case *ast.StarExpr:
		return receiverName(recv.X)
	case *ast.IndexExpr:
		return receiverName(recv.X)
	case *ast.IndexListExpr:
		return receiverName(recv.X)
	default:
		return ""
	}
}
