package analyzer

import (
	csharpdeps "github.com/ast-metrics/ast-metrics/internal/engine/csharp/deps"
	golangdeps "github.com/ast-metrics/ast-metrics/internal/engine/golang/deps"
	javadeps "github.com/ast-metrics/ast-metrics/internal/engine/java/deps"
	pythondeps "github.com/ast-metrics/ast-metrics/internal/engine/python/deps"
	rustdeps "github.com/ast-metrics/ast-metrics/internal/engine/rust/deps"
	typescriptdeps "github.com/ast-metrics/ast-metrics/internal/engine/typescript/deps"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// FileDependencies resolves the imports of the given files against each other
// with the standard resolver of every supported language. It is the graph the
// aggregator builds, without the rest of the aggregation.
func FileDependencies(files []*pb.File) FileDependencyGraph {
	return resolveFileDependencies(files,
		typescriptdeps.NewFileDependencyResolver(),
		golangdeps.NewFileDependencyResolver(),
		javadeps.NewFileDependencyResolver(),
		csharpdeps.NewFileDependencyResolver(),
		rustdeps.NewFileDependencyResolver(),
		pythondeps.NewFileDependencyResolver(),
	)
}
