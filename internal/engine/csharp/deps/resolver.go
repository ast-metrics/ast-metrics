package deps

import (
	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/dependency"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// Language is the value the C# engine writes in pb.File.ProgrammingLanguage.
const Language = "C#"

// FileDependencyResolver owns C# using resolution.
//
// C# is the one language of the set where a name says nothing about a file: a
// namespace is declared by as many files as its author likes, in any
// directory. A plain `using` therefore resolves to every analyzed file
// declaring that namespace, which is the granularity the directive itself
// carries. `using static` and alias forms name a type and resolve to the
// single file declaring it.
//
// This is why matching class names, as the shared fallback does, finds nothing
// at all in C#: a using directive names a namespace, and no class is called
// after one.
//
// The namespace granularity over-approximates, and knowingly so. A file using
// one type of a namespace of forty gets forty edges, where a Go import of a
// package genuinely depends on all of its files. Narrowing it means knowing
// which types the file names, which the C# AST does not record today: nothing
// in the parsed tree lists the identifiers a file references. Until it does,
// an edge too many is preferred to the graph C# had before, which was empty.
type FileDependencyResolver struct{}

var _ dependency.Resolver = (*FileDependencyResolver)(nil)

func NewFileDependencyResolver() *FileDependencyResolver {
	return &FileDependencyResolver{}
}

func (r *FileDependencyResolver) ForFiles(files []*pb.File) dependency.ScopedResolver {
	namespaces := dependency.NewIndex()
	types := dependency.NewIndex()

	for _, file := range files {
		path := file.GetPath()
		if file == nil || path == "" || file.GetProgrammingLanguage() != Language {
			continue
		}
		for _, namespace := range file.GetStmts().GetStmtNamespace() {
			if name := namespace.GetName(); name != nil {
				namespaces.Add(dependency.QualifiedOrShort(name), path)
			}
		}
		for _, class := range engine.GetClassesInFile(file) {
			if name := class.GetName(); name != nil {
				types.Add(dependency.QualifiedOrShort(name), path)
			}
		}
	}
	return &scopedFileDependencyResolver{namespaces: namespaces, types: types}
}

type scopedFileDependencyResolver struct {
	namespaces *dependency.Index
	types      *dependency.Index
}

var _ dependency.ScopedResolver = (*scopedFileDependencyResolver)(nil)

func (r *scopedFileDependencyResolver) Resolve(source *pb.File, dep *pb.StmtExternalDependency) ([]string, bool) {
	if source == nil || dep == nil || source.GetProgrammingLanguage() != Language {
		return nil, false
	}

	// Every dependency of a C# file is claimed, resolved or not: the BCL and
	// the NuGet packages sit outside the analyzed scope by design.
	target := dep.GetNamespace()
	if target == "" {
		return nil, true
	}
	// A type is looked up first: `using static Demo.Model.User` and
	// `using U = Demo.Model.User` both name one, and a namespace never shares
	// its name with a type in the same scope.
	if targets := r.types.Get(target); len(targets) > 0 {
		return targets, true
	}
	return r.namespaces.Get(target), true
}
