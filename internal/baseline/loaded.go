package baseline

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const goExtension = ".go"

type loadedGo struct {
	tree            *os.Root
	root, moduleDir string
	files, allowed  map[string]bool
	imports         map[packageImport]bool
}

type packageImport struct{ importer, imported string }

type listedPackage struct {
	Dir, ImportPath                              string
	GoFiles, CgoFiles, TestGoFiles, XTestGoFiles []string
	Imports, TestImports, XTestImports           []string
	Match                                        []string
	ForTest                                      string
	Module                                       *listedModule
}

type listedModule struct {
	Path string
	Main bool
}

func readLoaded(tree *os.Root, rules directiveRules) (loadedGo, error) {
	if rules.goList == "" {
		return loadedGo{tree: tree}, nil
	}
	root, err := physicalPath(rules.root)
	if err != nil {
		return loadedGo{}, fmt.Errorf("resolve go list root: %w", err)
	}
	file, err := os.Open(filepath.Clean(rules.goList))
	if err != nil {
		return loadedGo{}, fmt.Errorf("open go list stream: %w", err)
	}
	loaded := loadedGo{
		tree: tree, root: root, moduleDir: rules.moduleDir,
		files: map[string]bool{}, allowed: map[string]bool{},
		imports: map[packageImport]bool{},
	}
	return loaded, errors.Join(loaded.decode(file), file.Close())
}

func (loaded loadedGo) decode(input io.Reader) error {
	decoder := json.NewDecoder(input)
	for {
		var pkg listedPackage
		err := decoder.Decode(&pkg)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read go list stream: decode package: %w", err)
		}
		if err := loaded.index(pkg); err != nil {
			return err
		}
	}
}

func (loaded loadedGo) index(pkg listedPackage) error {
	if pkg.Module != nil && !pkg.Module.Main {
		loaded.allowed[pkg.ImportPath] = true
	}
	if len(pkg.Match) == 0 || pkg.ForTest != "" {
		return nil
	}
	loaded.allowed[pkg.ImportPath] = true
	loaded.indexImports(pkg)
	if pkg.Dir == "" {
		return nil
	}
	dir, err := physicalPath(pkg.Dir)
	if err != nil {
		return fmt.Errorf("resolve package directory: %w", err)
	}
	for _, file := range slices.Concat(pkg.GoFiles, pkg.CgoFiles, pkg.TestGoFiles, pkg.XTestGoFiles) {
		loaded.files[filepath.Join(dir, file)] = true
	}
	return nil
}

func (loaded loadedGo) indexImports(pkg listedPackage) {
	if pkg.Module == nil || !pkg.Module.Main {
		return
	}
	for _, imported := range slices.Concat(pkg.Imports, pkg.TestImports, pkg.XTestImports) {
		if imported == pkg.Module.Path || strings.HasPrefix(imported, pkg.Module.Path+"/") {
			loaded.imports[packageImport{pkg.ImportPath, imported}] = true
		}
	}
}

func physicalPath(name string) (string, error) {
	absolute, err := filepath.Abs(name)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks: %w", err)
	}
	return resolved, nil
}

func (loaded loadedGo) rejects(name string) (bool, error) {
	name = filepath.FromSlash(name)
	if loaded.files == nil || filepath.Ext(name) != goExtension ||
		loaded.files[filepath.Join(loaded.root, name)] ||
		slices.Contains(strings.Split(filepath.ToSlash(name), "/"), "testdata") {
		return false, nil
	}
	return belongsToModule(loaded.tree, filepath.Dir(name), filepath.FromSlash(loaded.moduleDir))
}

func belongsToModule(root *os.Root, dir, module string) (bool, error) {
	for ; dir != module && dir != "."; dir = filepath.Dir(dir) {
		info, err := root.Stat(filepath.Join(dir, goMod))
		if err == nil && info.Mode().IsRegular() {
			return false, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("stat nearest go.mod: %w", err)
		}
	}
	return dir == module, nil
}

func (loaded loadedGo) reportImports(log *strings.Builder) int {
	var messages []string
	for imported := range loaded.imports {
		if !loaded.allowed[imported.imported] {
			messages = append(messages, fmt.Sprintf("%q imports a package outside ./...: %q",
				imported.importer, imported.imported))
		}
	}
	slices.Sort(messages)
	for _, message := range messages {
		fmt.Fprintln(log, message)
	}
	return len(messages)
}
