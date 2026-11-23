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
)

const goExtension = ".go"

type loadedGo struct {
	root    string
	files   map[string]bool
	imports map[string]bool
}

type listedPackage struct {
	Dir, ImportPath                              string
	GoFiles, CgoFiles, TestGoFiles, XTestGoFiles []string
	Imports, TestImports, XTestImports           []string
}

func readLoaded(name, root string) (loadedGo, error) {
	if name == "" {
		return loadedGo{}, nil
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return loadedGo{}, fmt.Errorf("resolve go list root: %w", err)
	}
	file, err := os.Open(filepath.Clean(name))
	if err != nil {
		return loadedGo{}, fmt.Errorf("open go list stream: %w", err)
	}
	loaded := loadedGo{root: absolute, files: make(map[string]bool), imports: make(map[string]bool)}
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
		loaded.addPackage(pkg)
	}
}

func (loaded loadedGo) addPackage(pkg listedPackage) {
	for _, file := range slices.Concat(pkg.GoFiles, pkg.CgoFiles, pkg.TestGoFiles, pkg.XTestGoFiles) {
		loaded.files[filepath.Join(pkg.Dir, file)] = true
	}
	for _, imported := range slices.Concat(pkg.Imports, pkg.TestImports, pkg.XTestImports) {
		if unlintedImport(imported) {
			message := fmt.Sprintf("%q imports unlinted code %q", pkg.ImportPath, imported)
			loaded.imports[message] = true
		}
	}
}

func unlintedImport(name string) bool {
	for segment := range strings.SplitSeq(name, "/") {
		if segment == "testdata" || strings.HasPrefix(segment, "_") {
			return true
		}
	}
	return false
}

func (loaded loadedGo) rejects(name string, change *fileChange) bool {
	if loaded.files == nil || !change.created || path.Ext(name) != goExtension {
		return false
	}
	return !loaded.files[filepath.Join(loaded.root, filepath.FromSlash(name))] &&
		!slices.Contains(strings.Split(name, "/"), "testdata")
}

func (loaded loadedGo) reportImports(log *strings.Builder) int {
	for _, message := range slices.Sorted(maps.Keys(loaded.imports)) {
		fmt.Fprintln(log, message)
	}
	return len(loaded.imports)
}
