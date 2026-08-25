package file

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/configuration"
)

func writeFile(t testing.TB, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func scopeOf(path string, config *configuration.Configuration) configuration.Scope {
	return configuration.Scope{Path: path, Configuration: config}
}

func TestSearchMultipleGivesANestedSourceItsOwnFiles(t *testing.T) {
	root := t.TempDir()
	front := filepath.Join(root, "front")
	writeFile(t, filepath.Join(root, "a.go"))
	writeFile(t, filepath.Join(front, "b.go"))

	config := configuration.Configuration{SourcesToAnalyzePath: []string{root, front}}
	finder := Finder{Configuration: config, projectRoot: root}

	result := finder.SearchMultiple([]string{".go"})[".go"]

	// b.go is discovered once, by the source that owns it.
	if len(result.Files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(result.Files), result.Files)
	}
	if got := len(result.FilesByDirectory[root]); got != 1 {
		t.Errorf("expected 1 file under %s, got %d", root, got)
	}
	if got := len(result.FilesByDirectory[front]); got != 1 {
		t.Errorf("expected 1 file under %s, got %d", front, got)
	}
}

func TestSearchMultipleIgnoresADuplicatedSource(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.go"))

	config := configuration.Configuration{SourcesToAnalyzePath: []string{root, root}}
	finder := Finder{Configuration: config, projectRoot: root}

	result := finder.SearchMultiple([]string{".go"})[".go"]

	if len(result.Files) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(result.Files), result.Files)
	}
}

func TestSearchMultipleAppliesTheExcludesOfEachScope(t *testing.T) {
	root := t.TempDir()
	front := filepath.Join(root, "front")
	writeFile(t, filepath.Join(root, "generated", "a.go"))
	writeFile(t, filepath.Join(front, "generated", "b.go"))

	// The scope pattern is written against the scope directory, the same way it
	// would be if front were analyzed on its own.
	frontConfig := &configuration.Configuration{ExcludePatterns: []string{"/generated/"}}
	rootConfig := &configuration.Configuration{}
	config := configuration.Configuration{
		SourcesToAnalyzePath: []string{root, front},
		Scopes: []configuration.Scope{
			scopeOf(root, rootConfig),
			{Path: front, Configuration: frontConfig, Root: front},
		},
	}
	finder := Finder{Configuration: config, projectRoot: root}

	result := finder.SearchMultiple([]string{".go"})[".go"]

	if len(result.Files) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(result.Files), result.Files)
	}
	if result.Files[0] != filepath.Join(root, "generated", "a.go") {
		t.Errorf("expected the root file to be kept, got %s", result.Files[0])
	}
}

func TestSearchMultipleKeepsAnExtensionInsideTheScopeThatDeclaredIt(t *testing.T) {
	root := t.TempDir()
	front := filepath.Join(root, "front")
	writeFile(t, filepath.Join(root, "a.inc"))
	writeFile(t, filepath.Join(front, "b.inc"))

	frontConfig := &configuration.Configuration{Extensions: map[string][]string{"php": {".inc"}}}
	rootConfig := &configuration.Configuration{}
	config := configuration.Configuration{
		SourcesToAnalyzePath: []string{root, front},
		Scopes: []configuration.Scope{
			scopeOf(root, rootConfig),
			{Path: front, Configuration: frontConfig, Root: front},
		},
	}
	finder := Finder{Configuration: config, projectRoot: root}

	result := finder.SearchMultiple([]string{".inc"})[".inc"]

	if len(result.Files) != 1 {
		t.Fatalf("expected 1 file, got %d: %v", len(result.Files), result.Files)
	}
	if result.Files[0] != filepath.Join(front, "b.inc") {
		t.Errorf("expected only the front file, got %s", result.Files[0])
	}
}

// benchmarkTree lays out a project of the size the discovery is expected to
// cope with, including the vendor directory the defaults prune away.
func benchmarkTree(b *testing.B) string {
	root := b.TempDir()
	for directory := 0; directory < 40; directory++ {
		for file := 0; file < 25; file++ {
			writeFile(b, filepath.Join(root, "src", strconv.Itoa(directory), fmt.Sprintf("f%d.go", file)))
			writeFile(b, filepath.Join(root, "vendor", strconv.Itoa(directory), fmt.Sprintf("f%d.go", file)))
		}
	}

	return root
}

func BenchmarkSearchMultiple(b *testing.B) {
	root := benchmarkTree(b)
	config := configuration.Configuration{
		SourcesToAnalyzePath: []string{root},
		ExcludePatterns:      []string{"/vendor/"},
	}
	finder := Finder{Configuration: config, projectRoot: root}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		finder.SearchMultiple([]string{".go", ".php", ".ts"})
	}
}

func BenchmarkSearchMultipleScoped(b *testing.B) {
	root := benchmarkTree(b)
	source := filepath.Join(root, "src")
	nested := filepath.Join(root, "src", "0")

	rootConfig := &configuration.Configuration{ExcludePatterns: []string{"/vendor/"}}
	nestedConfig := &configuration.Configuration{ExcludePatterns: []string{"/vendor/"}}
	config := configuration.Configuration{
		SourcesToAnalyzePath: []string{source, nested},
		ExcludePatterns:      []string{"/vendor/"},
		Scopes: []configuration.Scope{
			scopeOf(source, rootConfig),
			{Path: nested, Configuration: nestedConfig, Root: nested},
		},
	}
	finder := Finder{Configuration: config, projectRoot: root}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		finder.SearchMultiple([]string{".go", ".php", ".ts"})
	}
}
