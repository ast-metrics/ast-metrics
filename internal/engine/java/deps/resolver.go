package deps

import (
	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/dependency"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// Language is the value the Java engine writes in pb.File.ProgrammingLanguage.
const Language = "Java"

// FileDependencyResolver owns Java import resolution.
//
// A Java import names a type by its fully qualified name, so resolution is a
// lookup on that name and nothing else. Matching on the simple name instead,
// as the shared fallback does, cannot tell `com.demo.a.Item` from
// `com.demo.b.Item`: the very case the package system exists to disambiguate.
type FileDependencyResolver struct{}

var _ dependency.Resolver = (*FileDependencyResolver)(nil)

func NewFileDependencyResolver() *FileDependencyResolver {
	return &FileDependencyResolver{}
}

func (r *FileDependencyResolver) ForFiles(files []*pb.File) dependency.ScopedResolver {
	types := dependency.NewIndex()
	packages := dependency.NewIndex()

	for _, file := range files {
		path := file.GetPath()
		if file == nil || path == "" || file.GetProgrammingLanguage() != Language {
			continue
		}
		for _, namespace := range file.GetStmts().GetStmtNamespace() {
			if name := namespace.GetName(); name != nil {
				packages.Add(dependency.QualifiedOrShort(name), path)
			}
		}
		for _, class := range engine.GetClassesInFile(file) {
			if name := class.GetName(); name != nil {
				types.Add(dependency.QualifiedOrShort(name), path)
			}
		}
	}
	return &scopedFileDependencyResolver{types: types, packages: packages}
}

type scopedFileDependencyResolver struct {
	types    *dependency.Index
	packages *dependency.Index
}

var _ dependency.ScopedResolver = (*scopedFileDependencyResolver)(nil)

func (r *scopedFileDependencyResolver) Resolve(source *pb.File, dep *pb.StmtExternalDependency) ([]string, bool) {
	if source == nil || dep == nil || source.GetProgrammingLanguage() != Language {
		return nil, false
	}

	// Every dependency of a Java file is claimed, resolved or not: the JDK and
	// the third-party jars are legitimately outside the analyzed scope, and an
	// unresolved `java.util.List` must not fall back to matching a project
	// class that happens to be called List.
	pkg, simpleName := dep.GetNamespace(), dep.GetClassName()

	// `import a.b.*;` carries no type name: it depends on the whole package.
	if simpleName == "" {
		return r.packages.Get(pkg), true
	}

	if targets := r.types.Get(pkg + "." + simpleName); len(targets) > 0 {
		return targets, true
	}

	// The same lookup one level up covers the two forms that name something
	// inside a type rather than inside a package: a static import
	// (`import static a.b.C.m;` splits into "a.b.C" and "m") and a nested type
	// (`import a.b.Outer.Inner;`). Both are declared by the file that declares
	// the enclosing type.
	return r.types.Get(pkg), true
}
