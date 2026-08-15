package analyzer

import (
	"os"
	"path/filepath"
	"testing"

	enginePkg "github.com/ast-metrics/ast-metrics/internal/engine"
	golangengine "github.com/ast-metrics/ast-metrics/internal/engine/golang"
	javaengine "github.com/ast-metrics/ast-metrics/internal/engine/java"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// couplingOfClass returns the coupling of the named class, failing when the
// class is not found.
func couplingOfClass(t *testing.T, aggregated *Aggregated, qualified string) *pb.Coupling {
	t.Helper()
	for _, file := range aggregated.ConcernedFiles {
		for _, class := range enginePkg.GetClassesInFile(file) {
			if class.Name.Qualified == qualified {
				return class.Stmts.Analyze.Coupling
			}
		}
	}
	t.Fatalf("class %q not found", qualified)
	return nil
}

// couplingOfFile returns the coupling of the file whose path ends with the
// given suffix.
func couplingOfFile(t *testing.T, aggregated *Aggregated, suffix string) *pb.Coupling {
	t.Helper()
	for _, file := range aggregated.ConcernedFiles {
		if filepath.ToSlash(file.Path) == suffix || len(file.Path) >= len(suffix) && filepath.ToSlash(file.Path[len(file.Path)-len(suffix):]) == suffix {
			return file.Stmts.Analyze.Coupling
		}
	}
	t.Fatalf("file %q not found", suffix)
	return nil
}

// Java imports at the top of the file and names the package and the class
// apart. A class is coupled to the imports its body names, and is depended on
// by every class naming it.
func TestJavaClassesAreCoupledThroughTheirImports(t *testing.T) {
	aggregated := graphOfFiles(
		parsedBy(t, &javaengine.JavaRunner{}, "package com.acme.product.artifact;\nimport com.acme.product.clearing.Clearing;\nimport java.util.List;\npublic class Entrypoint { private Clearing c; private List<String> names; }\n"),
		parsedBy(t, &javaengine.JavaRunner{}, "package com.acme.product.clearing;\nimport com.acme.product.shared.Util;\npublic class Clearing { public void run() { Util.go(); } }\n"),
		parsedBy(t, &javaengine.JavaRunner{}, "package com.acme.product.shared;\npublic class Util { public static void go() {} }\n"),
	)

	if got := couplingOfClass(t, aggregated, "com.acme.product.artifact.Entrypoint"); got.Efferent != 2 || got.Afferent != 0 {
		t.Errorf("Entrypoint: expected Ce=2 (Clearing, List) Ca=0, got Ce=%d Ca=%d", got.Efferent, got.Afferent)
	}
	if got := couplingOfClass(t, aggregated, "com.acme.product.clearing.Clearing"); got.Efferent != 1 || got.Afferent != 1 {
		t.Errorf("Clearing: expected Ce=1 Ca=1, got Ce=%d Ca=%d", got.Efferent, got.Afferent)
	}
	if got := couplingOfClass(t, aggregated, "com.acme.product.shared.Util"); got.Efferent != 0 || got.Afferent != 1 {
		t.Errorf("Util: expected Ce=0 Ca=1, got Ce=%d Ca=%d", got.Efferent, got.Afferent)
	}
	if got := aggregated.PackageRelations["com.acme.product.artifact"]["com.acme.product.clearing"]; got != 1 {
		t.Errorf("expected one relation from artifact to clearing, got %v", aggregated.PackageRelations)
	}
}

// A file importing two classes of the same package depends on two classes,
// not on one package.
func TestJavaImportsOfTheSamePackageCountApart(t *testing.T) {
	aggregated := graphOfFiles(
		parsedBy(t, &javaengine.JavaRunner{}, "package com.acme.app;\nimport java.util.List;\nimport java.util.Map;\npublic class Registry { private List<String> a; private Map<String, String> b; }\n"),
	)
	if got := couplingOfClass(t, aggregated, "com.acme.app.Registry"); got.Efferent != 2 {
		t.Errorf("expected Ce=2, got %d", got.Efferent)
	}
}

// Only the class naming an import is coupled to it: two classes of one file
// do not share the imports of the file.
func TestOnlyTheClassNamingAnImportIsCoupledToIt(t *testing.T) {
	aggregated := graphOfFiles(
		parsedBy(t, &javaengine.JavaRunner{}, "package com.acme.app;\nimport java.util.List;\nimport java.util.Map;\nclass Names { private List<String> a; }\nclass Index { private Map<String, String> b; }\n"),
	)
	if got := couplingOfClass(t, aggregated, "com.acme.app.Names"); got.Efferent != 1 {
		t.Errorf("Names: expected Ce=1 (List), got %d", got.Efferent)
	}
	if got := couplingOfClass(t, aggregated, "com.acme.app.Index"); got.Efferent != 1 {
		t.Errorf("Index: expected Ce=1 (Map), got %d", got.Efferent)
	}
}

// A Go file depends on packages, not on types: a file is depended on by
// every file importing its package, and a struct by what its methods use,
// even when they are declared outside of its body.
func TestGoFilesAreCoupledThroughTheirPackages(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sources := map[string]string{
		"internal/artifact/a.go": "package artifact\n\nimport \"example.com/demo/internal/clearing\"\n\ntype Entrypoint struct{ c clearing.Clearing }\n",
		"internal/clearing/c.go": "package clearing\n\nimport \"example.com/demo/internal/shared\"\n\ntype Clearing struct{}\n\nfunc (c Clearing) Run() { shared.Go() }\n",
		"internal/shared/s.go":   "package shared\n\nfunc Go() {}\n",
	}
	runner := &golangengine.GolangRunner{}
	files := make([]*pb.File, 0, len(sources))
	for _, relative := range []string{"internal/artifact/a.go", "internal/clearing/c.go", "internal/shared/s.go"} {
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(sources[relative]), 0o644); err != nil {
			t.Fatal(err)
		}
		file, err := runner.Parse(path)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		AnalyzeFile(file)
		files = append(files, file)
	}
	aggregated := graphOfFiles(files...)

	if got := couplingOfFile(t, aggregated, "internal/artifact/a.go"); got.Efferent != 1 || got.Afferent != 0 {
		t.Errorf("artifact: expected Ce=1 Ca=0, got Ce=%d Ca=%d", got.Efferent, got.Afferent)
	}
	if got := couplingOfFile(t, aggregated, "internal/clearing/c.go"); got.Efferent != 1 || got.Afferent != 1 {
		t.Errorf("clearing: expected Ce=1 Ca=1, got Ce=%d Ca=%d", got.Efferent, got.Afferent)
	}
	if got := couplingOfFile(t, aggregated, "internal/shared/s.go"); got.Efferent != 0 || got.Afferent != 1 {
		t.Errorf("shared: expected Ce=0 Ca=1, got Ce=%d Ca=%d", got.Efferent, got.Afferent)
	}
	// The method using shared is declared outside of the struct, and still
	// couples it.
	if got := couplingOfClass(t, aggregated, `clearing\Clearing`); got.Efferent != 1 {
		t.Errorf("Clearing: expected Ce=1 through its method, got %d", got.Efferent)
	}
	// The graph names the packages, never the structs.
	for id := range aggregated.Graph.Nodes {
		if id == `clearing\Clearing` || id == `artifact\Entrypoint` {
			t.Errorf("a struct is drawn as a node of the graph: %q", id)
		}
	}
}
