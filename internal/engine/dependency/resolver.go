// Package dependency defines the language-neutral contract used to resolve
// AST dependencies to files from an analysis scope.
package dependency

import pb "github.com/ast-metrics/ast-metrics/pb"

// Resolver creates an immutable resolver for one analysis scope. Building the
// scope once lets language engines index candidate files without sharing state
// between the different aggregates (project, language and directory).
type Resolver interface {
	ForFiles(files []*pb.File) ScopedResolver
}

// ScopedResolver resolves one AST dependency. Handled is true when the
// resolver owns that dependency, including when it deliberately leaves an
// external or missing target unresolved. Resolvers are consulted in order and
// the first one returning handled wins.
//
// Several targets are returned because an import does not always name a file.
// A Go import names a package, which is a directory; a Java wildcard import
// and a C# using name a namespace. The importing file depends on every file
// that makes up the named unit, and reporting only one of them would be an
// arbitrary choice among equals.
type ScopedResolver interface {
	Resolve(source *pb.File, dependency *pb.StmtExternalDependency) (targetPaths []string, handled bool)
}
