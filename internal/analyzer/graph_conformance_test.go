package analyzer

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	enginePkg "github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/csharp"
	"github.com/ast-metrics/ast-metrics/internal/engine/golang"
	"github.com/ast-metrics/ast-metrics/internal/engine/java"
	"github.com/ast-metrics/ast-metrics/internal/engine/php"
	"github.com/ast-metrics/ast-metrics/internal/engine/python"
	"github.com/ast-metrics/ast-metrics/internal/engine/rust"
	"github.com/ast-metrics/ast-metrics/internal/engine/typescript"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// The same project is written in every language: three modules, artifact
// depending on clearing depending on shared, under a root deeper than the
// depth the graph is cut at. Whatever the language, the graph has to draw the
// three modules and the two edges, the package relations have to read the same,
// and the files have to be coupled the same way. This is what keeps the
// languages from drifting apart on the architecture map.
type architectureSample struct {
	language string
	engine   enginePkg.Engine
	// manifest files marking the root of the project
	setup map[string]string
	// artifact, clearing and shared, in this order: path and source
	files [3][2]string
	// the three modules as the graph is expected to name them
	modules [3]string
	// what the package relations are expected to name the three modules
	packages [3]string
}

func architectureSamples() []architectureSample {
	phpRoot := `Company\Project\SubProject`
	return []architectureSample{
		{
			language: "PHP", engine: &php.PhpRunner{},
			files: [3][2]string{
				{"src/Artifact/Entrypoint.php", "<?php\nnamespace " + phpRoot + "\\Artifact;\nuse " + phpRoot + "\\Clearing\\Clearing;\nclass Entrypoint { public function __construct(Clearing $c) {} }\n"},
				{"src/Clearing/Clearing.php", "<?php\nnamespace " + phpRoot + "\\Clearing;\nuse " + phpRoot + "\\Shared\\Util;\nclass Clearing { public function run(): void { Util::go(); } }\n"},
				{"src/Shared/Util.php", "<?php\nnamespace " + phpRoot + "\\Shared;\nclass Util { public static function go(): void {} }\n"},
			},
			modules:  [3]string{phpRoot + `\Artifact`, phpRoot + `\Clearing`, phpRoot + `\Shared`},
			packages: [3]string{phpRoot + `\Artifact`, phpRoot + `\Clearing`, phpRoot + `\Shared`},
		},
		{
			language: "Java", engine: &java.JavaRunner{},
			files: [3][2]string{
				{"src/com/acme/product/artifact/Entrypoint.java", "package com.acme.product.artifact;\nimport com.acme.product.clearing.Clearing;\npublic class Entrypoint { private Clearing c; }\n"},
				{"src/com/acme/product/clearing/Clearing.java", "package com.acme.product.clearing;\nimport com.acme.product.shared.Util;\npublic class Clearing { public void run() { Util.go(); } }\n"},
				{"src/com/acme/product/shared/Util.java", "package com.acme.product.shared;\npublic class Util { public static void go() {} }\n"},
			},
			modules:  [3]string{"com.acme.product.artifact", "com.acme.product.clearing", "com.acme.product.shared"},
			packages: [3]string{"com.acme.product.artifact", "com.acme.product.clearing", "com.acme.product.shared"},
		},
		{
			language: "C#", engine: &csharp.CSharpRunner{},
			files: [3][2]string{
				{"src/Artifact/Entrypoint.cs", "using Company.Product.Sub.Clearing;\nnamespace Company.Product.Sub.Artifact { public class Entrypoint { private Clearing c; } }\n"},
				{"src/Clearing/Clearing.cs", "using Company.Product.Sub.Shared;\nnamespace Company.Product.Sub.Clearing { public class Clearing { public void Run() { Util.Go(); } } }\n"},
				{"src/Shared/Util.cs", "namespace Company.Product.Sub.Shared { public static class Util { public static void Go() {} } }\n"},
			},
			modules:  [3]string{"Company.Product.Sub.Artifact", "Company.Product.Sub.Clearing", "Company.Product.Sub.Shared"},
			packages: [3]string{"Company.Product.Sub.Artifact", "Company.Product.Sub.Clearing", "Company.Product.Sub.Shared"},
		},
		{
			language: "Go", engine: &golang.GolangRunner{}, setup: map[string]string{"go.mod": "module example.com/demo\n"},
			files: [3][2]string{
				{"internal/artifact/a.go", "package artifact\n\nimport \"example.com/demo/internal/clearing\"\n\ntype Entrypoint struct{ c clearing.Clearing }\n"},
				{"internal/clearing/c.go", "package clearing\n\nimport \"example.com/demo/internal/shared\"\n\ntype Clearing struct{}\n\nfunc (c Clearing) Run() { shared.Go() }\n"},
				{"internal/shared/s.go", "package shared\n\nfunc Go() {}\n"},
			},
			modules:  [3]string{"example.com/demo/internal/artifact", "example.com/demo/internal/clearing", "example.com/demo/internal/shared"},
			packages: [3]string{"example.com/demo/internal/artifact", "example.com/demo/internal/clearing", "example.com/demo/internal/shared"},
		},
		{
			language: "Python", engine: &python.PythonRunner{}, setup: map[string]string{"pyproject.toml": ""},
			files: [3][2]string{
				{"company/product/artifact/entrypoint.py", "from company.product.clearing.clearing import Clearing\n\nclass Entrypoint:\n    def __init__(self, c: Clearing):\n        self.c = c\n"},
				{"company/product/clearing/clearing.py", "from ..shared.util import go\n\nclass Clearing:\n    def run(self):\n        go()\n"},
				{"company/product/shared/util.py", "def go():\n    pass\n"},
			},
			modules:  [3]string{"company.product.artifact", "company.product.clearing", "company.product.shared"},
			packages: [3]string{"company.product.artifact", "company.product.clearing", "company.product.shared"},
		},
		{
			language: "TypeScript", engine: &typescript.TypeScriptRunner{}, setup: map[string]string{"package.json": "{}"},
			files: [3][2]string{
				{"src/artifact/entrypoint.ts", "import { Clearing } from '../clearing/clearing';\nexport class Entrypoint { constructor(private c: Clearing) {} }\n"},
				{"src/clearing/clearing.ts", "import { go } from '../shared/util';\nexport class Clearing { run() { go(); } }\n"},
				{"src/shared/util.ts", "export function go() {}\n"},
			},
			modules:  [3]string{"src/artifact/entrypoint", "src/clearing/clearing", "src/shared/util"},
			packages: [3]string{"src/artifact", "src/clearing", "src/shared"},
		},
		{
			language: "Rust", engine: &rust.RustRunner{}, setup: map[string]string{"Cargo.toml": "[package]\nname = \"demo\"\n"},
			files: [3][2]string{
				{"src/artifact/mod.rs", "use crate::clearing::Clearing;\npub struct Entrypoint { c: Clearing }\n"},
				{"src/clearing/mod.rs", "use crate::shared::go;\npub struct Clearing;\nimpl Clearing { pub fn run(&self) { go() } }\n"},
				{"src/shared/mod.rs", "pub fn go() {}\n"},
			},
			modules:  [3]string{"demo::artifact", "demo::clearing", "demo::shared"},
			packages: [3]string{"demo::artifact", "demo::clearing", "demo::shared"},
		},
	}
}

func TestArchitectureIsReadTheSameWayInEveryLanguage(t *testing.T) {
	for _, sample := range architectureSamples() {
		t.Run(sample.language, func(t *testing.T) {
			root := t.TempDir()
			for path, content := range sample.setup {
				if err := os.WriteFile(filepath.Join(root, path), []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			files := make([]*pb.File, 0, 3)
			for _, source := range sample.files {
				path := filepath.Join(root, source[0])
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(source[1]), 0o644); err != nil {
					t.Fatal(err)
				}
				file, err := sample.engine.Parse(path)
				if err != nil {
					t.Fatalf("parse %s: %v", source[0], err)
				}
				AnalyzeFile(file)
				files = append(files, file)
			}
			aggregated := graphOfFiles(files...)

			artifact, clearing, shared := sample.modules[0], sample.modules[1], sample.modules[2]
			if got := nodesOf(aggregated); !slices.Equal(got, sortedCopy([]string{artifact, clearing, shared})) {
				t.Fatalf("graph nodes = %q, expected %q", got, sample.modules)
			}
			if edges := aggregated.Graph.Nodes[artifact].Edges; !slices.Contains(edges, clearing) {
				t.Errorf("expected an edge from artifact to clearing, got %q", edges)
			}
			if edges := aggregated.Graph.Nodes[clearing].Edges; !slices.Contains(edges, shared) {
				t.Errorf("expected an edge from clearing to shared, got %q", edges)
			}

			p := sample.packages
			if aggregated.PackageRelations[p[0]][p[1]] != 1 || aggregated.PackageRelations[p[1]][p[2]] != 1 {
				t.Errorf("expected the relations %s -> %s and %s -> %s, got %v", p[0], p[1], p[1], p[2], aggregated.PackageRelations)
			}

			for i, expected := range [3][2]int32{{1, 0}, {1, 1}, {0, 1}} {
				coupling := files[i].Stmts.Analyze.Coupling
				if coupling.Efferent != expected[0] || coupling.Afferent != expected[1] {
					t.Errorf("%s: expected Ce=%d Ca=%d, got Ce=%d Ca=%d", sample.files[i][0], expected[0], expected[1], coupling.Efferent, coupling.Afferent)
				}
			}
			if !strings.Contains(sample.language, "#") {
				// The C# using directive names a namespace and no class, which is
				// as far as the syntax goes: its classes are not coupled by name.
				entrypoint := enginePkg.GetClassesInFile(files[0])[0]
				if got := entrypoint.Stmts.Analyze.Coupling.Efferent; got != 1 {
					t.Errorf("expected the Entrypoint class to depend on one class, got %d", got)
				}
			}
		})
	}
}

func sortedCopy(values []string) []string {
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return sorted
}
