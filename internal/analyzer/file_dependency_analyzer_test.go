package analyzer

import (
	"path/filepath"
	"reflect"
	"testing"

	pb "github.com/ast-metrics/ast-metrics/pb"
)

func fileWithDependency(path, language, namespace, className string) *pb.File {
	dependency := &pb.StmtExternalDependency{Namespace: namespace, ClassName: className}
	statements := &pb.Stmts{}
	if language == "TypeScript" {
		// TypeScript imports and re-exports are recorded on their namespace by
		// the tree-sitter visitor. Model that shape so tests also exercise the
		// distinction between module specifiers and class names.
		dependency.From = moduleName(path)
		statements.StmtNamespace = []*pb.StmtNamespace{{
			Stmts: &pb.Stmts{StmtExternalDependencies: []*pb.StmtExternalDependency{dependency}},
		}}
	} else {
		statements.StmtExternalDependencies = []*pb.StmtExternalDependency{dependency}
	}
	return &pb.File{
		Path:                path,
		ProgrammingLanguage: language,
		Stmts:               statements,
	}
}

func moduleName(path string) string {
	return filepath.Base(stripTypeScriptResolveExtension(path))
}

func emptyFile(path, language string) *pb.File {
	return &pb.File{Path: path, ProgrammingLanguage: language, Stmts: &pb.Stmts{}}
}

func TestFileDependencyAnalyzerResolvesTypeScriptModules(t *testing.T) {
	tests := []struct {
		name       string
		source     *pb.File
		target     *pb.File
		wantTarget bool
	}{
		{
			name:       "extensionless sibling",
			source:     fileWithDependency("/tmp/project/Foo.ts", "TypeScript", "./Bar", "Bar"),
			target:     emptyFile("/tmp/project/Bar.ts", "TypeScript"),
			wantTarget: true,
		},
		{
			name:       "directory index",
			source:     fileWithDependency("/tmp/project/Foo.ts", "TypeScript", "./bar", "Bar"),
			target:     emptyFile("/tmp/project/bar/index.ts", "TypeScript"),
			wantTarget: true,
		},
		{
			name:       "parent directory",
			source:     fileWithDependency("/tmp/project/feature/Foo.ts", "TypeScript", "../Bar", "Bar"),
			target:     emptyFile("/tmp/project/Bar.ts", "TypeScript"),
			wantTarget: true,
		},
		{
			name:       "explicit vue extension",
			source:     fileWithDependency("/tmp/project/Modal.ts", "TypeScript", "./steps/CelebrationStep.vue", "CelebrationStep"),
			target:     emptyFile("/tmp/project/steps/CelebrationStep.vue", "TypeScript"),
			wantTarget: true,
		},
		{
			name:       "modern mts extension",
			source:     fileWithDependency("/tmp/project/Foo.ts", "TypeScript", "./Bar", "Bar"),
			target:     emptyFile("/tmp/project/Bar.mts", "TypeScript"),
			wantTarget: true,
		},
		{
			name:       "external bare specifier",
			source:     fileWithDependency("/tmp/project/Foo.ts", "TypeScript", "react", "React"),
			target:     emptyFile("/tmp/project/Bar.ts", "TypeScript"),
			wantTarget: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			aggregate := &Aggregated{ConcernedFiles: []*pb.File{test.source, test.target}}
			NewFileDependencyAnalyzer().Calculate(aggregate)

			got := aggregate.FileDependencies.Efferent[test.source.Path]
			if test.wantTarget && !reflect.DeepEqual(got, []string{test.target.Path}) {
				t.Fatalf("expected %s to depend on %s, got %v", test.source.Path, test.target.Path, got)
			}
			if !test.wantTarget && len(got) != 0 {
				t.Fatalf("expected import to stay unresolved, got %v", got)
			}
			if test.wantTarget {
				wantAfferent := []string{test.source.Path}
				if gotAfferent := aggregate.FileDependencies.Afferent[test.target.Path]; !reflect.DeepEqual(gotAfferent, wantAfferent) {
					t.Fatalf("expected inverse dependency %v, got %v", wantAfferent, gotAfferent)
				}
			}
		})
	}
}

func TestFileDependencyAnalyzerUsesDeterministicTypeScriptPrecedence(t *testing.T) {
	source := fileWithDependency("/tmp/project/Foo.ts", "TypeScript", "./Bar", "Bar")
	targetTS := emptyFile("/tmp/project/Bar.ts", "TypeScript")
	targetTSX := emptyFile("/tmp/project/Bar.tsx", "TypeScript")

	for _, files := range [][]*pb.File{
		{source, targetTS, targetTSX},
		{source, targetTSX, targetTS},
	} {
		graph := resolveFileDependencies(files)
		if got := graph.Efferent[source.Path]; !reflect.DeepEqual(got, []string{targetTS.Path}) {
			t.Fatalf("expected stable .ts precedence, got %v", got)
		}
	}
}

func TestFileDependencyAnalyzerPrefersAnExplicitExtension(t *testing.T) {
	source := fileWithDependency("/tmp/project/Foo.ts", "TypeScript", "./Bar.tsx", "Bar")
	targetTS := emptyFile("/tmp/project/Bar.ts", "TypeScript")
	targetTSX := emptyFile("/tmp/project/Bar.tsx", "TypeScript")

	graph := resolveFileDependencies([]*pb.File{source, targetTS, targetTSX})

	if got := graph.Efferent[source.Path]; !reflect.DeepEqual(got, []string{targetTSX.Path}) {
		t.Fatalf("expected the explicit .tsx target, got %v", got)
	}
}

func TestFileDependencyAnalyzerOnlySubstitutesJavaScriptExtensions(t *testing.T) {
	tests := []struct {
		name      string
		specifier string
		target    string
		want      bool
	}{
		{name: "emitted js path", specifier: "./Bar.js", target: "/tmp/project/Bar.ts", want: true},
		{name: "emitted mjs path", specifier: "./Bar.mjs", target: "/tmp/project/Bar.mts", want: true},
		{name: "missing vue file", specifier: "./Bar.vue", target: "/tmp/project/Bar.ts", want: false},
		{name: "missing ts file", specifier: "./Bar.ts", target: "/tmp/project/Bar.tsx", want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := fileWithDependency("/tmp/project/Foo.ts", "TypeScript", test.specifier, "Bar")
			target := emptyFile(test.target, "TypeScript")
			graph := resolveFileDependencies([]*pb.File{source, target})
			got := graph.Efferent[source.Path]
			if test.want && !reflect.DeepEqual(got, []string{target.Path}) {
				t.Fatalf("expected %s to resolve to %s, got %v", test.specifier, target.Path, got)
			}
			if !test.want && len(got) != 0 {
				t.Fatalf("expected %s to stay unresolved, got %v", test.specifier, got)
			}
		})
	}
}

func TestFileDependencyAnalyzerDoesNotBindTypeScriptModulesToClassNames(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
	}{
		{name: "external package", namespace: "react"},
		{name: "missing relative module", namespace: "./missing"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := fileWithDependency("/tmp/project/Foo.ts", "TypeScript", test.namespace, "React")
			target := &pb.File{
				Path:                "/tmp/project/React.ts",
				ProgrammingLanguage: "TypeScript",
				Stmts: &pb.Stmts{StmtClass: []*pb.StmtClass{{
					Name: &pb.Name{Short: "React", Qualified: "React"},
				}}},
			}

			graph := resolveFileDependencies([]*pb.File{source, target})

			if got := graph.Efferent[source.Path]; len(got) != 0 {
				t.Fatalf("expected the module specifier to stay unresolved, got %v", got)
			}
		})
	}
}

func TestFileDependencyAnalyzerLeavesAmbiguousClassNamesUnresolved(t *testing.T) {
	source := fileWithDependency("/tmp/project/a.go", "Golang", "", "Shared")
	targetA := &pb.File{Path: "/tmp/project/a/shared.go", Stmts: &pb.Stmts{StmtClass: []*pb.StmtClass{{
		Name: &pb.Name{Short: "Shared", Qualified: "a.Shared"},
	}}}}
	targetB := &pb.File{Path: "/tmp/project/b/shared.go", Stmts: &pb.Stmts{StmtClass: []*pb.StmtClass{{
		Name: &pb.Name{Short: "Shared", Qualified: "b.Shared"},
	}}}}

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

func TestFileDependencyAnalyzerStillResolvesClassDependencies(t *testing.T) {
	source := fileWithDependency("/tmp/project/a.go", "Golang", "", "Bar")
	target := &pb.File{
		Path:                "/tmp/project/b.go",
		ProgrammingLanguage: "Golang",
		Stmts: &pb.Stmts{StmtClass: []*pb.StmtClass{
			{Name: &pb.Name{Short: "Bar", Qualified: "pkg\\Bar"}},
		}},
	}
	aggregate := &Aggregated{ConcernedFiles: []*pb.File{source, target}}

	NewFileDependencyAnalyzer().Calculate(aggregate)

	if got := aggregate.FileDependencies.Efferent[source.Path]; !reflect.DeepEqual(got, []string{target.Path}) {
		t.Fatalf("expected class dependency on %s, got %v", target.Path, got)
	}
}

func TestFileDependencyAnalyzerDeduplicatesAndSortsEdges(t *testing.T) {
	source := &pb.File{
		Path:                "/tmp/project/source.go",
		ProgrammingLanguage: "Golang",
		Stmts: &pb.Stmts{StmtExternalDependencies: []*pb.StmtExternalDependency{
			{ClassName: "Zulu"},
			{ClassName: "Alpha"},
			{ClassName: "Zulu"},
		}},
	}
	alpha := &pb.File{Path: "/tmp/project/alpha.go", Stmts: &pb.Stmts{StmtClass: []*pb.StmtClass{{Name: &pb.Name{Short: "Alpha"}}}}}
	zulu := &pb.File{Path: "/tmp/project/zulu.go", Stmts: &pb.Stmts{StmtClass: []*pb.StmtClass{{Name: &pb.Name{Short: "Zulu"}}}}}

	graph := resolveFileDependencies([]*pb.File{source, zulu, alpha})

	want := []string{alpha.Path, zulu.Path}
	if got := graph.Efferent[source.Path]; !reflect.DeepEqual(got, want) {
		t.Fatalf("expected sorted unique edges %v, got %v", want, got)
	}
}

func TestFileDependencyAnalyzerIsPartOfDefaultAggregation(t *testing.T) {
	source := fileWithDependency("/tmp/project/Foo.ts", "TypeScript", "./Bar", "Bar")
	target := emptyFile("/tmp/project/Bar.ts", "TypeScript")

	project := NewAggregator([]*pb.File{source, target}, nil).Aggregates()

	if got := project.Combined.FileDependencies.Efferent[source.Path]; !reflect.DeepEqual(got, []string{target.Path}) {
		t.Fatalf("expected the aggregate to expose the resolved dependency, got %v", got)
	}
}

func TestFileDependencyAnalyzerRespectsAggregationScopes(t *testing.T) {
	source := fileWithDependency("/tmp/project/src/Foo.ts", "TypeScript", "./Bar", "Bar")
	target := emptyFile("/tmp/project/src/Bar.ts", "TypeScript")
	otherLanguage := emptyFile("/tmp/project/src/helper.go", "Golang")
	otherDirectory := emptyFile("/tmp/project/lib/Other.ts", "TypeScript")
	aggregator := NewAggregator([]*pb.File{source, target, otherLanguage, otherDirectory}, nil)
	aggregator.WithAnalyzedPaths([]string{"/tmp/project/src", "/tmp/project/lib"})

	project := aggregator.Aggregates()

	assertEdge := func(name string, aggregate Aggregated) {
		t.Helper()
		if got := aggregate.FileDependencies.Efferent[source.Path]; !reflect.DeepEqual(got, []string{target.Path}) {
			t.Fatalf("expected %s scope to contain the TypeScript edge, got %v", name, got)
		}
	}
	assertEdge("combined", project.Combined)
	assertEdge("language", project.ByProgrammingLanguage["TypeScript"])
	assertEdge("directory", project.ByDirectory["/tmp/project/src"])

	if got := project.ByProgrammingLanguage["Golang"].FileDependencies.Efferent; len(got) != 0 {
		t.Fatalf("expected Golang scope to exclude TypeScript edges, got %v", got)
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
