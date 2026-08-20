package analyzer

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// testSupportDirectories are the directories whose files support the tests:
// the tests themselves, and the fixtures, stubs and dummies beside them.
var testSupportDirectories = map[string]bool{
	"test": true, "tests": true, "__tests__": true, "spec": true, "specs": true,
	"fixture": true, "fixtures": true, "testdata": true, "stubs": true, "mocks": true, "__mocks__": true,
}

// namespaceImportIsNotAUse tells the languages where a dependency naming a
// namespace alone is a using directive or a wildcard import: a door opened on
// a namespace, not a use of it. In Python or Go, the same shape is an import
// of a module or a package, which is used as such.
func namespaceImportIsNotAUse(language string) bool {
	switch language {
	case "C#", "Java":
		return true
	}
	return false
}

// isTestSupportPath tells whether a path sits under a test directory.
func isTestSupportPath(path string) bool {
	for _, segment := range strings.Split(filepath.ToSlash(path), "/") {
		if testSupportDirectories[strings.ToLower(segment)] {
			return true
		}
	}
	return false
}

// Granularity names what a unit of the community graph is.
const (
	// GranularityClass: a unit is a class of the project. This is what the
	// languages naming their dependencies class by class get: PHP, Java, C#,
	// TypeScript, Python when it imports names.
	GranularityClass = "class"
	// GranularityNamespace: a unit is a package or a module of the project, for
	// the languages depending on packages rather than on classes, Go first.
	GranularityNamespace = "namespace"
)

// unitGraph is the dependency graph the communities are detected on.
//
// The graph drawn on the architecture pages joins namespaces, and finding
// communities among a dozen namespaces mostly finds those namespaces back. The
// communities are therefore looked for one level below, among the classes,
// where the dependencies actually run: two classes that call each other belong
// together whatever folder each of them was filed in. Only the project's own
// code takes part: a framework is what the code stands on, not one of its
// parts, and test files are left out for the reason the coupling leaves them
// out.
//
// A language whose dependencies name packages rather than classes (Go, and any
// file that imports modules wholesale) has no class-to-class edges to offer:
// its units are its namespaces, reduced the way the architecture graph reduces
// them.
type unitGraph struct {
	// Units lists every unit taking part in an edge, sorted.
	Units []string
	// Namespace maps a unit onto the node of the architecture graph it belongs
	// to: the reduced namespace of the file declaring it.
	Namespace map[string]string
	// IsClass tells a class unit from a namespace unit.
	IsClass map[string]bool
	// IsInterface flags the units that are interfaces.
	IsInterface map[string]bool
	// IsFile flags the units standing for a file of top-level code: a script,
	// a configuration file, a package-info, in a language whose other units
	// are classes.
	IsFile map[string]bool
	// FileOf gives the file declaring a class unit, when there is one.
	FileOf map[string]*pb.File
	// Out holds the directed weighted edges: Out[a][b] is the number of
	// distinct places in a where b is used. In is the same graph read from
	// the other end: In[b][a] == Out[a][b].
	Out map[string]map[string]int
	In  map[string]map[string]int
	// Externals counts, per unit, the foreign packages it depends on, named
	// as the architecture graph names them.
	Externals map[string]map[string]int
	// Total is the number of units the project declares at this granularity,
	// whether they take part in an edge or not.
	Total int
	// Granularity is the granularity most of the units have.
	Granularity string
	// Language names the granularity each language got.
	Language map[string]string
}

// buildUnitGraph reads the dependencies of the project into a unit graph.
func buildUnitGraph(aggregate *Aggregated) *unitGraph {
	g := &unitGraph{
		Namespace:   map[string]string{},
		IsClass:     map[string]bool{},
		IsInterface: map[string]bool{},
		IsFile:      map[string]bool{},
		FileOf:      map[string]*pb.File{},
		Out:         map[string]map[string]int{},
		In:          map[string]map[string]int{},
		Externals:   map[string]map[string]int{},
		Language:    map[string]string{},
	}
	if aggregate == nil {
		return g
	}
	// Fixtures, stubs and dummies living under a test directory are not the
	// architecture either, whatever their file name says.
	production := make([]*pb.File, 0, len(aggregate.ConcernedFiles))
	for _, file := range aggregate.ConcernedFiles {
		if file != nil && !isTestSupportPath(file.Path) {
			production = append(production, file)
		}
	}
	index := indexProject(production)
	scopes := namespacesOfScopes(production)
	reduce := func(file *pb.File, namespace string) string {
		return aggregate.NamespaceReducers.Reduce(file.GetProgrammingLanguage(), namespace)
	}

	dependenciesOf := make(map[*pb.File][]*pb.StmtExternalDependency, len(index.files))
	for _, file := range index.files {
		dependenciesOf[file] = engine.GetDependenciesInFile(file)
	}

	// The nodes of the architecture graph the project owns, to tell a
	// reference to the project's own code the analysis cannot place from a
	// reference to a foreign package.
	internalNodes := map[string]bool{}
	for _, file := range index.files {
		if namespace := index.namespaceOfFile[file]; namespace != "" {
			internalNodes[reduce(file, namespace)] = true
		}
	}

	// Decide the granularity of each language on what its dependencies name:
	// a language whose internal targets are classes at least as often as
	// packages works class by class.
	classHits := map[string]int{}
	packageHits := map[string]int{}
	for _, file := range index.files {
		language := file.GetProgrammingLanguage()
		for _, dep := range dependenciesOf[file] {
			if dep == nil {
				continue
			}
			if index.targetTypeOf(dep, file) != nil {
				classHits[language]++
			} else if _, internal := index.targetNamespaceOf(dep, file); internal {
				packageHits[language]++
			}
		}
	}
	languages := map[string]struct{}{}
	for _, file := range index.files {
		languages[file.GetProgrammingLanguage()] = struct{}{}
	}
	classUnits, namespaceUnits := 0, 0
	for language := range languages {
		if classHits[language] > 0 && classHits[language] >= packageHits[language] {
			g.Language[language] = GranularityClass
		} else {
			g.Language[language] = GranularityNamespace
		}
	}

	// Count the units the project declares, so that the ones taking part in
	// no edge can be told apart from the ones that were never there.
	namespacesSeen := map[string]struct{}{}
	for _, file := range index.files {
		language := file.GetProgrammingLanguage()
		if g.Language[language] == GranularityClass {
			classUnits += len(index.typesOfFile[file])
			continue
		}
		if namespace := index.namespaceOfFile[file]; namespace != "" {
			namespacesSeen[reduce(file, namespace)] = struct{}{}
		}
	}
	namespaceUnits = len(namespacesSeen)
	g.Total = classUnits + namespaceUnits
	if classUnits >= namespaceUnits {
		g.Granularity = GranularityClass
	} else {
		g.Granularity = GranularityNamespace
	}

	// A namespace unit is filed under its parent namespace: what its
	// community is compared with is the folder above it, the way a class is
	// compared with the namespace declaring it.
	unitOfNamespace := func(file *pb.File, namespace string) string {
		node := reduce(file, namespace)
		if node == "" {
			return ""
		}
		if _, known := g.Namespace[node]; !known {
			parent := parentNamespace(node)
			if parent == "" {
				parent = node
			}
			g.Namespace[node] = parent
		}
		return node
	}
	// A file with code outside any class, in a language whose units are
	// classes: a script, a DI configuration, a package-info. The file itself
	// is the unit, filed under its namespace, rather than the namespace,
	// which would gather every such file of a package into one hub.
	unitOfFile := func(file *pb.File) string {
		id := file.Path
		if id == "" {
			return ""
		}
		if _, known := g.Namespace[id]; !known {
			g.Namespace[id] = reduce(file, index.namespaceOfFile[file])
			g.IsFile[id] = true
			g.FileOf[id] = file
		}
		return id
	}
	unitOfType := func(t *projectType) string {
		if t.Short == "@non-utf8" {
			// the engine's stand-in for a name it could not read: several
			// classes may bear it, and a unit made of them would link
			// everything they touch
			return ""
		}
		id := t.Qualified
		if _, known := g.Namespace[id]; !known {
			g.Namespace[id] = reduce(t.File, index.namespaceOfFile[t.File])
			g.IsClass[id] = true
			g.IsInterface[id] = t.Class == nil
			g.FileOf[id] = t.File
		}
		return id
	}

	for _, file := range index.files {
		language := file.GetProgrammingLanguage()
		classLevel := g.Language[language] == GranularityClass
		for _, dep := range dependenciesOf[file] {
			if dep == nil {
				continue
			}
			if dep.ClassName == "" && namespaceImportIsNotAUse(language) {
				// a using directive or a wildcard import opens a namespace,
				// it uses nothing of it: the types used are read one by one
				continue
			}
			// the unit using the dependency
			source := ""
			if classLevel {
				if t := index.sourceTypeOf(dep, file); t != nil {
					source = unitOfType(t)
					if source == "" {
						continue
					}
				} else {
					source = unitOfFile(file)
				}
			}
			if source == "" {
				source = unitOfNamespace(file, scopes.sourceOf(dep))
			}
			if source == "" {
				continue
			}
			// the unit being used
			target := ""
			if t := index.targetTypeOf(dep, file); t != nil {
				if classLevel {
					target = unitOfType(t)
					if target == "" {
						continue
					}
				} else {
					target = unitOfNamespace(t.File, index.namespaceOfFile[t.File])
				}
			} else if namespace, internal := index.targetNamespaceOf(dep, file); internal {
				target = unitOfNamespace(file, namespace)
			} else {
				foreign := reduce(file, dep.Namespace)
				if foreign == "" || internalNodes[foreign] {
					// A reference to the project's own code that cannot be
					// placed on a type or a namespace: not a foreign package.
					continue
				}
				if g.Externals[source] == nil {
					g.Externals[source] = map[string]int{}
				}
				g.Externals[source][foreign]++
				continue
			}
			if target == "" || target == source {
				continue
			}
			if g.Out[source] == nil {
				g.Out[source] = map[string]int{}
			}
			if g.In[target] == nil {
				g.In[target] = map[string]int{}
			}
			g.Out[source][target]++
			g.In[target][source]++
		}
	}

	units := map[string]struct{}{}
	for a, tos := range g.Out {
		units[a] = struct{}{}
		for b := range tos {
			units[b] = struct{}{}
		}
	}
	g.Units = slices.Sorted(maps.Keys(units))
	return g
}

// subgraphOf returns the part of a unit graph made of the given units and of
// the references between them: what a folder is, when it is analysed alone.
func subgraphOf(g *unitGraph, units []string) *unitGraph {
	keep := make(map[string]bool, len(units))
	for _, unit := range units {
		keep[unit] = true
	}
	sub := &unitGraph{
		Namespace:   map[string]string{},
		IsClass:     map[string]bool{},
		IsInterface: map[string]bool{},
		IsFile:      map[string]bool{},
		FileOf:      map[string]*pb.File{},
		Out:         map[string]map[string]int{},
		In:          map[string]map[string]int{},
		Externals:   map[string]map[string]int{},
		Total:       len(units),
		Granularity: g.Granularity,
		Language:    g.Language,
	}
	// The namespaces of the classes are cut anew, one level below what the
	// folder's classes all share: analysed alone, a folder is a project of
	// its own, whose modules are the namespaces right under its root, not
	// the levels the whole project was cut at.
	fullNamespaces := []string{}
	for _, unit := range g.Units {
		if keep[unit] && g.IsClass[unit] {
			fullNamespaces = append(fullNamespaces, parentNamespace(unit))
		}
	}
	shared := commonPrefixOfNamespaces(fullNamespaces)
	localNamespace := func(unit string) string {
		if !g.IsClass[unit] {
			return g.Namespace[unit]
		}
		full := parentNamespace(unit)
		if shared == "" || len(full) <= len(shared) {
			return full
		}
		rest := full[len(shared):]
		sep := namespaceSeparator(full)
		rest = strings.TrimPrefix(rest, sep)
		if i := strings.Index(rest, sep); i >= 0 && sep != "" {
			rest = rest[:i]
		}
		return shared + sep + rest
	}
	for _, unit := range g.Units {
		if !keep[unit] {
			continue
		}
		sub.Namespace[unit] = localNamespace(unit)
		sub.IsClass[unit] = g.IsClass[unit]
		sub.IsInterface[unit] = g.IsInterface[unit]
		sub.IsFile[unit] = g.IsFile[unit]
		if f := g.FileOf[unit]; f != nil {
			sub.FileOf[unit] = f
		}
		if ext := g.Externals[unit]; ext != nil {
			sub.Externals[unit] = ext
		}
		for to, w := range g.Out[unit] {
			if !keep[to] {
				continue
			}
			if sub.Out[unit] == nil {
				sub.Out[unit] = map[string]int{}
			}
			if sub.In[to] == nil {
				sub.In[to] = map[string]int{}
			}
			sub.Out[unit][to] = w
			sub.In[to][unit] = w
		}
	}
	linked := map[string]struct{}{}
	for a, tos := range sub.Out {
		linked[a] = struct{}{}
		for b := range tos {
			linked[b] = struct{}{}
		}
	}
	sub.Units = slices.Sorted(maps.Keys(linked))
	return sub
}
