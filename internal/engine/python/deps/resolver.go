package deps

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/engine/dependency"
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

	packages := newPackageCache()
	for _, file := range files {
		path := file.GetPath()
		if file == nil || path == "" || file.GetProgrammingLanguage() != Language {
			continue
		}
		module := packages.moduleOf(path)
		if module == "" {
			continue
		}
		moduleOfFile[path] = module
		modules.Add(module, path)
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

// rebase turns an import specifier into an absolute module path. A specifier
// with no leading dot is already absolute; each leading dot climbs one package
// from the one holding the importing file.
func (r *scopedFileDependencyResolver) rebase(sourcePath, specifier string) (string, bool) {
	level := len(specifier) - len(strings.TrimLeft(specifier, "."))
	if level == 0 {
		return specifier, true
	}

	module, located := r.moduleOfFile[sourcePath]
	if !located {
		return "", false
	}
	segments := strings.Split(module, ".")
	// The package holding the file is its module path minus the file itself,
	// except for an __init__.py, which is the package.
	if filepath.Base(sourcePath) != "__init__.py" {
		segments = segments[:len(segments)-1]
	}
	// One dot means the current package, so only the dots beyond the first
	// climb. A specifier that climbs past the top of the tree names something
	// outside the analyzed sources.
	if level-1 > len(segments) {
		return "", false
	}
	segments = segments[:len(segments)-(level-1)]

	base := strings.Join(segments, ".")
	relative := strings.TrimLeft(specifier, ".")
	if relative == "" {
		return base, true
	}
	if base == "" {
		return relative, true
	}
	return base + "." + relative, true
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

// packageCache maps a file to its dotted module path, remembering which
// directories are packages so the __init__.py chain is only walked once.
type packageCache struct {
	isPackage map[string]bool
}

func newPackageCache() *packageCache {
	return &packageCache{isPackage: make(map[string]bool)}
}

func (c *packageCache) moduleOf(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = filepath.Clean(path)
	}

	segments := []string{}
	// An __init__.py is the package holding it, not a module of its own:
	// `import pkg` loads pkg/__init__.py.
	if base := strings.TrimSuffix(filepath.Base(absolute), filepath.Ext(absolute)); base != "__init__" {
		segments = append(segments, base)
	}
	for current := filepath.Dir(absolute); c.packageAt(current); {
		segments = append([]string{filepath.Base(current)}, segments...)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return strings.Join(segments, ".")
}

func (c *packageCache) packageAt(directory string) bool {
	if known, cached := c.isPackage[directory]; cached {
		return known
	}
	_, err := os.Stat(filepath.Join(directory, "__init__.py"))
	isPackage := err == nil
	c.isPackage[directory] = isPackage
	return isPackage
}
