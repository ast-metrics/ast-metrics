package analyzer

import (
	"reflect"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/engine/dependency"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

func fileWithDependency(path, language, namespace, className string) *pb.File {
	dependency := &pb.StmtExternalDependency{Namespace: namespace, ClassName: className}
	return &pb.File{
		Path:                path,
		ProgrammingLanguage: language,
		Stmts: &pb.Stmts{
			StmtExternalDependencies: []*pb.StmtExternalDependency{dependency},
		},
	}
}

func emptyFile(path, language string) *pb.File {
	return &pb.File{Path: path, ProgrammingLanguage: language, Stmts: &pb.Stmts{}}
}

type stubResolverFactory struct {
	targetPaths []string
	handled     bool
}

func (resolver stubResolverFactory) ForFiles([]*pb.File) dependency.ScopedResolver {
	return resolver
}

func (resolver stubResolverFactory) Resolve(*pb.File, *pb.StmtExternalDependency) ([]string, bool) {
	return resolver.targetPaths, resolver.handled
}

func TestFileDependencyAnalyzerDelegatesResolution(t *testing.T) {
	source := fileWithDependency("/tmp/project/source.go", "Golang", "", "Target")
	target := &pb.File{
		Path:  "/tmp/project/target.go",
		Stmts: &pb.Stmts{StmtClass: []*pb.StmtClass{{Name: &pb.Name{Short: "Target"}}}},
	}
	sibling := &pb.File{Path: "/tmp/project/sibling.go", Stmts: &pb.Stmts{}}

	tests := []struct {
		name     string
		resolver stubResolverFactory
		want     []string
	}{
		{
			name:     "resolver returns a target",
			resolver: stubResolverFactory{targetPaths: []string{target.Path}, handled: true},
			want:     []string{target.Path},
		},
		{
			name:     "an import naming a package depends on all of its files",
			resolver: stubResolverFactory{targetPaths: []string{sibling.Path, target.Path}, handled: true},
			want:     []string{sibling.Path, target.Path},
		},
		{
			name:     "handled unresolved dependency does not fall back to a symbol",
			resolver: stubResolverFactory{handled: true},
		},
		{
			name:     "resolver cannot create an edge outside the analysis scope",
			resolver: stubResolverFactory{targetPaths: []string{"/outside/project.go"}, handled: true},
		},
		{
			name:     "targets outside the scope are dropped, the others kept",
			resolver: stubResolverFactory{targetPaths: []string{"/outside/project.go", target.Path}, handled: true},
			want:     []string{target.Path},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			graph := resolveFileDependencies([]*pb.File{source, target, sibling}, test.resolver)
			if got := graph.Efferent[source.Path]; !reflect.DeepEqual(got, test.want) {
				t.Fatalf("expected dependencies %v, got %v", test.want, got)
			}
		})
	}
}

// classInFile builds a file declaring one class, for the name-matching
// fallback that serves the languages with no resolver of their own.
func classInFile(path, language, short, qualified string) *pb.File {
	return &pb.File{
		Path:                path,
		ProgrammingLanguage: language,
		Stmts: &pb.Stmts{StmtClass: []*pb.StmtClass{{
			Name: &pb.Name{Short: short, Qualified: qualified},
		}}},
	}
}

func TestFileDependencyAnalyzerLeavesAmbiguousClassNamesUnresolved(t *testing.T) {
	source := fileWithDependency("/tmp/project/a.example", "Example", "", "Shared")
	targetA := classInFile("/tmp/project/a/shared.example", "Example", "Shared", "a.Shared")
	targetB := classInFile("/tmp/project/b/shared.example", "Example", "Shared", "b.Shared")

	for _, files := range [][]*pb.File{
		{source, targetA, targetB},
		{source, targetB, targetA},
	} {
		graph := resolveFileDependencies(files)
		if got := graph.Efferent[source.Path]; len(got) != 0 {
			t.Fatalf("expected ambiguous class name to stay unresolved, got %v", got)
		}
	}
}

// Two languages cannot import each other, so a name declared in both is not
// ambiguous. Sharing one index between them used to drop the edges of both.
func TestFileDependencyAnalyzerKeepsNamesOfDifferentLanguagesApart(t *testing.T) {
	source := fileWithDependency("/tmp/project/a.example", "Example", "", "User")
	target := classInFile("/tmp/project/user.example", "Example", "User", "a.User")
	homonym := classInFile("/tmp/project/user.other", "Other", "User", "b.User")

	graph := resolveFileDependencies([]*pb.File{source, target, homonym})

	if got := graph.Efferent[source.Path]; !reflect.DeepEqual(got, []string{target.Path}) {
		t.Fatalf("expected the edge to survive a homonym in another language, got %v", got)
	}
}

func TestFileDependencyAnalyzerStillResolvesClassDependencies(t *testing.T) {
	source := fileWithDependency("/tmp/project/a.example", "Example", "", "Bar")
	target := classInFile("/tmp/project/b.example", "Example", "Bar", "pkg\\Bar")
	aggregate := &Aggregated{ConcernedFiles: []*pb.File{source, target}}

	NewFileDependencyAnalyzer().Calculate(aggregate)

	if got := aggregate.FileDependencies.Efferent[source.Path]; !reflect.DeepEqual(got, []string{target.Path}) {
		t.Fatalf("expected class dependency on %s, got %v", target.Path, got)
	}
}

func TestFileDependencyAnalyzerDeduplicatesAndSortsEdges(t *testing.T) {
	source := &pb.File{
		Path:                "/tmp/project/source.example",
		ProgrammingLanguage: "Example",
		Stmts: &pb.Stmts{StmtExternalDependencies: []*pb.StmtExternalDependency{
			{ClassName: "Zulu"},
			{ClassName: "Alpha"},
			{ClassName: "Zulu"},
		}},
	}
	alpha := classInFile("/tmp/project/alpha.example", "Example", "Alpha", "")
	zulu := classInFile("/tmp/project/zulu.example", "Example", "Zulu", "")

	graph := resolveFileDependencies([]*pb.File{source, zulu, alpha})

	want := []string{alpha.Path, zulu.Path}
	if got := graph.Efferent[source.Path]; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected sorted unique edges %v, got %v", want, got)
	}
}

func TestFileDependencyAnalyzerRespectsAggregationScopes(t *testing.T) {
	source := fileWithDependency("/tmp/project/src/source.example", "Example", "external", "Missing")
	target := emptyFile("/tmp/project/src/target.example", "Example")
	otherLanguage := emptyFile("/tmp/project/src/helper.go", "Golang")
	otherDirectory := emptyFile("/tmp/project/lib/other.example", "Example")
	aggregator := NewAggregator([]*pb.File{source, target, otherLanguage, otherDirectory}, nil)
	aggregator.WithAnalyzedPaths([]string{"/tmp/project/src", "/tmp/project/lib"})
	aggregator.WithAggregateAnalyzer(NewFileDependencyAnalyzer(stubResolverFactory{
		targetPaths: []string{target.Path},
		handled:     true,
	}))

	project := aggregator.Aggregates()

	assertEdge := func(name string, aggregate Aggregated) {
		t.Helper()
		if got := aggregate.FileDependencies.Efferent[source.Path]; !reflect.DeepEqual(got, []string{target.Path}) {
			t.Fatalf("expected %s scope to contain the resolved edge, got %v", name, got)
		}
	}
	assertEdge("combined", project.Combined)
	assertEdge("language", project.ByProgrammingLanguage["Example"])
	assertEdge("directory", project.ByDirectory["/tmp/project/src"])

	if got := project.ByProgrammingLanguage["Golang"].FileDependencies.Efferent; len(got) != 0 {
		t.Fatalf("expected Golang scope to exclude other-language edges, got %v", got)
	}
	if got := project.ByDirectory["/tmp/project/lib"].FileDependencies.Efferent; len(got) != 0 {
		t.Fatalf("expected unrelated directory scope to exclude the edge, got %v", got)
	}
}

func TestFileDependencyAnalyzerAcceptsNilAndEmptyInputs(t *testing.T) {
	NewFileDependencyAnalyzer().Calculate(nil)

	aggregate := &Aggregated{ConcernedFiles: []*pb.File{nil, {}}}
	NewFileDependencyAnalyzer().Calculate(aggregate)
	if aggregate.FileDependencies.Efferent == nil || aggregate.FileDependencies.Afferent == nil {
		t.Fatal("expected initialized empty dependency maps")
	}
}
