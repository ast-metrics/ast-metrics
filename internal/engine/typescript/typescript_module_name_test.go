package typescript

import (
	"os"
	"path/filepath"
	"testing"

	enginePkg "github.com/ast-metrics/ast-metrics/internal/engine"
)

// A TypeScript module names itself by its bare file name, where every import
// names it by a path relative to the importing file. Both ends of a dependency
// have to be spelled the same way, from the root of the package.
func TestParseNamesTheModuleAfterItsPathInThePackage(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src", "artifact"), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "src", "artifact", "entrypoint.ts")
	source := "import { Clearing } from '../clearing/clearing';\nimport { go } from '../shared';\nimport React from 'react';\n\nexport class Entrypoint { constructor(private c: Clearing) { go(); } }\n"
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	file, err := (&TypeScriptRunner{}).Parse(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	namespace := file.Stmts.StmtNamespace[0].Name
	if namespace.Qualified != "src/artifact/entrypoint" || namespace.Short != "entrypoint" {
		t.Errorf("namespace = %q/%q, expected the path from the package and the bare name", namespace.Qualified, namespace.Short)
	}
	targets := map[string]bool{}
	for _, dependency := range enginePkg.GetDependenciesInFile(file) {
		targets[dependency.Namespace] = true
		if dependency.From != "src/artifact/entrypoint" && dependency.From != `entrypoint\Entrypoint` {
			t.Errorf("dependency on %q comes from %q, expected the module or its class", dependency.Namespace, dependency.From)
		}
	}
	for _, expected := range []string{"src/clearing/clearing", "src/shared", "react"} {
		if !targets[expected] {
			t.Errorf("expected a dependency on %q, got %v", expected, targets)
		}
	}
}
