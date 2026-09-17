package deps

import (
	"path/filepath"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/engine/dependency"
	"github.com/ast-metrics/ast-metrics/internal/engine/golang/module"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// Language is the value the Go engine writes in pb.File.ProgrammingLanguage.
const Language = "Golang"

// FileDependencyResolver owns Go import resolution.
//
// A Go import names a package, and a package is a directory. Its import path
// is the module path declared in go.mod followed by the path of the directory
// relative to that go.mod, which is the same rule the toolchain applies. The
// importing file therefore depends on every analyzed file of that directory:
// they are compiled as one unit and the import statement names no symbol that
// would let us single one of them out.
type FileDependencyResolver struct{}

var _ dependency.Resolver = (*FileDependencyResolver)(nil)

func NewFileDependencyResolver() *FileDependencyResolver {
	return &FileDependencyResolver{}
}

func (r *FileDependencyResolver) ForFiles(files []*pb.File) dependency.ScopedResolver {
	packages := dependency.NewIndex()
	// Directory suffixes back up the go.mod lookup: a repository analyzed from
	// a subdirectory, or vendored sources, can put the module file out of
	// reach. A suffix is only trusted when it designates a single package.
	suffixes := dependency.NewIndex()

	modules := module.NewCache()
	moduleOfFile := make(map[string]string)
	for _, file := range files {
		path := file.GetPath()
		if file == nil || path == "" || file.GetProgrammingLanguage() != Language {
			continue
		}
		directory := filepath.Dir(path)
		if importPath := modules.ImportPathOf(directory); importPath != "" {
			packages.Add(importPath, path)
			moduleOfFile[path] = modules.ModulePathOf(directory)
		}
		for _, suffix := range directorySuffixes(directory) {
			suffixes.Add(suffix, path)
		}
	}
	return &scopedFileDependencyResolver{packages: packages, suffixes: suffixes, moduleOfFile: moduleOfFile}
}

type scopedFileDependencyResolver struct {
	packages *dependency.Index
	suffixes *dependency.Index
	// moduleOfFile is the go.mod module path each file sits under.
	moduleOfFile map[string]string
}

var _ dependency.ScopedResolver = (*scopedFileDependencyResolver)(nil)
var _ dependency.LibraryTeller = (*scopedFileDependencyResolver)(nil)

// IsStandardLibrary tells the standard library from a module of a host: the
// first segment of "fmt" or "net/http" holds no dot, the one of
// "github.com/x/y" or "gopkg.in/yaml.v3" does.
func (r *scopedFileDependencyResolver) IsStandardLibrary(importPath string) bool {
	first := importPath
	if i := strings.IndexByte(first, '/'); i >= 0 {
		first = first[:i]
	}
	return first != "" && !strings.Contains(first, ".")
}

var _ dependency.StandardLibraryTeller = (*scopedFileDependencyResolver)(nil)

// IsLibrary tells a package of another module from a package of the same
// module that the analysis was not given: "example.com/demo/internal/x" is
// the project itself when the importing file sits under the go.mod of
// example.com/demo, whether or not internal/x was analyzed.
func (r *scopedFileDependencyResolver) IsLibrary(source *pb.File, importPath string) bool {
	modulePath := r.moduleOfFile[source.GetPath()]
	if modulePath == "" {
		return true
	}
	return importPath != modulePath && !strings.HasPrefix(importPath, modulePath+"/")
}

func (r *scopedFileDependencyResolver) Resolve(source *pb.File, dep *pb.StmtExternalDependency) ([]string, bool) {
	if source == nil || dep == nil || source.GetProgrammingLanguage() != Language {
		return nil, false
	}

	// Every dependency of a Go file is claimed, resolved or not. The standard
	// library and third-party modules are legitimately absent from the scope,
	// and letting "fmt" or "errors" fall through to name matching would bind
	// the file to whichever type happens to carry that name.
	importPath := dep.GetNamespace()
	if importPath == "" {
		return nil, true
	}
	if targets := r.packages.Get(importPath); len(targets) > 0 {
		return targets, true
	}

	// Longest first, so "example.com/demo/internal/model" prefers a package
	// sitting in ".../demo/internal/model" over one in ".../model". The last
	// segment alone is never tried: matching "model" against every directory
	// of that name would invent edges, and a one-segment import path is a
	// standard library package, which is out of scope by definition.
	for _, suffix := range pathSuffixes(importPath) {
		if targets := r.suffixes.GetUnambiguous(suffix); len(targets) > 0 {
			return targets, true
		}
	}
	return nil, true
}

// pathSuffixes lists the trailing fragments of a slash-separated path, longest
// first, down to two segments.
func pathSuffixes(path string) []string {
	segments := strings.Split(strings.Trim(filepath.ToSlash(path), "/"), "/")
	suffixes := make([]string, 0, len(segments))
	for i := 0; i+1 < len(segments); i++ {
		suffixes = append(suffixes, strings.Join(segments[i:], "/"))
	}
	return suffixes
}

// directorySuffixes lists the trailing fragments of a directory path, so a
// package can be found by the tail of its import path.
func directorySuffixes(directory string) []string {
	return pathSuffixes(filepath.Clean(directory))
}
