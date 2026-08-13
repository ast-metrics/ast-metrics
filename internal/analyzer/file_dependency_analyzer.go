package analyzer

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// FileDependencyGraph is the project-level view of dependencies between
// analyzed files. Import syntax remains in the AST; this graph stores the
// result of resolving those imports against the other files in the scope.
type FileDependencyGraph struct {
	Efferent map[string][]string
	Afferent map[string][]string
}

// FileDependencyAnalyzer resolves AST dependencies to analyzed files. This is
// a semantic analysis step: reporters only project the resulting graph into
// their output format.
type FileDependencyAnalyzer struct{}

func NewFileDependencyAnalyzer() *FileDependencyAnalyzer {
	return &FileDependencyAnalyzer{}
}

func (a *FileDependencyAnalyzer) Calculate(aggregate *Aggregated) {
	if aggregate == nil {
		return
	}
	aggregate.FileDependencies = resolveFileDependencies(aggregate.ConcernedFiles)
}

// Keep longer suffixes first: declarations must lose ".d.ts", not just ".ts".
var typeScriptResolveExtensions = []string{
	".d.mts", ".d.cts", ".d.ts",
	".mts", ".cts", ".tsx", ".ts",
	".jsx", ".js", ".mjs", ".cjs", ".vue",
}

func typeScriptResolveExtension(path string) string {
	for _, extension := range typeScriptResolveExtensions {
		if strings.HasSuffix(path, extension) {
			return extension
		}
	}
	return ""
}

func stripTypeScriptResolveExtension(path string) string {
	return strings.TrimSuffix(path, typeScriptResolveExtension(path))
}

// uniqueFileIndex deliberately leaves ambiguous names unresolved. Choosing the
// first class encountered would make the graph depend on goroutine scheduling
// because aggregate file order is not stable.
type uniqueFileIndex struct {
	paths     map[string]string
	ambiguous map[string]struct{}
}

func newUniqueFileIndex() *uniqueFileIndex {
	return &uniqueFileIndex{
		paths:     make(map[string]string),
		ambiguous: make(map[string]struct{}),
	}
}

func (index *uniqueFileIndex) add(name, path string) {
	if name == "" || path == "" {
		return
	}
	if _, ambiguous := index.ambiguous[name]; ambiguous {
		return
	}
	if existing, found := index.paths[name]; found {
		if existing != path {
			delete(index.paths, name)
			index.ambiguous[name] = struct{}{}
		}
		return
	}
	index.paths[name] = path
}

func (index *uniqueFileIndex) get(name string) string {
	return index.paths[name]
}

type typeScriptModuleCandidate struct {
	path      string
	extension string
	priority  int
	indexFile bool
}

// typeScriptModuleIndex stores every valid candidate instead of silently
// overwriting collisions such as foo.ts/foo.tsx. Candidates are sorted once,
// which keeps module resolution deterministic regardless of input order.
type typeScriptModuleIndex struct {
	exact      map[string]string
	candidates map[string][]typeScriptModuleCandidate
}

func newTypeScriptModuleIndex() *typeScriptModuleIndex {
	return &typeScriptModuleIndex{
		exact:      make(map[string]string),
		candidates: make(map[string][]typeScriptModuleCandidate),
	}
}

func (index *typeScriptModuleIndex) add(path string) {
	cleanPath := filepath.Clean(path)
	extension := typeScriptResolveExtension(cleanPath)
	if extension == "" {
		return
	}
	index.exact[cleanPath] = path

	modulePath := stripTypeScriptResolveExtension(cleanPath)
	index.addCandidate(modulePath, typeScriptModuleCandidate{
		path:      path,
		extension: extension,
		priority:  typeScriptExtensionPriority(extension),
	})
	if filepath.Base(modulePath) == "index" {
		index.addCandidate(filepath.Dir(modulePath), typeScriptModuleCandidate{
			path:      path,
			extension: extension,
			priority:  typeScriptExtensionPriority(extension),
			indexFile: true,
		})
	}
}

func (index *typeScriptModuleIndex) addCandidate(modulePath string, candidate typeScriptModuleCandidate) {
	for _, existing := range index.candidates[modulePath] {
		if existing.path == candidate.path {
			return
		}
	}
	index.candidates[modulePath] = append(index.candidates[modulePath], candidate)
}

func (index *typeScriptModuleIndex) sort() {
	for modulePath := range index.candidates {
		candidates := index.candidates[modulePath]
		sort.Slice(candidates, func(i, j int) bool {
			if candidates[i].indexFile != candidates[j].indexFile {
				return !candidates[i].indexFile
			}
			if candidates[i].priority != candidates[j].priority {
				return candidates[i].priority < candidates[j].priority
			}
			return candidates[i].path < candidates[j].path
		})
		index.candidates[modulePath] = candidates
	}
}

func typeScriptExtensionPriority(extension string) int {
	// Prefer TypeScript sources and declarations to JavaScript fallbacks when
	// an extensionless specifier has several analyzed candidates.
	if priority, found := typeScriptExtensionPriorities[extension]; found {
		return priority
	}
	return len(typeScriptResolveExtensions)
}

var typeScriptExtensionPriorities = map[string]int{
	".ts": 0, ".tsx": 1, ".mts": 2, ".cts": 3,
	".d.ts": 4, ".d.mts": 5, ".d.cts": 6,
	".js": 7, ".jsx": 8, ".mjs": 9, ".cjs": 10, ".vue": 11,
}

func isTypeScriptRelativeSpecifier(specifier string) bool {
	return specifier == "." || specifier == ".." ||
		strings.HasPrefix(specifier, "./") || strings.HasPrefix(specifier, "../")
}

func (index *typeScriptModuleIndex) resolveRelative(fromPath, specifier string) string {
	if !isTypeScriptRelativeSpecifier(specifier) {
		return ""
	}
	joined := filepath.Clean(filepath.Join(filepath.Dir(fromPath), specifier))
	requestedExtension := typeScriptResolveExtension(joined)
	if requestedExtension != "" {
		if exact := index.exact[joined]; exact != "" {
			return exact
		}
	}
	candidates := index.candidates[stripTypeScriptResolveExtension(joined)]
	for _, candidate := range candidates {
		if requestedExtension == "" || isTypeScriptExtensionSubstitution(requestedExtension, candidate.extension) {
			return candidate.path
		}
	}
	return ""
}

// TypeScript substitutes source extensions when emitted JavaScript paths are
// imported. Other explicit extensions (notably .vue) must match exactly.
func isTypeScriptExtensionSubstitution(requested, candidate string) bool {
	switch requested {
	case ".js":
		return candidate == ".ts" || candidate == ".tsx" || candidate == ".d.ts"
	case ".jsx":
		return candidate == ".tsx" || candidate == ".d.ts"
	case ".mjs":
		return candidate == ".mts" || candidate == ".d.mts"
	case ".cjs":
		return candidate == ".cts" || candidate == ".d.cts"
	default:
		return false
	}
}

type dependencyIdentity struct {
	namespace    string
	className    string
	functionName string
	from         string
}

func identityOf(dependency *pb.StmtExternalDependency) dependencyIdentity {
	return dependencyIdentity{
		namespace:    dependency.GetNamespace(),
		className:    dependency.GetClassName(),
		functionName: dependency.GetFunctionName(),
		from:         dependency.GetFrom(),
	}
}

// TypeScript imports and re-exports are attached to namespace statements by
// the parser. Keeping their identities lets us distinguish a bare module
// specifier from a class reference: `import React from "react"` must not bind
// to an unrelated internal class named React.
func typeScriptModuleDependencies(file *pb.File) map[dependencyIdentity]struct{} {
	dependencies := make(map[dependencyIdentity]struct{})
	if file == nil || file.Stmts == nil {
		return dependencies
	}
	for _, namespace := range file.Stmts.GetStmtNamespace() {
		if namespace == nil || namespace.Stmts == nil {
			continue
		}
		for _, dependency := range namespace.Stmts.GetStmtExternalDependencies() {
			if dependency != nil {
				dependencies[identityOf(dependency)] = struct{}{}
			}
		}
	}
	return dependencies
}

func resolveFileDependencies(files []*pb.File) FileDependencyGraph {
	graph := FileDependencyGraph{
		Efferent: make(map[string][]string),
		Afferent: make(map[string][]string),
	}

	classToFile := newUniqueFileIndex()
	typeScriptModules := newTypeScriptModuleIndex()
	for _, file := range files {
		if file == nil || file.Stmts == nil {
			continue
		}
		for _, class := range engine.GetClassesInFile(file) {
			if class == nil || class.Name == nil {
				continue
			}
			classToFile.add(class.Name.GetQualified(), file.Path)
			classToFile.add(class.Name.GetShort(), file.Path)
		}

		if file.GetProgrammingLanguage() == "TypeScript" {
			typeScriptModules.add(file.Path)
		}
	}
	typeScriptModules.sort()

	edges := make(map[string]map[string]struct{})
	for _, file := range files {
		if file == nil || file.Stmts == nil {
			continue
		}
		var moduleDependencies map[dependencyIdentity]struct{}
		if file.GetProgrammingLanguage() == "TypeScript" {
			moduleDependencies = typeScriptModuleDependencies(file)
		}
		for _, dependency := range engine.GetDependenciesInFile(file) {
			if dependency == nil {
				continue
			}

			namespace := dependency.GetNamespace()
			target := ""
			_, isTypeScriptModule := moduleDependencies[identityOf(dependency)]
			if file.GetProgrammingLanguage() == "TypeScript" &&
				(isTypeScriptModule || isTypeScriptRelativeSpecifier(namespace)) {
				target = typeScriptModules.resolveRelative(file.Path, namespace)
			} else {
				target = classToFile.get(namespace)
				if target == "" {
					target = classToFile.get(dependency.GetClassName())
				}
			}
			if target == "" || target == file.Path {
				continue
			}

			if edges[file.Path] == nil {
				edges[file.Path] = make(map[string]struct{})
			}
			edges[file.Path][target] = struct{}{}
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

	return graph
}
