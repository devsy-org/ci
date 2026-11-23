package baseline

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
)

const goExtension = ".go"

type loadedGo struct {
	root, moduleDir, modulePath string
	files, packages             map[string]bool
	imports                     map[packageImport]bool
}

type packageImport struct{ importer, imported string }

type listedPackage struct {
	Dir, ImportPath                              string
	GoFiles, CgoFiles, TestGoFiles, XTestGoFiles []string
	Imports, TestImports, XTestImports           []string
}

func readLoaded(rules directiveRules) (loadedGo, error) {
	if rules.goList == "" {
		return loadedGo{}, nil
	}
	root, err := physicalPath(rules.root)
	if err != nil {
		return loadedGo{}, fmt.Errorf("resolve go list root: %w", err)
	}
	manifest := filepath.Join(rules.moduleDir, "go.mod")
	modulePath := modfile.ModulePath(readSource(root, manifest))
	if modulePath == "" {
		return loadedGo{}, fmt.Errorf("expected a module path in %q", manifest)
	}
	file, err := os.Open(filepath.Clean(rules.goList))
	if err != nil {
		return loadedGo{}, fmt.Errorf("open go list stream: %w", err)
	}
	loaded := loadedGo{
		root:       root,
		moduleDir:  rules.moduleDir,
		modulePath: modulePath,
		files: make(
			map[string]bool,
		),
		packages: make(map[string]bool),
		imports:  make(map[packageImport]bool),
	}
	if err := errors.Join(loaded.decode(file), file.Close()); err != nil {
		return loadedGo{}, fmt.Errorf("read go list stream: %w", err)
	}
	return loaded, nil
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
			return fmt.Errorf("decode package: %w", err)
		}
		if err := loaded.addPackage(pkg); err != nil {
			return err
		}
	}
}

func (loaded loadedGo) addPackage(pkg listedPackage) error {
	loaded.packages[pkg.ImportPath] = true
	if pkg.Dir != "" {
		dir, err := physicalPath(pkg.Dir)
		if err != nil {
			return fmt.Errorf("resolve package directory: %w", err)
		}
		for _, file := range slices.Concat(pkg.GoFiles, pkg.CgoFiles, pkg.TestGoFiles, pkg.XTestGoFiles) {
			loaded.files[filepath.Join(dir, file)] = true
		}
	}
	for _, imported := range slices.Concat(pkg.Imports, pkg.TestImports, pkg.XTestImports) {
		if imported == loaded.modulePath || strings.HasPrefix(imported, loaded.modulePath+"/") {
			loaded.imports[packageImport{pkg.ImportPath, imported}] = true
		}
	}
	return nil
}

func physicalPath(name string) (string, error) {
	resolved, err := filepath.EvalSymlinks(name)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", name, err)
	}
	absolute, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("make %q absolute: %w", resolved, err)
	}
	return absolute, nil
}

func (loaded loadedGo) rejects(name string, change *fileChange) bool {
	if loaded.files == nil || (!change.created && len(change.lines) == 0) ||
		path.Ext(name) != goExtension {
		return false
	}
	if loaded.moduleDir != "." && !strings.HasPrefix(name, loaded.moduleDir+"/") {
		return false
	}
	return !loaded.files[filepath.Join(loaded.root, filepath.FromSlash(name))] &&
		!slices.Contains(strings.Split(name, "/"), "testdata")
}

func (loaded loadedGo) reportImports(log *strings.Builder) int {
	var messages []string
	for imported := range maps.Keys(loaded.imports) {
		if !loaded.packages[imported.imported] {
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
