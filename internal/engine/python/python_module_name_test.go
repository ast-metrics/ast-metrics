package python

import (
	"os"
	"path/filepath"
	"testing"

	enginePkg "github.com/ast-metrics/ast-metrics/internal/engine"
)

// A Python module names itself with its bare file name, but every module
// importing it names it by its dotted path. Both ends of a dependency have to
// be spelled the same way, or nothing links a module to the modules using it.
func TestParseNamesTheModuleAfterItsDottedPath(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"company/__init__.py", "company/product/__init__.py", "company/product/artifact/__init__.py"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, p)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, p), []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "company/product/artifact/entrypoint.py")
	source := "from company.product.clearing.clearing import Clearing\nfrom .importer import Importer\nfrom ..shared import util\n\nclass Entrypoint:\n    def __init__(self, c: Clearing, i: Importer):\n        self.c = c\n"
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	file, err := (&PythonRunner{}).Parse(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	namespace := file.Stmts.StmtNamespace[0].Name
	if namespace.Qualified != "company.product.artifact.entrypoint" || namespace.Short != "entrypoint" {
		t.Errorf("namespace = %q/%q, expected the dotted path and the bare name", namespace.Qualified, namespace.Short)
	}

	targets := map[string]bool{}
	for _, dependency := range enginePkg.GetDependenciesInFile(file) {
		targets[dependency.Namespace] = true
		if dependency.From != "company.product.artifact.entrypoint" && dependency.From != `entrypoint\Entrypoint` {
			t.Errorf("dependency on %q comes from %q, expected the module or its class", dependency.Namespace, dependency.From)
		}
	}
	for _, expected := range []string{"company.product.clearing.clearing", "company.product.artifact.importer", "company.product.shared"} {
		if !targets[expected] {
			t.Errorf("expected a dependency on %q, got %v", expected, targets)
		}
	}
	if targets[".importer"] || targets["..shared"] {
		t.Errorf("expected the relative imports to be spelled absolutely, got %v", targets)
	}
}
