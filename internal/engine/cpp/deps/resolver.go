package deps

import (
	"path/filepath"

	"github.com/ast-metrics/ast-metrics/internal/engine/dependency"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// Language is the value the C++ engine writes in pb.File.ProgrammingLanguage.
const Language = "C++"

// FileDependencyResolver resolves includes that name another analyzed file.
// It deliberately implements only the compiler-independent rule: a relative
// path is resolved from the including file's directory. Include search paths
// and compile_commands.json are outside the syntax-level engine's scope.
type FileDependencyResolver struct{}

var _ dependency.Resolver = (*FileDependencyResolver)(nil)

func NewFileDependencyResolver() *FileDependencyResolver { return &FileDependencyResolver{} }

func (r *FileDependencyResolver) ForFiles(files []*pb.File) dependency.ScopedResolver {
	paths := make(map[string]string)
	for _, file := range files {
		if file == nil || file.GetProgrammingLanguage() != Language || file.GetPath() == "" {
			continue
		}
		paths[filepath.Clean(file.GetPath())] = file.GetPath()
	}
	return &scopedFileDependencyResolver{paths: paths}
}

type scopedFileDependencyResolver struct{ paths map[string]string }

var _ dependency.ScopedResolver = (*scopedFileDependencyResolver)(nil)

func (r *scopedFileDependencyResolver) Resolve(source *pb.File, dep *pb.StmtExternalDependency) ([]string, bool) {
	if source == nil || dep == nil || source.GetProgrammingLanguage() != Language {
		return nil, false
	}
	// Class dependencies still belong to the shared qualified-name resolver.
	if dep.GetClassName() != "" {
		return nil, false
	}
	include := dep.GetNamespace()
	if include == "" || filepath.IsAbs(include) {
		return nil, true
	}
	target := filepath.Clean(filepath.Join(filepath.Dir(source.GetPath()), filepath.FromSlash(include)))
	if path := r.paths[target]; path != "" {
		return []string{path}, true
	}
	return nil, true
}
