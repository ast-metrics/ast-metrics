// Package conformance also checks that a dependency between two files is
// found in every supported language.
//
// Unlike complexity or volume, this is not a number that must agree across
// languages: an import means a genuinely different thing in each of them. A Go
// import names a directory, a Java import names a type, a C# using names a
// namespace, a Rust use names a module and a Python import names either a
// module or an object inside one. What must hold everywhere is the outcome:
// when a file reads another file of the project, the graph says so, and when
// two candidates share a name, the one the language would pick is the one that
// wins.
//
// The fixtures are written to disk rather than kept in memory, because that is
// where the answers are: go.mod carries the module path, Cargo.toml the crate
// name, and the __init__.py chain the boundary of a Python package.
package conformance

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/csharp"
	"github.com/ast-metrics/ast-metrics/internal/engine/golang"
	"github.com/ast-metrics/ast-metrics/internal/engine/java"
	"github.com/ast-metrics/ast-metrics/internal/engine/php"
	"github.com/ast-metrics/ast-metrics/internal/engine/python"
	"github.com/ast-metrics/ast-metrics/internal/engine/rust"
	"github.com/ast-metrics/ast-metrics/internal/engine/typescript"
)

type dependencyScenario struct {
	name string
	// why explains what the case proves, so a failure is readable.
	why string
	// files maps a path relative to the project root to its content.
	files map[string]string
	// edges maps a source file to the files it must depend on, both relative
	// to the project root. A file absent from the map must have no dependency.
	edges map[string][]string
}

// ---------------------------------------------------------------------------
// Go: an import names a package, which is a directory under the module path
// ---------------------------------------------------------------------------

var goScenarios = []dependencyScenario{
	{
		name: "import path built from the module path",
		why:  "the specifier is example.com/demo/internal/model, which no type name can ever match",
		files: map[string]string{
			"go.mod":                  "module example.com/demo\n\ngo 1.21\n",
			"internal/model/user.go":  "package model\n\ntype User struct{ Name string }\n",
			"internal/store/store.go": "package store\n\nimport \"example.com/demo/internal/model\"\n\ntype Store struct{ Users []model.User }\n",
			"cmd/app/main.go":         "package main\n\nimport (\n\t\"fmt\"\n\t\"example.com/demo/internal/store\"\n)\n\nfunc main() { fmt.Println(store.Store{}) }\n",
		},
		edges: map[string][]string{
			"internal/store/store.go": {"internal/model/user.go"},
			// "fmt" is outside the analyzed sources and must not become an edge.
			"cmd/app/main.go": {"internal/store/store.go"},
		},
	},
	{
		name: "a package is every file of its directory",
		why:  "Go compiles a directory as one unit, so importing it depends on all of it",
		files: map[string]string{
			"go.mod":            "module example.com/demo\n",
			"pkg/data/user.go":  "package data\n\ntype User struct{}\n",
			"pkg/data/order.go": "package data\n\ntype Order struct{}\n",
			"app.go":            "package main\n\nimport \"example.com/demo/pkg/data\"\n\nvar u = data.User{}\n",
		},
		edges: map[string][]string{
			"app.go": {"pkg/data/order.go", "pkg/data/user.go"},
		},
	},
	{
		name: "two modules in one repository",
		why:  "each import path is read against the go.mod nearest to the importing file",
		files: map[string]string{
			"a/go.mod":         "module example.com/a\n",
			"a/thing/thing.go": "package thing\n\ntype Thing struct{}\n",
			"a/main.go":        "package main\n\nimport \"example.com/a/thing\"\n\nvar t = thing.Thing{}\n",
			"b/go.mod":         "module example.com/b\n",
			"b/thing/thing.go": "package thing\n\ntype Thing struct{}\n",
			"b/main.go":        "package main\n\nimport \"example.com/b/thing\"\n\nvar t = thing.Thing{}\n",
		},
		edges: map[string][]string{
			"a/main.go": {"a/thing/thing.go"},
			"b/main.go": {"b/thing/thing.go"},
		},
	},
}

// ---------------------------------------------------------------------------
// Java: an import names a type by its fully qualified name
// ---------------------------------------------------------------------------

var javaScenarios = []dependencyScenario{
	{
		name: "the package tells two types of the same name apart",
		why:  "com.demo.a.Item and com.demo.b.Item are two types; matching on Item alone can only guess",
		files: map[string]string{
			"src/com/demo/a/Item.java": "package com.demo.a;\npublic class Item { public int id; }\n",
			"src/com/demo/b/Item.java": "package com.demo.b;\npublic class Item { public String label; }\n",
			"src/com/demo/App.java":    "package com.demo;\nimport com.demo.a.Item;\npublic class App { Item it = new Item(); }\n",
		},
		edges: map[string][]string{
			"src/com/demo/App.java": {"src/com/demo/a/Item.java"},
		},
	},
	{
		name: "a wildcard import depends on the whole package",
		why:  "import com.demo.model.* names no type, so every file of the package answers",
		files: map[string]string{
			"src/com/demo/model/User.java":  "package com.demo.model;\npublic class User {}\n",
			"src/com/demo/model/Order.java": "package com.demo.model;\npublic class Order {}\n",
			"src/com/demo/App.java":         "package com.demo;\nimport com.demo.model.*;\npublic class App { User u; }\n",
		},
		edges: map[string][]string{
			"src/com/demo/App.java": {"src/com/demo/model/Order.java", "src/com/demo/model/User.java"},
		},
	},
	{
		name: "a static import names a member of a type",
		why:  "import static com.demo.util.Maths.add splits one level below the package",
		files: map[string]string{
			"src/com/demo/util/Maths.java": "package com.demo.util;\npublic class Maths { public static int add(int a, int b) { return a + b; } }\n",
			"src/com/demo/App.java":        "package com.demo;\nimport static com.demo.util.Maths.add;\npublic class App { int r = add(1, 2); }\n",
		},
		edges: map[string][]string{
			"src/com/demo/App.java": {"src/com/demo/util/Maths.java"},
		},
	},
	{
		name: "the JDK is not part of the project",
		why:  "java.util.List must stay unresolved rather than bind to a class of the project called List",
		files: map[string]string{
			"src/com/demo/List.java": "package com.demo;\npublic class List {}\n",
			"src/com/demo/App.java":  "package com.demo;\nimport java.util.List;\npublic class App { List<String> l; }\n",
		},
		edges: map[string][]string{},
	},
}

// ---------------------------------------------------------------------------
// C#: a using names a namespace, which files declare wherever they like
// ---------------------------------------------------------------------------

var csharpScenarios = []dependencyScenario{
	{
		name: "a using resolves to the files declaring the namespace",
		why:  "no class is named after a namespace, so name matching finds nothing at all in C#",
		files: map[string]string{
			"Model/User.cs":  "namespace Demo.Model;\n\npublic class User { public string Name { get; set; } }\n",
			"Model/Order.cs": "namespace Demo.Model;\n\npublic class Order { public int Id { get; set; } }\n",
			"Store/Store.cs": "using Demo.Model;\n\nnamespace Demo.Store;\n\npublic class Store { public User Find() => new User(); }\n",
		},
		edges: map[string][]string{
			"Store/Store.cs": {"Model/Order.cs", "Model/User.cs"},
		},
	},
	{
		name: "a nested namespace is not pulled in by its parent",
		why:  "C# does not import namespaces transitively",
		files: map[string]string{
			"Model/User.cs":            "namespace Demo.Model;\n\npublic class User {}\n",
			"Model/Internal/Detail.cs": "namespace Demo.Model.Internal;\n\npublic class Detail {}\n",
			"App.cs":                   "using Demo.Model;\n\nnamespace Demo;\n\npublic class App { User u; }\n",
		},
		edges: map[string][]string{
			"App.cs": {"Model/User.cs"},
		},
	},
	{
		name: "the base class library is not part of the project",
		why:  "using System must stay unresolved",
		files: map[string]string{
			"App.cs": "using System;\n\nnamespace Demo;\n\npublic class App { public void Run() { Console.WriteLine(\"x\"); } }\n",
		},
		edges: map[string][]string{},
	},
}

// ---------------------------------------------------------------------------
// Rust: a use names a module of the crate's module tree
// ---------------------------------------------------------------------------

var rustScenarios = []dependencyScenario{
	{
		name: "crate, self and super anchor a path",
		why:  "the same module is spelled three ways depending on where the file sits",
		files: map[string]string{
			"Cargo.toml":         "[package]\nname = \"demo\"\nversion = \"0.1.0\"\n",
			"src/main.rs":        "mod model;\nmod store;\n\nfn main() {}\n",
			"src/model.rs":       "pub struct User { pub name: String }\n",
			"src/store/mod.rs":   "mod inner;\nuse crate::model::User;\n\npub struct Store { pub users: Vec<User> }\n",
			"src/store/inner.rs": "use super::Store;\n\npub fn take(_s: Store) {}\n",
		},
		edges: map[string][]string{
			"src/main.rs":        {"src/model.rs", "src/store/mod.rs"},
			"src/store/mod.rs":   {"src/model.rs", "src/store/inner.rs"},
			"src/store/inner.rs": {"src/store/mod.rs"},
		},
	},
	{
		name: "a module written as a file or as a directory is the same module",
		why:  "src/a.rs and src/a/mod.rs are both the module a",
		files: map[string]string{
			"Cargo.toml":      "[package]\nname = \"demo\"\n",
			"src/lib.rs":      "mod alpha;\nmod beta;\n",
			"src/alpha.rs":    "pub struct Alpha;\n",
			"src/beta/mod.rs": "use crate::alpha::Alpha;\n\npub struct Beta(pub Alpha);\n",
		},
		edges: map[string][]string{
			"src/lib.rs":      {"src/alpha.rs", "src/beta/mod.rs"},
			"src/beta/mod.rs": {"src/alpha.rs"},
		},
	},
	{
		name: "another crate of the workspace",
		why:  "a leading crate name is read against the Cargo.toml that declares it",
		files: map[string]string{
			"core/Cargo.toml": "[package]\nname = \"demo-core\"\n",
			"core/src/lib.rs": "pub struct Config;\n",
			"app/Cargo.toml":  "[package]\nname = \"demo-app\"\n",
			"app/src/main.rs": "use demo_core::Config;\n\nfn main() { let _c = Config; }\n",
		},
		edges: map[string][]string{
			"app/src/main.rs": {"core/src/lib.rs"},
		},
	},
	{
		name: "an external crate is not part of the project",
		why:  "use std::fmt::Display must stay unresolved even next to a module called fmt",
		files: map[string]string{
			"Cargo.toml": "[package]\nname = \"demo\"\n",
			"src/lib.rs": "use std::fmt::Display;\n\npub struct Thing;\n",
		},
		edges: map[string][]string{},
	},
}

// ---------------------------------------------------------------------------
// Python: an import names a module, found through the __init__.py chain
// ---------------------------------------------------------------------------

var pythonScenarios = []dependencyScenario{
	{
		name: "absolute and relative imports reach the same module",
		why:  "the leading dots of ..model count packages upwards from the importing file",
		files: map[string]string{
			"pkg/__init__.py":     "",
			"pkg/sub/__init__.py": "",
			"pkg/model.py":        "class User:\n    pass\n",
			"pkg/sub/store.py":    "from ..model import User\n\nclass Store:\n    def add(self, u: User):\n        return u\n",
			"app.py":              "from pkg.sub.store import Store\nimport pkg.model\n\ndef main():\n    return Store()\n",
		},
		edges: map[string][]string{
			"pkg/sub/store.py": {"pkg/model.py"},
			"app.py":           {"pkg/model.py", "pkg/sub/store.py"},
		},
	},
	{
		name: "a relative import climbing one package too far",
		why:  "two modules called model, only the one the dots point at is the target",
		files: map[string]string{
			"pkg/__init__.py":     "",
			"pkg/model.py":        "class Outer:\n    pass\n",
			"pkg/sub/__init__.py": "",
			"pkg/sub/model.py":    "class Inner:\n    pass\n",
			"pkg/sub/store.py":    "from .model import Inner\n\nclass Store:\n    pass\n",
		},
		edges: map[string][]string{
			"pkg/sub/store.py": {"pkg/sub/model.py"},
		},
	},
	{
		name: "from a package import a submodule",
		why:  "in `from pkg import model` the name is a module, not an object of the package",
		files: map[string]string{
			"pkg/__init__.py": "",
			"pkg/model.py":    "class User:\n    pass\n",
			"app.py":          "from pkg import model\n\ndef main():\n    return model.User()\n",
		},
		edges: map[string][]string{
			"app.py": {"pkg/model.py"},
		},
	},
	{
		name: "a module inside a package is not reachable by its last segment",
		why:  "helpers.json is not what `import json` names, so the standard library stays unresolved",
		files: map[string]string{
			"helpers/__init__.py": "",
			"helpers/json.py":     "class Encoder:\n    pass\n",
			"app.py":              "import json\n\ndef main():\n    return json.dumps({})\n",
		},
		edges: map[string][]string{},
	},
}

// ---------------------------------------------------------------------------
// PHP and TypeScript
// ---------------------------------------------------------------------------

var phpScenarios = []dependencyScenario{
	{
		name: "a use names a class by its fully qualified name",
		why:  "the namespace makes the name unique, which is what PSR-4 maps onto a path",
		files: map[string]string{
			"src/Model/User.php":  "<?php\nnamespace App\\Model;\n\nclass User { public string $name = \"\"; }\n",
			"src/Store/Store.php": "<?php\nnamespace App\\Store;\n\nuse App\\Model\\User;\n\nclass Store { public function find(): User { return new User(); } }\n",
			"src/App.php":         "<?php\nnamespace App;\n\nuse App\\Store\\Store;\n\nclass App { public function run(): Store { return new Store(); } }\n",
		},
		edges: map[string][]string{
			"src/Store/Store.php": {"src/Model/User.php"},
			"src/App.php":         {"src/Store/Store.php"},
		},
	},
}

var typescriptScenarios = []dependencyScenario{
	{
		name: "relative specifiers, directory index and extension substitution",
		why:  "./store is the index of a directory, and ./model.js is written for the emitted file",
		files: map[string]string{
			"src/model.ts":       "export class User { constructor(public name: string) {} }\n",
			"src/store/index.ts": "import { User } from \"../model\";\n\nexport class Store { users: User[] = []; }\n",
			"src/app.ts":         "import { Store } from \"./store\";\nimport { User } from \"./model.js\";\n\nexport class App { store = new Store(); u?: User; }\n",
		},
		edges: map[string][]string{
			"src/store/index.ts": {"src/model.ts"},
			"src/app.ts":         {"src/model.ts", "src/store/index.ts"},
		},
	},
	{
		name: "a package import is not a file of the project",
		why:  "a bare specifier must not bind to a class of the project carrying that name",
		files: map[string]string{
			"src/lodash.ts": "export class Lodash {}\n",
			"src/app.ts":    "import lodash from \"lodash\";\n\nexport class App { l = lodash; }\n",
		},
		edges: map[string][]string{},
	},
}

func TestDependencyConformance(t *testing.T) {
	suites := []struct {
		language  string
		scenarios []dependencyScenario
	}{
		{langGo, goScenarios},
		{langJava, javaScenarios},
		{langCSharp, csharpScenarios},
		{langRust, rustScenarios},
		{langPython, pythonScenarios},
		{langPHP, phpScenarios},
		{langTS, typescriptScenarios},
	}

	for _, suite := range suites {
		for _, scenario := range suite.scenarios {
			t.Run(suite.language+"/"+scenario.name, func(t *testing.T) {
				root := writeProject(t, scenario.files)
				got := dependencyEdges(t, root)
				want := scenario.edges
				if want == nil {
					want = map[string][]string{}
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("%s\nexpected %v\ngot      %v", scenario.why, want, got)
				}
			})
		}
	}
}

// writeProject materializes a fixture and returns its root directory.
func writeProject(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("cannot create %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("cannot write %s: %v", path, err)
		}
	}
	return root
}

// dependencyEdges runs the whole pipeline over a directory and returns the
// resolved graph, with every path made relative to the project root.
func dependencyEdges(t *testing.T, root string) map[string][]string {
	t.Helper()

	config := configuration.NewConfiguration()
	config.SetSourcesToAnalyzePath([]string{root})

	parsed, err := engine.ParseFiles(config, []engine.Engine{
		&golang.GolangRunner{}, &php.PhpRunner{}, &python.PythonRunner{},
		&rust.RustRunner{}, &typescript.TypeScriptRunner{},
		&java.JavaRunner{}, &csharp.CSharpRunner{},
	})
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	project := analyzer.NewAggregator(analyzer.AnalyzeFiles(parsed, nil), nil).Aggregates()

	edges := map[string][]string{}
	for source, targets := range project.Combined.FileDependencies.Efferent {
		if len(targets) == 0 {
			continue
		}
		relative := make([]string, 0, len(targets))
		for _, target := range targets {
			relative = append(relative, relativeTo(t, root, target))
		}
		sort.Strings(relative)
		edges[relativeTo(t, root, source)] = relative
	}
	return edges
}

func relativeTo(t *testing.T, root, path string) string {
	t.Helper()
	relative, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatalf("%s is not under %s: %v", path, root, err)
	}
	return filepath.ToSlash(relative)
}
