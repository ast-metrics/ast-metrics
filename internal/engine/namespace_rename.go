package engine

import pb "github.com/ast-metrics/ast-metrics/pb"

// RenameNamespace spells the namespace of a parsed file the way an import
// statement spells it, and returns whether anything was renamed.
//
// Several languages name a module by its bare file or package name while every
// file importing it names it by a path: "analyzer" against
// "example.com/demo/internal/analyzer" in Go, "entrypoint" against
// "company.product.artifact.entrypoint" in Python. Left as they are, the two
// ends of a dependency are not written in the same language: nothing links a
// module to the modules using it, and two directories that happen to hold a
// module of the same name are one and the same. The short name is kept: it is
// how the module reads in the report.
//
// The dependencies were named after the module while it was being read, so the
// ones coming from the module itself carry the bare name and are spelled again.
// The ones coming from a class or a function of the module keep the name of
// that scope.
func RenameNamespace(file *pb.File, qualified string) bool {
	if file == nil || file.Stmts == nil || len(file.Stmts.StmtNamespace) == 0 || qualified == "" {
		return false
	}
	namespace := file.Stmts.StmtNamespace[0]
	if namespace == nil || namespace.Name == nil {
		return false
	}
	previous := namespace.Name.Qualified
	if previous == qualified {
		return false
	}
	namespace.Name.Qualified = qualified
	for _, dependency := range DependenciesAtEveryScope(file) {
		if dependency != nil && dependency.From == previous {
			dependency.From = qualified
		}
	}
	return true
}

// DependenciesAtEveryScope lists the dependencies held by a file, at every
// scope they can be attached to. The visitor attaches one dependency to several
// scopes at once, so the same one can be listed more than once.
func DependenciesAtEveryScope(file *pb.File) []*pb.StmtExternalDependency {
	if file == nil || file.Stmts == nil {
		return nil
	}
	dependencies := file.Stmts.StmtExternalDependencies
	for _, namespace := range file.Stmts.StmtNamespace {
		if namespace != nil && namespace.Stmts != nil {
			dependencies = append(dependencies, namespace.Stmts.StmtExternalDependencies...)
		}
	}
	for _, class := range GetClassesInFile(file) {
		if class != nil && class.Stmts != nil {
			dependencies = append(dependencies, class.Stmts.StmtExternalDependencies...)
		}
	}
	for _, function := range GetFunctionsInFile(file) {
		if function != nil && function.Stmts != nil {
			dependencies = append(dependencies, function.Stmts.StmtExternalDependencies...)
		}
	}
	return dependencies
}
