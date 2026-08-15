package rust

import (
	"os"
	"path/filepath"
	"testing"

	enginePkg "github.com/ast-metrics/ast-metrics/internal/engine"
)

// A Rust file names its module by its bare file name, "mod" for every mod.rs
// of the crate, where every use statement names it by its path from the crate.
// Both ends of a dependency have to be spelled the same way.
func TestParseNamesTheModuleAfterItsPathInTheCrate(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src", "artifact"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Cargo.toml"), []byte("[package]\nname = \"demo\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "src", "artifact", "mod.rs")
	source := "use crate::clearing::Clearing;\nuse super::shared::go;\nuse serde::Serialize;\n\npub struct Entrypoint { c: Clearing }\nimpl Entrypoint { pub fn run(&self) { go(); self.c.run() } }\n"
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	file, err := (&RustRunner{}).Parse(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	namespace := file.Stmts.StmtNamespace[0].Name
	if namespace.Qualified != "demo::artifact" || namespace.Short != "mod" {
		t.Errorf("namespace = %q/%q, expected demo::artifact and the bare name", namespace.Qualified, namespace.Short)
	}
	targets := map[string]bool{}
	for _, dependency := range enginePkg.GetDependenciesInFile(file) {
		targets[dependency.Namespace] = true
		if dependency.From != "demo::artifact" && dependency.From != `mod\Entrypoint` {
			t.Errorf("dependency on %q comes from %q, expected the module or its struct", dependency.Namespace, dependency.From)
		}
	}
	for _, expected := range []string{"demo::clearing", "demo::shared", "serde"} {
		if !targets[expected] {
			t.Errorf("expected a dependency on %q, got %v", expected, targets)
		}
	}
	if targets["crate::clearing"] || targets["super::shared"] {
		t.Errorf("expected the paths to be anchored, got %v", targets)
	}
}
