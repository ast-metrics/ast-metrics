package golang

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	enginePkg "github.com/ast-metrics/ast-metrics/internal/engine"
)

// parseInModule writes a Go file inside a module rooted at a temporary
// directory, and parses it. An empty modulePath leaves the module out.
func parseInModule(t *testing.T, modulePath string, directory string, source string) (path string, namespaceShort string, namespaceQualified string, froms []string) {
	t.Helper()

	root := t.TempDir()
	if modulePath != "" {
		if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module "+modulePath+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, directory), 0o755); err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(root, directory, "source.go")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	runner := &GolangRunner{}
	file, err := runner.Parse(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	namespace := file.Stmts.StmtNamespace[0]
	for _, dependency := range enginePkg.GetDependenciesInFile(file) {
		froms = append(froms, dependency.From)
	}
	return path, namespace.Name.Short, namespace.Name.Qualified, froms
}

const packageSource = `package analyzer

import "example.com/demo/internal/engine"

type Aggregator struct{ parser *engine.Parser }
`

// A Go package names itself with a bare word, but every file importing it names
// it by its import path. Both ends of a dependency have to be spelled the same
// way, or nothing links a package to the packages using it.
func TestParseNamesThePackageAfterItsImportPath(t *testing.T) {
	_, short, qualified, froms := parseInModule(t, "example.com/demo", "internal/analyzer", packageSource)

	if expected := "example.com/demo/internal/analyzer"; qualified != expected {
		t.Errorf("namespace qualified name = %q, expected %q", qualified, expected)
	}
	// The bare name is how the package reads in a report, and stays.
	if short != "analyzer" {
		t.Errorf("namespace short name = %q, expected %q", short, "analyzer")
	}
	// The import is read once at the top of the file, coming from the package,
	// and once more from the struct using it, coming from the struct.
	if len(froms) == 0 {
		t.Error("expected the import to be read as a dependency")
	}
	for _, from := range froms {
		if from != "example.com/demo/internal/analyzer" && from != `analyzer\Aggregator` {
			t.Errorf("dependency comes from %q, expected the import path or the struct", from)
		}
	}
	if !slices.Contains(froms, "example.com/demo/internal/analyzer") {
		t.Errorf("expected a dependency coming from the package, got %q", froms)
	}
}

// The package sits at the root of its module, and its import path is the module
// path itself.
func TestParseNamesTheRootPackageAfterTheModule(t *testing.T) {
	_, _, qualified, _ := parseInModule(t, "example.com/demo", ".", packageSource)

	if expected := "example.com/demo"; qualified != expected {
		t.Errorf("namespace qualified name = %q, expected %q", qualified, expected)
	}
}

// Without a go.mod there is no import path to be had, and the bare name is all
// the package has.
func TestParseKeepsTheBareNameOutsideOfAnyModule(t *testing.T) {
	_, short, qualified, froms := parseInModule(t, "", "internal/analyzer", packageSource)

	if qualified != "analyzer" || short != "analyzer" {
		t.Errorf("namespace = %q/%q, expected the bare name on both", short, qualified)
	}
	for _, from := range froms {
		if from != "analyzer" && from != `analyzer\Aggregator` {
			t.Errorf("dependency comes from %q, expected the bare name or the struct", from)
		}
	}
}

// A test file refers to the structs of its own package as well, and those
// references are dependencies like the others: their source has to be the
// import path too, or the test files draw a second node named after the bare
// package beside the one the production files draw.
func TestParseNamesTheTestFileAfterItsImportPath(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "internal", "analyzer")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "source_test.go")
	source := "package analyzer\n\nimport \"testing\"\n\nfunc TestAggregator(t *testing.T) { _ = Aggregator{} }\n"
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	file, err := (&GolangRunner{}).Parse(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !file.GetIsTest() {
		t.Fatal("expected the file to be read as a test file")
	}
	dependencies := enginePkg.GetDependenciesInFile(file)
	if len(dependencies) == 0 {
		t.Fatal("expected the references of the test to be read as dependencies")
	}
	for _, dependency := range dependencies {
		if dependency.From != "example.com/demo/internal/analyzer" {
			t.Errorf("dependency %q comes from %q, expected the import path", dependency.ClassName, dependency.From)
		}
		if dependency.Namespace == "analyzer" {
			t.Errorf("dependency %q points at the bare package name, expected the import path", dependency.ClassName)
		}
	}
}
