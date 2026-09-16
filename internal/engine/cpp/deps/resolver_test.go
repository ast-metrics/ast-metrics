package deps

import (
	"testing"

	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/stretchr/testify/assert"
)

func TestRelativeIncludeResolvesToAnalyzedFile(t *testing.T) {
	resolver := NewFileDependencyResolver().ForFiles([]*pb.File{
		{Path: "/project/include/relay.hpp", ProgrammingLanguage: Language},
		{Path: "/project/src/controller.cpp", ProgrammingLanguage: Language},
	})
	targets, handled := resolver.Resolve(
		&pb.File{Path: "/project/src/controller.cpp", ProgrammingLanguage: Language},
		&pb.StmtExternalDependency{Namespace: "../include/relay.hpp"},
	)
	assert.True(t, handled)
	assert.Equal(t, []string{"/project/include/relay.hpp"}, targets)
}

func TestClassDependencyFallsThroughToSharedResolver(t *testing.T) {
	resolver := NewFileDependencyResolver().ForFiles(nil)
	_, handled := resolver.Resolve(
		&pb.File{Path: "/project/controller.cpp", ProgrammingLanguage: Language},
		&pb.StmtExternalDependency{Namespace: "devices::Relay", ClassName: "Relay"},
	)
	assert.False(t, handled)
}
