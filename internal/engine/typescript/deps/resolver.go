package deps

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/engine/dependency"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// FileDependencyResolver owns TypeScript module resolution. The project-level
// analyzer only consumes its language-neutral Resolver contract.
type FileDependencyResolver struct{}

var _ dependency.Resolver = (*FileDependencyResolver)(nil)

func NewFileDependencyResolver() *FileDependencyResolver {
	return &FileDependencyResolver{}
}

func (r *FileDependencyResolver) ForFiles(files []*pb.File) dependency.ScopedResolver {
	index := newModuleIndex()
	moduleDependencies := make(map[*pb.File]map[dependencyIdentity]struct{})
	for _, file := range files {
		if file == nil || file.GetProgrammingLanguage() != "TypeScript" {
			continue
		}
		index.add(file.GetPath())
		moduleDependencies[file] = dependenciesAttachedToModules(file)
	}
	index.sort()
	return &scopedFileDependencyResolver{
		modules:            index,
		moduleDependencies: moduleDependencies,
	}
}

type scopedFileDependencyResolver struct {
	modules            *moduleIndex
	moduleDependencies map[*pb.File]map[dependencyIdentity]struct{}
}

var _ dependency.ScopedResolver = (*scopedFileDependencyResolver)(nil)

func (r *scopedFileDependencyResolver) Resolve(source *pb.File, dep *pb.StmtExternalDependency) ([]string, bool) {
	if source == nil || dep == nil || source.GetProgrammingLanguage() != "TypeScript" {
		return nil, false
	}

	specifier := dep.GetNamespace()
	_, isModuleDependency := r.moduleDependencies[source][identityOf(dep)]
	if !isModuleDependency && !isRelativeSpecifier(specifier) {
		return nil, false
	}

	// A bare package import and a missing relative import are both handled:
	// neither may fall through to symbol matching and bind to an unrelated
	// class with the same name.
	if target := r.modules.resolveRelative(source.GetPath(), specifier); target != "" {
		return []string{target}, true
	}
	return nil, true
}

type dependencyIdentity struct {
	namespace    string
	className    string
	functionName string
	from         string
}

func identityOf(dep *pb.StmtExternalDependency) dependencyIdentity {
	return dependencyIdentity{
		namespace:    dep.GetNamespace(),
		className:    dep.GetClassName(),
		functionName: dep.GetFunctionName(),
		from:         dep.GetFrom(),
	}
}

// The tree-sitter visitor attaches TypeScript imports and re-exports to their
// namespace. This identifies module dependencies without changing the shared
// protobuf model or confusing them with class references synthesized later.
func dependenciesAttachedToModules(file *pb.File) map[dependencyIdentity]struct{} {
	dependencies := make(map[dependencyIdentity]struct{})
	if file == nil || file.Stmts == nil {
		return dependencies
	}
	for _, namespace := range file.Stmts.GetStmtNamespace() {
		if namespace == nil || namespace.Stmts == nil {
			continue
		}
		for _, dep := range namespace.Stmts.GetStmtExternalDependencies() {
			if dep != nil {
				dependencies[identityOf(dep)] = struct{}{}
			}
		}
	}
	return dependencies
}

// Longer suffixes come first so declarations lose ".d.ts", not just ".ts".
var resolveExtensions = []string{
	".d.mts", ".d.cts", ".d.ts",
	".mts", ".cts", ".tsx", ".ts",
	".jsx", ".js", ".mjs", ".cjs", ".vue",
}

var extensionPriorities = map[string]int{
	".ts": 0, ".tsx": 1, ".mts": 2, ".cts": 3,
	".d.ts": 4, ".d.mts": 5, ".d.cts": 6,
	".js": 7, ".jsx": 8, ".mjs": 9, ".cjs": 10, ".vue": 11,
}

func resolveExtension(path string) string {
	for _, extension := range resolveExtensions {
		if strings.HasSuffix(path, extension) {
			return extension
		}
	}
	return ""
}

func stripResolveExtension(path string) string {
	return strings.TrimSuffix(path, resolveExtension(path))
}

type moduleCandidate struct {
	path      string
	extension string
	priority  int
	indexFile bool
}

// moduleIndex keeps every candidate instead of silently overwriting
// collisions such as foo.ts/foo.tsx. Sorting makes resolution independent of
// the parser's concurrent file order.
type moduleIndex struct {
	exact      map[string][]string
	candidates map[string][]moduleCandidate
}

func newModuleIndex() *moduleIndex {
	return &moduleIndex{
		exact:      make(map[string][]string),
		candidates: make(map[string][]moduleCandidate),
	}
}

func (index *moduleIndex) add(path string) {
	cleanPath := filepath.Clean(path)
	extension := resolveExtension(cleanPath)
	if extension == "" {
		return
	}
	if !containsPath(index.exact[cleanPath], path) {
		index.exact[cleanPath] = append(index.exact[cleanPath], path)
	}

	modulePath := stripResolveExtension(cleanPath)
	index.addCandidate(modulePath, moduleCandidate{
		path:      path,
		extension: extension,
		priority:  extensionPriority(extension),
	})
	if filepath.Base(modulePath) == "index" {
		index.addCandidate(filepath.Dir(modulePath), moduleCandidate{
			path:      path,
			extension: extension,
			priority:  extensionPriority(extension),
			indexFile: true,
		})
	}
}

func (index *moduleIndex) addCandidate(modulePath string, candidate moduleCandidate) {
	for _, existing := range index.candidates[modulePath] {
		if existing.path == candidate.path {
			return
		}
	}
	index.candidates[modulePath] = append(index.candidates[modulePath], candidate)
}

func containsPath(paths []string, path string) bool {
	for _, existing := range paths {
		if existing == path {
			return true
		}
	}
	return false
}

func (index *moduleIndex) sort() {
	for cleanPath := range index.exact {
		sort.Strings(index.exact[cleanPath])
	}
	for modulePath, candidates := range index.candidates {
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

func extensionPriority(extension string) int {
	if priority, found := extensionPriorities[extension]; found {
		return priority
	}
	return len(resolveExtensions)
}

func isRelativeSpecifier(specifier string) bool {
	return specifier == "." || specifier == ".." ||
		strings.HasPrefix(specifier, "./") || strings.HasPrefix(specifier, "../")
}

func (index *moduleIndex) resolveRelative(fromPath, specifier string) string {
	if !isRelativeSpecifier(specifier) {
		return ""
	}
	joined := filepath.Clean(filepath.Join(filepath.Dir(fromPath), specifier))
	requestedExtension := resolveExtension(joined)
	if requestedExtension != "" {
		if exact := index.exact[joined]; len(exact) > 0 {
			return exact[0]
		}
	}
	for _, candidate := range index.candidates[stripResolveExtension(joined)] {
		if requestedExtension == "" || isExtensionSubstitution(requestedExtension, candidate.extension) {
			return candidate.path
		}
	}
	return ""
}

// TypeScript substitutes source extensions when emitted JavaScript paths are
// imported. Other explicit extensions (notably .vue) must match exactly.
func isExtensionSubstitution(requested, candidate string) bool {
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
