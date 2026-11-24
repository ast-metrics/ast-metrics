package deps

import (
	"strings"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/stretchr/testify/assert"
)

func cppFile(path string, classes ...string) *pb.File {
	file := &pb.File{Path: path, ProgrammingLanguage: Language, Stmts: engine.FactoryStmts()}
	for _, qualified := range classes {
		file.Stmts.StmtClass = append(file.Stmts.StmtClass, &pb.StmtClass{Name: &pb.Name{Qualified: qualified}, Stmts: engine.FactoryStmts()})
	}
	return file
}

func include(path string) *pb.StmtExternalDependency {
	return &pb.StmtExternalDependency{Namespace: path}
}

func TestRelativeIncludeResolvesToAnalyzedFile(t *testing.T) {
	resolver := NewFileDependencyResolver().ForFiles([]*pb.File{
		cppFile("/project/include/relay.hpp"),
		cppFile("/project/src/controller.cpp"),
	})
	targets, handled := resolver.Resolve(cppFile("/project/src/controller.cpp"), include("../include/relay.hpp"))
	assert.True(t, handled)
	assert.Equal(t, []string{"/project/include/relay.hpp"}, targets)
}

// `#include "fmt/format.h"` is written from an include root the build system
// knows and the engine does not: the file is found by its trailing segments,
// as long as a single file of the project ends that way.
func TestIncludeResolvesFromAnIncludeRoot(t *testing.T) {
	resolver := NewFileDependencyResolver().ForFiles([]*pb.File{
		cppFile("/project/include/fmt/format.h"),
		cppFile("/project/src/format.cc"),
		cppFile("/project/src/a/util.h"),
		cppFile("/project/src/b/util.h"),
	})
	source := cppFile("/project/src/format.cc")

	targets, handled := resolver.Resolve(source, include("fmt/format.h"))
	assert.True(t, handled)
	assert.Equal(t, []string{"/project/include/fmt/format.h"}, targets)

	targets, handled = resolver.Resolve(source, include("util.h"))
	assert.True(t, handled)
	assert.Empty(t, targets, "two files end with util.h: nothing to pick between")

	targets, handled = resolver.Resolve(source, include("vector"))
	assert.True(t, handled)
	assert.Empty(t, targets)
}

func TestTypeIsLookedUpFromTheScopeItIsUsedFrom(t *testing.T) {
	resolver := NewFileDependencyResolver().ForFiles([]*pb.File{
		cppFile("/project/clock.hpp", "app::Clock"),
		cppFile("/project/io/clock.hpp", "app::io::Clock"),
		cppFile("/project/socket.cpp", "app::net::Socket"),
	})
	source := cppFile("/project/socket.cpp")
	use := func(from, name string) []string {
		dep := &pb.StmtExternalDependency{ClassName: name, From: from}
		if i := strings.LastIndex(name, "::"); i >= 0 {
			dep.Namespace, dep.ClassName = name, name[i+2:]
		}
		targets, handled := resolver.Resolve(source, dep)
		assert.True(t, handled)
		return targets
	}
	assert.Equal(t, []string{"/project/clock.hpp"}, use("app::net::Socket", "Clock"))
	assert.Equal(t, []string{"/project/io/clock.hpp"}, use("app::net::Socket", "io::Clock"))
	assert.Equal(t, []string{"/project/io/clock.hpp"}, use("app::io::Port", "Clock"))
	assert.Empty(t, use("app::net::Socket", "Missing"))
}

func TestOnlyAnIncludeIsALibrary(t *testing.T) {
	source := cppFile("/project/logger.hpp", "acme::Logger")
	source.Stmts.StmtExternalDependencies = []*pb.StmtExternalDependency{
		include("spdlog/spdlog.h"),
		{Namespace: "spdlog::logger", ClassName: "logger", From: "acme::Logger"},
	}
	resolver := NewFileDependencyResolver().ForFiles([]*pb.File{source})
	teller := resolver.(interface {
		IsLibrary(*pb.File, string) bool
		IsStandardLibrary(string) bool
	})
	assert.True(t, teller.IsLibrary(source, "spdlog/spdlog.h"))
	assert.False(t, teller.IsLibrary(source, "spdlog::logger"))

	for _, header := range []string{"vector", "string_view", "cstdio", "stdio.h"} {
		assert.True(t, teller.IsStandardLibrary(header), header)
	}
	for _, header := range []string{"spdlog/spdlog.h", "Arduino.h", "boost/asio.hpp"} {
		assert.False(t, teller.IsStandardLibrary(header), header)
	}
}
