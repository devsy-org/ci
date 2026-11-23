package baseline

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestScopeSkipsSymlink(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("symlinks may require elevated permissions")
	}
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "target.go")
	if err := os.WriteFile(target, []byte("package p\nfunc F() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const link = "link.go"
	if err := os.Symlink(target, filepath.Join(root, link)); err != nil {
		t.Fatal(err)
	}
	cache := scopeCache{root: root, files: map[string][]funcScope{}}
	if got := cache.scope(link, 2); got != "-" {
		t.Errorf("scope() = %q, want -", got)
	}
}

func TestScopeSkipsOversizeFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	const file = "large.go"
	const source = "package p\nfunc F() {}\n"
	data := source + strings.Repeat(" ", maxSourceBytes-len(source)+1)
	if err := os.WriteFile(filepath.Join(root, file), []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cache := scopeCache{root: root, files: map[string][]funcScope{}}
	if got := cache.scope(file, 2); got != "-" {
		t.Errorf("scope() = %q, want -", got)
	}
}

func TestScopeSkipsSymlinkedDirectoryOutsideRoot(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("symlinks may require elevated permissions")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(outside, "f.go"),
		[]byte("package p\nfunc F() {}\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "dir")); err != nil {
		t.Fatal(err)
	}
	cache := scopeCache{root: root, files: map[string][]funcScope{}}
	if got := cache.scope("dir/f.go", 2); got != "-" {
		t.Errorf("scope() = %q, want -", got)
	}
}
