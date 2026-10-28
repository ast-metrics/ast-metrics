package analyzer

import (
	"sort"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/dependency"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// FileDependencyGraph is the project-level view of dependencies between
// analyzed files. Import syntax remains in the AST; this graph stores the
// result of resolving those imports against the other files in the scope.
type FileDependencyGraph struct {
	Efferent map[string][]string
	Afferent map[string][]string
	// Libraries maps a file onto the modules it imports that resolve to no
	// file of the scope: the standard library, a framework, a package, or a
	// file the analysis was not given. A module is spelled the way the import
	// spells it and nothing is reduced, so that a query can pin
	// "org.apache.logging.log4j.core" rather than "org.apache".
	Libraries map[string][]string
	// LibraryUsers is the inverse of Libraries: the files importing a module.
	LibraryUsers map[string][]string
	// StandardLibraries holds the modules of Libraries that ship with the
	// language, when its engine can tell: "fmt", "java.util", "System.IO".
	StandardLibraries map[string]struct{}
}

// FileDependencyAnalyzer resolves AST dependencies to analyzed files. This is
// a semantic analysis step: reporters only project the resulting graph into
// their output format.
type FileDependencyAnalyzer struct {
	resolvers []dependency.Resolver
}

func NewFileDependencyAnalyzer(resolvers ...dependency.Resolver) *FileDependencyAnalyzer {
	return &FileDependencyAnalyzer{resolvers: resolvers}
}

func (a *FileDependencyAnalyzer) Calculate(aggregate *Aggregated) {
	if aggregate == nil {
		return
	}
	aggregate.FileDependencies = resolveFileDependencies(aggregate.ConcernedFiles, a.resolvers...)
}

// uniqueFileIndex deliberately leaves ambiguous names unresolved. Choosing the
// first class encountered would make the graph depend on goroutine scheduling
// because aggregate file order is not stable.
//
// Names are qualified by the language that declares them. Without that, a Rust
// `User` and a Java `User` are the same key, and a polyglot repository loses
// the edges of both: two languages that cannot import each other would still
// make each other's names ambiguous.
type uniqueFileIndex struct {
	paths     map[nameInLanguage]string
	ambiguous map[nameInLanguage]struct{}
}

type nameInLanguage struct {
	language string
	name     string
}

func newUniqueFileIndex() *uniqueFileIndex {
	return &uniqueFileIndex{
		paths:     make(map[nameInLanguage]string),
		ambiguous: make(map[nameInLanguage]struct{}),
	}
}

func (index *uniqueFileIndex) add(language, name, path string) {
	if name == "" || path == "" {
		return
	}
	key := nameInLanguage{language: language, name: name}
	if _, ambiguous := index.ambiguous[key]; ambiguous {
		return
	}
	if existing, found := index.paths[key]; found {
		if existing != path {
			delete(index.paths, key)
			index.ambiguous[key] = struct{}{}
		}
		return
	}
	index.paths[key] = path
}

func (index *uniqueFileIndex) get(language, name string) string {
	return index.paths[nameInLanguage{language: language, name: name}]
}

func resolveFileDependencies(files []*pb.File, resolvers ...dependency.Resolver) FileDependencyGraph {
	graph := FileDependencyGraph{
		Efferent:          make(map[string][]string),
		Afferent:          make(map[string][]string),
		Libraries:         make(map[string][]string),
		LibraryUsers:      make(map[string][]string),
		StandardLibraries: make(map[string]struct{}),
	}

	analyzedPaths := make(map[string]struct{}, len(files))
	classToFile := newUniqueFileIndex()
	for _, file := range files {
		if file == nil || file.GetPath() == "" {
			continue
		}
		analyzedPaths[file.GetPath()] = struct{}{}
		if file.Stmts == nil {
			continue
		}
		language := file.GetProgrammingLanguage()
		for _, class := range engine.GetClassesInFile(file) {
			if class == nil || class.Name == nil {
				continue
			}
			classToFile.add(language, class.Name.GetQualified(), file.Path)
			classToFile.add(language, class.Name.GetShort(), file.Path)
		}
		for _, itf := range engine.GetInterfacesInFile(file) {
			if itf == nil || itf.Name == nil {
				continue
			}
			classToFile.add(language, itf.Name.GetQualified(), file.Path)
			classToFile.add(language, itf.Name.GetShort(), file.Path)
		}
	}

	scopedResolvers := make([]dependency.ScopedResolver, 0, len(resolvers))
	for _, resolver := range resolvers {
		if resolver != nil {
			if scoped := resolver.ForFiles(files); scoped != nil {
				scopedResolvers = append(scopedResolvers, scoped)
			}
		}
	}

	edges := make(map[string]map[string]struct{})
	imports := make(map[string]map[string]struct{})
	for _, file := range files {
		if file == nil || file.GetPath() == "" || file.Stmts == nil {
			continue
		}
		for _, external := range engine.GetDependenciesInFile(file) {
			if external == nil {
				continue
			}

			var targets []string
			var owner dependency.ScopedResolver
			handled := false
			for _, resolver := range scopedResolvers {
				targets, handled = resolver.Resolve(file, external)
				if handled {
					owner = resolver
					break
				}
			}
			if !handled {
				language := file.GetProgrammingLanguage()
				target := classToFile.get(language, external.GetNamespace())
				if target == "" {
					target = classToFile.get(language, external.GetClassName())
				}
				targets = []string{target}
			}

			resolved := false
			for _, target := range targets {
				if _, analyzed := analyzedPaths[target]; !analyzed {
					continue
				}
				resolved = true
				if target == file.Path {
					continue
				}
				if edges[file.Path] == nil {
					edges[file.Path] = make(map[string]struct{})
				}
				edges[file.Path][target] = struct{}{}
			}
			if resolved {
				continue
			}
			// A simple name with no module ("List", "string") stands for a
			// type the file did not import; a relative module that resolves
			// to nothing is a broken import. Neither is a library.
			module := external.GetNamespace()
			if module == "" || dependency.IsRelative(module) {
				continue
			}
			if teller, tells := owner.(dependency.LibraryTeller); tells && !teller.IsLibrary(file, module) {
				continue
			}
			if imports[file.Path] == nil {
				imports[file.Path] = make(map[string]struct{})
			}
			imports[file.Path][module] = struct{}{}
			if teller, tells := owner.(dependency.StandardLibraryTeller); tells && teller.IsStandardLibrary(module) {
				graph.StandardLibraries[module] = struct{}{}
			}
		}
	}

	for source, targets := range edges {
		for target := range targets {
			graph.Efferent[source] = append(graph.Efferent[source], target)
			graph.Afferent[target] = append(graph.Afferent[target], source)
		}
	}
	for source := range graph.Efferent {
		sort.Strings(graph.Efferent[source])
	}
	for target := range graph.Afferent {
		sort.Strings(graph.Afferent[target])
	}
	for file, modules := range imports {
		for module := range modules {
			graph.Libraries[file] = append(graph.Libraries[file], module)
			graph.LibraryUsers[module] = append(graph.LibraryUsers[module], file)
		}
	}
	for file := range graph.Libraries {
		sort.Strings(graph.Libraries[file])
	}
	for module := range graph.LibraryUsers {
		sort.Strings(graph.LibraryUsers[module])
	}

	return graph
}
