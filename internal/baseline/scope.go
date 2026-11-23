package baseline

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
)

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
	fset := token.NewFileSet()
	tree, err := parser.ParseFile(fset, filepath.Join(root, file), nil, parser.SkipObjectResolution)
	if err != nil {
		return nil
	}
	var scopes []funcScope
	for _, decl := range tree.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok {
			start := fset.PositionFor(fn.Pos(), false).Line
			end := fset.PositionFor(fn.End(), false).Line
			scopes = append(scopes, funcScope{funcName(fn), start, end})
		}
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
