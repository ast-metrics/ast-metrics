// Package deps resolves the dependencies of a PHP file to the files of the
// analysis scope. The engine records every reference as a fully qualified
// class name, the use clauses already applied: resolving it is looking the
// name up among the classes and interfaces the scope declares.
package deps

import (
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/dependency"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

const Language = "PHP"

type FileDependencyResolver struct{}

var _ dependency.Resolver = (*FileDependencyResolver)(nil)

func NewFileDependencyResolver() *FileDependencyResolver {
	return &FileDependencyResolver{}
}

func (r *FileDependencyResolver) ForFiles(files []*pb.File) dependency.ScopedResolver {
	types := dependency.NewIndex()
	for _, file := range files {
		path := file.GetPath()
		if file == nil || path == "" || file.GetProgrammingLanguage() != Language {
			continue
		}
		for _, class := range engine.GetClassesInFile(file) {
			if name := class.GetName(); name != nil {
				types.Add(dependency.QualifiedOrShort(name), path)
			}
		}
		for _, itf := range engine.GetInterfacesInFile(file) {
			if name := itf.GetName(); name != nil {
				types.Add(dependency.QualifiedOrShort(name), path)
			}
		}
	}
	return &scopedFileDependencyResolver{types: types}
}

type scopedFileDependencyResolver struct {
	types *dependency.Index
}

var _ dependency.ScopedResolver = (*scopedFileDependencyResolver)(nil)
var _ dependency.StandardLibraryTeller = (*scopedFileDependencyResolver)(nil)

// Resolve claims every dependency of a PHP file: a name the scope does not
// declare belongs to a package, to an extension or to the language, and
// matching it by its short name would bind the file to whichever class
// happens to carry that name.
func (r *scopedFileDependencyResolver) Resolve(source *pb.File, dep *pb.StmtExternalDependency) ([]string, bool) {
	if source == nil || dep == nil || source.GetProgrammingLanguage() != Language {
		return nil, false
	}
	name := dep.GetNamespace()
	if name == "" {
		name = dep.GetClassName()
	}
	name = strings.TrimPrefix(name, `\`)
	if name == "" {
		return nil, true
	}
	return r.types.Get(name), true
}

// IsStandardLibrary tells the classes of the language and of its extensions
// from the ones of a package: they live in the global namespace, where a
// package, by the autoloading conventions, never declares anything.
func (r *scopedFileDependencyResolver) IsStandardLibrary(name string) bool {
	return !strings.Contains(strings.TrimPrefix(name, `\`), `\`)
}
