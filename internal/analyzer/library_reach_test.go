package analyzer

import (
	"reflect"
	"testing"

	pb "github.com/ast-metrics/ast-metrics/pb"
)

func TestFileDependencyAnalyzerRecordsLibraries(t *testing.T) {
	importer := fileWithDependency("/tmp/project/source.ts", "TypeScript", "react", "useState")
	tests := []struct {
		name     string
		resolver stubResolverFactory
		want     map[string][]string
	}{
		{
			name:     "a module the resolver claims but finds nowhere is a library",
			resolver: stubResolverFactory{handled: true},
			want:     map[string][]string{importer.Path: {"react"}},
		},
		{
			name:     "a module no resolver claims and no file declares is a library",
			resolver: stubResolverFactory{handled: false},
			want:     map[string][]string{importer.Path: {"react"}},
		},
		{
			name:     "a module resolved to a file of the scope is not",
			resolver: stubResolverFactory{handled: true, targetPaths: []string{"/tmp/project/other.ts"}},
			want:     map[string][]string{},
		},
		{
			name:     "a module resolved to the importing file itself is not",
			resolver: stubResolverFactory{handled: true, targetPaths: []string{importer.Path}},
			want:     map[string][]string{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			graph := resolveFileDependencies([]*pb.File{importer, emptyFile("/tmp/project/other.ts", "TypeScript")}, test.resolver)
			if !reflect.DeepEqual(graph.Libraries, test.want) {
				t.Errorf("Libraries = %v, want %v", graph.Libraries, test.want)
			}
		})
	}
}

func TestFileDependencyAnalyzerLeavesBrokenImportsOutOfTheLibraries(t *testing.T) {
	for _, module := range []string{"", "./missing", "..", "super::gone"} {
		importer := fileWithDependency("/tmp/project/source.ts", "TypeScript", module, "Thing")
		graph := resolveFileDependencies([]*pb.File{importer}, stubResolverFactory{handled: true})
		if len(graph.Libraries) != 0 {
			t.Errorf("%q: Libraries = %v, want none", module, graph.Libraries)
		}
	}
}

func TestFileDependencyAnalyzerIndexesTheUsersOfALibrary(t *testing.T) {
	a := fileWithDependency("/tmp/project/a.ts", "TypeScript", "react", "")
	b := fileWithDependency("/tmp/project/b.ts", "TypeScript", "react", "")
	graph := resolveFileDependencies([]*pb.File{b, a}, stubResolverFactory{handled: true})
	if want := map[string][]string{"react": {a.Path, b.Path}}; !reflect.DeepEqual(graph.LibraryUsers, want) {
		t.Errorf("LibraryUsers = %v, want %v", graph.LibraryUsers, want)
	}
}

func reachSample() FileDependencyGraph {
	// a imports react; b and c depend on a; d depends on b; a depends on d,
	// which closes a cycle; e stands alone.
	return FileDependencyGraph{
		Efferent: map[string][]string{"b": {"a"}, "c": {"a"}, "d": {"b"}, "a": {"d"}},
		Afferent: map[string][]string{"a": {"b", "c"}, "b": {"d"}, "d": {"a"}},
		Libraries: map[string][]string{
			"a": {"react", "react-dom"},
			"e": {"lodash"},
		},
		LibraryUsers: map[string][]string{
			"react":     {"a"},
			"react-dom": {"a"},
			"lodash":    {"e"},
		},
	}
}

func TestReachFollowsTheDependentsLevelByLevel(t *testing.T) {
	reach := reachSample().Reach("REACT", 0)
	if want := []string{"react", "react-dom"}; !reflect.DeepEqual(reach.Modules, want) {
		t.Errorf("Modules = %v, want %v", reach.Modules, want)
	}
	if want := [][]string{{"a"}, {"b", "c"}, {"d"}}; !reflect.DeepEqual(reach.Levels, want) {
		t.Errorf("Levels = %v, want %v", reach.Levels, want)
	}
	if reach.Files() != 4 {
		t.Errorf("Files() = %d, want 4", reach.Files())
	}
}

func TestReachStopsAtTheDepthAsked(t *testing.T) {
	reach := reachSample().Reach("react", 1)
	if want := [][]string{{"a"}, {"b", "c"}}; !reflect.DeepEqual(reach.Levels, want) {
		t.Errorf("Levels = %v, want %v", reach.Levels, want)
	}
}

func TestReachOfAnUnknownModuleIsEmpty(t *testing.T) {
	for _, query := range []string{"", "vue"} {
		reach := reachSample().Reach(query, 0)
		if len(reach.Modules) != 0 || len(reach.Levels) != 0 {
			t.Errorf("%q: got %+v, want nothing", query, reach)
		}
	}
}

func TestLibraryUsesAreSortedByReach(t *testing.T) {
	uses := reachSample().LibraryUses()
	want := []LibraryUse{
		{Module: "react", Importers: 1, Reach: 4},
		{Module: "react-dom", Importers: 1, Reach: 4},
		{Module: "lodash", Importers: 1, Reach: 1},
	}
	if !reflect.DeepEqual(uses, want) {
		t.Errorf("LibraryUses() = %v, want %v", uses, want)
	}
}

func TestTouchedCommunitiesAreTheMostCoveredFirst(t *testing.T) {
	metrics := &CommunityMetrics{
		Communities: []*Community{
			{ID: "c1", ShortName: "billing"},
			{ID: "c2", ShortName: "catalog"},
			{ID: "c3", ShortName: "untouched"},
		},
		NodeToCommunity: map[string]string{"Invoice": "c1", "Payment": "c1", "Product": "c2", "Price": "c2", "Sku": "c2", "Other": "c3"},
		UnitFiles:       map[string]string{"Invoice": "invoice", "Payment": "payment", "Product": "product", "Price": "price", "Sku": "sku", "Other": "other"},
	}
	touched := metrics.Touched([]string{"invoice", "payment", "product"})
	want := []CommunityTouch{
		{ID: "c1", Name: "billing", Reached: 2, Files: 2},
		{ID: "c2", Name: "catalog", Reached: 1, Files: 3},
	}
	if !reflect.DeepEqual(touched, want) {
		t.Errorf("Touched() = %v, want %v", touched, want)
	}
	if (*CommunityMetrics)(nil).Touched([]string{"invoice"}) != nil {
		t.Error("no community, no touch")
	}
}
