package deps

import (
	"path/filepath"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/engine/dependency"
	"github.com/ast-metrics/ast-metrics/internal/engine/python/module"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// Language is the value the Python engine writes in pb.File.ProgrammingLanguage.
const Language = "Python"

// FileDependencyResolver owns Python module resolution.
//
// A Python module path is its file path with the separators turned into dots,
// counted from the first directory that is not a package. A directory is a
// package when it holds an __init__.py, so walking that chain upwards is what
// tells `pkg.sub.store` apart from `sub.store`. Relative imports are resolved
// from the package holding the importing file, one level per leading dot.
type FileDependencyResolver struct{}

var _ dependency.Resolver = (*FileDependencyResolver)(nil)

func NewFileDependencyResolver() *FileDependencyResolver {
	return &FileDependencyResolver{}
}

func (r *FileDependencyResolver) ForFiles(files []*pb.File) dependency.ScopedResolver {
	modules := dependency.NewIndex()
	// Suffixes back up the package walk. A namespace package (PEP 420) has no
	// __init__.py, so the walk stops too early and the module comes out under
	// a shorter path than the one the imports use. A suffix is only trusted
	// when it designates a single module.
	suffixes := dependency.NewIndex()
	moduleOfFile := make(map[string]string)

	packages := module.NewCache()
	for _, file := range files {
		path := file.GetPath()
		if file == nil || path == "" || file.GetProgrammingLanguage() != Language {
			continue
		}
		modulePath := packages.ModuleOf(path)
		if modulePath == "" {
			continue
		}
		moduleOfFile[path] = modulePath
		modules.Add(modulePath, path)
		// Indexed from the file path rather than from the module path: when
		// the package walk stopped early, the module path is exactly what is
		// missing its head, and the directories above it are what supply it.
		for _, suffix := range dottedPathSuffixes(path) {
			suffixes.Add(suffix, path)
		}
	}
	return &scopedFileDependencyResolver{
		modules:      modules,
		suffixes:     suffixes,
		moduleOfFile: moduleOfFile,
	}
}

type scopedFileDependencyResolver struct {
	modules      *dependency.Index
	suffixes     *dependency.Index
	moduleOfFile map[string]string
}

var _ dependency.ScopedResolver = (*scopedFileDependencyResolver)(nil)

func (r *scopedFileDependencyResolver) Resolve(source *pb.File, dep *pb.StmtExternalDependency) ([]string, bool) {
	if source == nil || dep == nil || source.GetProgrammingLanguage() != Language {
		return nil, false
	}

	// Every dependency of a Python file is claimed, resolved or not: the
	// standard library and the installed packages are outside the analyzed
	// scope, and an unresolved `typing` must not fall back to matching a
	// project class by name.
	specifier := dep.GetNamespace()
	if specifier == "" {
		return nil, true
	}

	module, understood := r.rebase(source.GetPath(), specifier)
	if !understood {
		return nil, true
	}

	// `from pkg import name` cannot be told apart from `from pkg.mod import
	// name` by syntax alone: the name is a submodule in the first case and an
	// object of the module in the second. The submodule is tried first, since
	// it is the more specific reading.
	if name := dep.GetClassName(); name != "" {
		if targets := r.lookup(module + "." + name); len(targets) > 0 {
			return targets, true
		}
	}
	return r.lookup(module), true
}

func (r *scopedFileDependencyResolver) lookup(module string) []string {
	module = strings.Trim(module, ".")
	if module == "" {
		return nil
	}
	if targets := r.modules.Get(module); len(targets) > 0 {
		return targets
	}
	return r.suffixes.GetUnambiguous(module)
}

// rebase turns an import specifier into an absolute module path, from the
// module of the importing file.
func (r *scopedFileDependencyResolver) rebase(sourcePath, specifier string) (string, bool) {
	if !strings.HasPrefix(specifier, ".") {
		return specifier, true
	}
	modulePath, located := r.moduleOfFile[sourcePath]
	if !located {
		return "", false
	}
	return module.Rebase(modulePath, filepath.Base(sourcePath) == "__init__.py", specifier)
}

// dottedPathSuffixes reads a file path as a module path and lists its trailing
// fragments, down to two segments. The last segment alone is never offered:
// matching "store" against every file of that name would invent edges.
func dottedPathSuffixes(path string) []string {
	path = strings.TrimSuffix(filepath.ToSlash(filepath.Clean(path)), filepath.Ext(path))
	segments := make([]string, 0, 8)
	for _, segment := range strings.Split(path, "/") {
		if segment != "" && segment != "." {
			segments = append(segments, segment)
		}
	}
	// An __init__.py is the package holding it, not a module of its own.
	if len(segments) > 0 && segments[len(segments)-1] == "__init__" {
		segments = segments[:len(segments)-1]
	}

	suffixes := make([]string, 0, len(segments))
	for i := 0; i+1 < len(segments); i++ {
		suffixes = append(suffixes, strings.Join(segments[i:], "."))
	}
	return suffixes
}
