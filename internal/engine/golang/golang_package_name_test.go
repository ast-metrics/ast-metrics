package golang

import (
	"os"
	"path/filepath"
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
	for _, from := range froms {
		if expected := "example.com/demo/internal/analyzer"; from != expected {
			t.Errorf("dependency comes from %q, expected %q", from, expected)
		}
	}
	if len(froms) == 0 {
		t.Error("expected the import to be read as a dependency")
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
		if from != "analyzer" {
			t.Errorf("dependency comes from %q, expected %q", from, "analyzer")
		}
	}
}
