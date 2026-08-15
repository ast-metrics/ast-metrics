package analyzer

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	enginePkg "github.com/ast-metrics/ast-metrics/internal/engine"
	golangengine "github.com/ast-metrics/ast-metrics/internal/engine/golang"
	phpengine "github.com/ast-metrics/ast-metrics/internal/engine/php"
	pythonengine "github.com/ast-metrics/ast-metrics/internal/engine/python"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// graphOf parses the given PHP sources and returns the graph built out of them.
func graphOf(t *testing.T, sources ...string) *Aggregated {
	t.Helper()

	files := make([]*pb.File, 0, len(sources))
	for _, source := range sources {
		files = append(files, parsedBy(t, &phpengine.PhpRunner{}, source))
	}
	return graphOfFiles(files...)
}

// graphOfFiles returns the graph built out of already parsed files.
func graphOfFiles(files ...*pb.File) *Aggregated {
	aggregated := NewAggregator(files, nil).Aggregates().Combined
	return &aggregated
}

// parsedBy parses a source with the given engine and analyzes it.
func parsedBy(t *testing.T, parser enginePkg.Engine, source string) *pb.File {
	t.Helper()
	file, err := enginePkg.CreateTestFileWithCode(parser, source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	AnalyzeFile(file)
	return file
}

// nodesOf returns the sorted identifiers of the nodes of the graph.
func nodesOf(aggregated *Aggregated) []string {
	nodes := make([]string, 0, len(aggregated.Graph.Nodes))
	for id := range aggregated.Graph.Nodes {
		nodes = append(nodes, id)
	}
	slices.Sort(nodes)
	return nodes
}

// phpModule is a module of a project, holding one class under the given
// namespace and using the Service class of another namespace, so that the graph
// has an edge to draw. An empty dependsOn leaves the module depending on nothing.
func phpModule(namespace string, dependsOn string) string {
	if dependsOn == "" {
		return fmt.Sprintf(`<?php
namespace %s;

class Entrypoint {
    public function run(): int { return 1; }
}
`, namespace)
	}
	return fmt.Sprintf(`<?php
namespace %s;

use %s\Service;

class Entrypoint {
    private Service $service;
    public function __construct(Service $service) { $this->service = $service; }
}
`, namespace, dependsOn)
}

// The case of https://github.com/ast-metrics/ast-metrics/issues/140: a project
// whose namespaces all begin with the same three levels used to end up on a
// single node, which left the whole graph empty.
func TestGraphKeepsTheModulesApartUnderADeepSharedRoot(t *testing.T) {
	root := `Company\Project\SubProject`
	aggregated := graphOf(t,
		phpModule(root+`\Artifact`, root+`\Clearing`),
		phpModule(root+`\Clearing`, root+`\Import`),
		phpModule(root+`\Import`, root+`\Shared`),
		phpModule(root+`\Shared`, ""),
	)

	expected := []string{
		root + `\Artifact`,
		root + `\Clearing`,
		root + `\Import`,
		root + `\Shared`,
	}
	if got := nodesOf(aggregated); !slices.Equal(got, expected) {
		t.Fatalf("graph nodes = %q, expected %q", got, expected)
	}

	edges := aggregated.Graph.Nodes[root+`\Artifact`].Edges
	if !slices.Contains(edges, root+`\Clearing`) {
		t.Errorf("expected an edge from Artifact to Clearing, got %q", edges)
	}
}

// A project rooted near the top keeps the depth it has always been cut at.
func TestGraphKeepsTheDefaultDepthUnderAShallowRoot(t *testing.T) {
	aggregated := graphOf(t,
		phpModule(`App\Service\Mailer`, `App\Service\Queue`),
		phpModule(`App\Service\Queue`, `App\Http\Router`),
		phpModule(`App\Http\Router`, ""),
	)

	expected := []string{`App\Http\Router`, `App\Service\Mailer`, `App\Service\Queue`}
	if got := nodesOf(aggregated); !slices.Equal(got, expected) {
		t.Fatalf("graph nodes = %q, expected %q", got, expected)
	}
}

// The classes of the project are cut at the depth its own root calls for, and
// the libraries it uses at the plain one: their insides are not the subject.
func TestGraphCutsForeignNamespacesAtTheDefaultDepth(t *testing.T) {
	root := `Company\Project\SubProject`
	aggregated := graphOf(t,
		phpModule(root+`\Artifact`, `Symfony\Component\HttpFoundation`),
		phpModule(root+`\Clearing`, root+`\Artifact`),
	)

	nodes := nodesOf(aggregated)
	if !slices.Contains(nodes, `Symfony\Component\HttpFoundation`) {
		t.Errorf("expected the library to be cut at three levels, got %q", nodes)
	}
	if !slices.Contains(nodes, root+`\Artifact`) {
		t.Errorf("expected the project to be cut below its root, got %q", nodes)
	}
}

// A Go package used to name itself with a bare word while every file importing
// it named it by its import path, so the two ends of an edge were never written
// in the same language and no Go package was ever linked to another.
func TestGraphLinksGoPackagesToEachOther(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	packages := map[string]string{"analyzer": "engine", "engine": "report", "report": "analyzer"}
	runner := &golangengine.GolangRunner{}
	files := make([]*pb.File, 0, len(packages))
	for name, imported := range packages {
		directory := filepath.Join(root, "internal", name)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
		source := fmt.Sprintf("package %s\n\nimport \"example.com/demo/internal/%s\"\n\nvar _ = %s.Name\n",
			name, imported, imported)
		path := filepath.Join(directory, "source.go")
		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
		file, err := runner.Parse(path)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		AnalyzeFile(file)
		files = append(files, file)
	}

	aggregated := NewAggregator(files, nil).Aggregates().Combined

	expected := []string{
		"example.com/demo/internal/analyzer",
		"example.com/demo/internal/engine",
		"example.com/demo/internal/report",
	}
	if got := nodesOf(&aggregated); !slices.Equal(got, expected) {
		t.Fatalf("graph nodes = %q, expected %q", got, expected)
	}
	for name, imported := range packages {
		from := "example.com/demo/internal/" + name
		to := "example.com/demo/internal/" + imported
		if !slices.Contains(aggregated.Graph.Nodes[from].Edges, to) {
			t.Errorf("expected an edge from %q to %q, got %q", from, to, aggregated.Graph.Nodes[from].Edges)
		}
		// A package of the project is not a third-party dependency.
		if aggregated.ExternalNodes[from] {
			t.Errorf("%q is a package of the project, not an external one", from)
		}
	}
}

// Whatever the depth the namespaces end up cut at, the communities have to be
// looked up under the very identifiers the graph is made of.
func TestCommunitiesAreKeyedOnTheNodesOfTheGraph(t *testing.T) {
	root := `Company\Project\SubProject`
	aggregated := graphOf(t,
		phpModule(root+`\Artifact`, root+`\Clearing`),
		phpModule(root+`\Clearing`, root+`\Import`),
		phpModule(root+`\Import`, ""),
	)

	if aggregated.Community == nil || len(aggregated.Community.NodeToCommunity) == 0 {
		t.Fatal("expected the communities to be detected")
	}
	for node := range aggregated.Community.NodeToCommunity {
		if !strings.HasPrefix(node, root) {
			continue
		}
		if aggregated.Graph.Nodes[node] == nil {
			t.Errorf("community keyed on %q, which is not a node of the graph", node)
		}
	}
}

// The root of a project is what its production code shares. A test suite kept
// under a namespace of its own, Tests\Unit beside Company\Project\SubProject,
// shares nothing with it, and must not leave the project with no root at all.
func TestGraphRootIgnoresTheTestFiles(t *testing.T) {
	root := `Company\Project\SubProject`
	tests := parsedBy(t, &phpengine.PhpRunner{}, phpModule(`Tests\Unit\Artifact`, root+`\Artifact`))
	tests.IsTest = true

	aggregated := graphOfFiles(
		parsedBy(t, &phpengine.PhpRunner{}, phpModule(root+`\Artifact`, root+`\Clearing`)),
		parsedBy(t, &phpengine.PhpRunner{}, phpModule(root+`\Clearing`, root+`\Import`)),
		parsedBy(t, &phpengine.PhpRunner{}, phpModule(root+`\Import`, "")),
		tests,
	)

	nodes := nodesOf(aggregated)
	for _, expected := range []string{root + `\Artifact`, root + `\Clearing`, root + `\Import`} {
		if !slices.Contains(nodes, expected) {
			t.Errorf("expected the node %q, got %q", expected, nodes)
		}
	}
	// The test namespace is foreign to the root, and cut at the plain depth.
	if !slices.Contains(nodes, `Tests\Unit\Artifact`) {
		t.Errorf("expected the tests to keep their own node, got %q", nodes)
	}
}

// Every language has a root of its own: the Python scripts kept beside a PHP
// project name their modules after bare file names, which share nothing with
// the namespaces of the PHP code, and must not decide how those are cut.
func TestGraphRootIsFoundPerLanguage(t *testing.T) {
	root := `Company\Project\SubProject`
	aggregated := graphOfFiles(
		parsedBy(t, &phpengine.PhpRunner{}, phpModule(root+`\Artifact`, root+`\Clearing`)),
		parsedBy(t, &phpengine.PhpRunner{}, phpModule(root+`\Clearing`, root+`\Import`)),
		parsedBy(t, &phpengine.PhpRunner{}, phpModule(root+`\Import`, "")),
		parsedBy(t, &pythonengine.PythonRunner{}, "import os\n\nclass Script:\n    def run(self):\n        return os.getcwd()\n"),
	)

	nodes := nodesOf(aggregated)
	for _, expected := range []string{root + `\Artifact`, root + `\Clearing`, root + `\Import`} {
		if !slices.Contains(nodes, expected) {
			t.Errorf("expected the node %q, got %q", expected, nodes)
		}
	}
}
