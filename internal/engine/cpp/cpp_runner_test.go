package cpp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseCpp(t *testing.T, src string) *pb.File {
	t.Helper()
	result, err := engine.CreateTestFileWithCode(&CppRunner{}, src)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}

func TestCppFreeFunction(t *testing.T) {
	file := parseCpp(t, `int add(int left, int right) { return left + right; }`)
	require.Len(t, file.Stmts.StmtFunction, 1)
	fn := file.Stmts.StmtFunction[0]
	assert.Equal(t, "add", fn.Name.Short)
	assert.Equal(t, "add", fn.Name.Qualified)
	require.Len(t, fn.Parameters, 2)
	assert.Equal(t, "left", fn.Parameters[0].Name)
	assert.Equal(t, "right", fn.Parameters[1].Name)
	assert.NotNil(t, fn.Location)
	assert.Equal(t, "C++", file.ProgrammingLanguage)
}

func TestCppFunctionLinesOfCode(t *testing.T) {
	file := parseCpp(t, `int sum(int left, int right) {
    // a comment line
    int total = left + right;
    return total;
}
`)
	require.Len(t, file.Stmts.StmtFunction, 1)
	fn := file.Stmts.StmtFunction[0]
	assert.Equal(t, int32(5), fn.LinesOfCode.LinesOfCode)
	assert.Equal(t, int32(2), fn.LinesOfCode.LogicalLinesOfCode)
	assert.Equal(t, int32(1), fn.LinesOfCode.CommentLinesOfCode)
	assert.Equal(t, int32(4), fn.LinesOfCode.NonCommentLinesOfCode)
}

func TestCppClassesStructsAndSpecialMembers(t *testing.T) {
	file := parseCpp(t, `
class Widget {
public:
    explicit Widget(int value) : value_(value) {}
    ~Widget() {}
    int value() const { return value_; }
private:
    int value_;
};
struct Point { int x; int y; int sum() { return x + y; } };
`)
	require.Len(t, file.Stmts.StmtClass, 2)
	widget := file.Stmts.StmtClass[0]
	assert.Equal(t, "Widget", widget.Name.Short)
	assert.Equal(t, "Widget", widget.Name.Qualified)
	require.Len(t, widget.Stmts.StmtFunction, 3)
	assert.Equal(t, []string{"Widget", "~Widget", "value"}, functionNames(widget.Stmts.StmtFunction))
	assert.Equal(t, "Widget::Widget", widget.Stmts.StmtFunction[0].Name.Qualified)
	assert.Equal(t, []string{"value_"}, operandNames(widget.Operands))
	assert.Equal(t, "Point", file.Stmts.StmtClass[1].Name.Short)
}

func TestCppNamespacesAndQualifiedDefinition(t *testing.T) {
	file := parseCpp(t, `
namespace devices {
class Relay { public: void update(bool enabled); };
void Relay::update(bool enabled) { if (enabled) {} }
int utility(int value) { return value; }
}
`)
	require.Len(t, file.Stmts.StmtClass, 1)
	assert.Equal(t, "Relay", file.Stmts.StmtClass[0].Name.Short)
	assert.Equal(t, "devices::Relay", file.Stmts.StmtClass[0].Name.Qualified)
	require.Len(t, file.Stmts.StmtClass[0].Stmts.StmtFunction, 1)
	assert.Equal(t, "update", file.Stmts.StmtClass[0].Stmts.StmtFunction[0].Name.Short)
	assert.Equal(t, "devices::Relay::update", file.Stmts.StmtClass[0].Stmts.StmtFunction[0].Name.Qualified)
	// the out-of-class definition was bound to the class: it is not a free function anymore
	assert.Equal(t, []string{"utility"}, functionNames(file.Stmts.StmtFunction))
	assert.Equal(t, "devices::utility", file.Stmts.StmtFunction[0].Name.Qualified)
}

func TestCppNestedAndCxx17Namespaces(t *testing.T) {
	file := parseCpp(t, `
namespace outer { namespace inner { class Nested {}; } }
namespace a::b { class Flat {}; void free(); }
`)
	classes := engine.GetClassesInFile(file)
	require.Len(t, classes, 2)
	byShort := map[string]*pb.StmtClass{}
	for _, class := range classes {
		byShort[class.Name.Short] = class
	}
	assert.Equal(t, "outer::inner::Nested", byShort["Nested"].Name.Qualified)
	assert.Equal(t, "a::b::Flat", byShort["Flat"].Name.Qualified)
}

func TestCppOutOfClassDefinitionKeepsQualificationWithoutLocalClass(t *testing.T) {
	// The class lives in another translation unit (e.g. a .cpp defining the
	// methods of a class declared in a .hpp): the definition keeps the
	// qualification as written instead of collapsing to a bare method name.
	file := parseCpp(t, `
#include "engine.hpp"
void core::Engine::ignite() { run(); }
void core::startAll() { igniteAll(); }
`)
	require.Len(t, file.Stmts.StmtFunction, 2)
	assert.Equal(t, "ignite", file.Stmts.StmtFunction[0].Name.Short)
	assert.Equal(t, "core::Engine::ignite", file.Stmts.StmtFunction[0].Name.Qualified)
	assert.Equal(t, "startAll", file.Stmts.StmtFunction[1].Name.Short)
	assert.Equal(t, "core::startAll", file.Stmts.StmtFunction[1].Name.Qualified)
}

func TestCppForwardDeclarationsAndBodilessMembersAreNotClassesOrFunctions(t *testing.T) {
	file := parseCpp(t, `
class Engine;
struct Handle;
class Widget {
public:
    Widget() = default;
    ~Widget() = delete;
    virtual void hook() = 0;
    int counter_ = 0;
    bool flag_{false};
};
`)
	// only the definition counts: the forward declarations would shadow the
	// real classes with empty entries
	require.Len(t, file.Stmts.StmtClass, 1)
	assert.Equal(t, "Widget", file.Stmts.StmtClass[0].Name.Short)
	// defaulted/deleted/pure-virtual members have no body and are not functions,
	// and `int counter_ = 0;` (parsed as a function_definition by the grammar)
	// must stay a field: only the field with a braced initializer remains countable
	assert.Empty(t, functionNames(file.Stmts.StmtClass[0].Stmts.StmtFunction))
	assert.Equal(t, []string{"counter_", "flag_"}, operandNames(file.Stmts.StmtClass[0].Operands))
}

func TestCppMacroFalloutIsRejected(t *testing.T) {
	file := parseCpp(t, `
FMT_BEGIN_NAMESPACE
namespace fmt {
class Formatter { public: int value() { return 1; } };
}
FMT_END_NAMESPACE
`)
	// the unexpanded macro must not become a function named `namespace`
	// wrapping the whole region
	for _, fn := range file.Stmts.StmtFunction {
		assert.NotContains(t, []string{"namespace", "class", "struct", "template"}, fn.Name.Short)
		assert.NotEqual(t, "FMT_BEGIN_NAMESPACE", fn.Name.Short)
	}
	// the class inside the macro block is still analyzed
	classes := engine.GetClassesInFile(file)
	require.Len(t, classes, 1)
	assert.Equal(t, "Formatter", classes[0].Name.Short)
	assert.Equal(t, []string{"value"}, functionNames(classes[0].Stmts.StmtFunction))
}

func TestCppConversionOperatorHasAName(t *testing.T) {
	file := parseCpp(t, `
struct Token {
    explicit operator bool() { return true; }
};
`)
	require.Len(t, file.Stmts.StmtClass, 1)
	functions := file.Stmts.StmtClass[0].Stmts.StmtFunction
	require.Len(t, functions, 1)
	assert.Equal(t, "operator bool", functions[0].Name.Short)
	assert.Equal(t, "Token::operator bool", functions[0].Name.Qualified)
}

func TestCppGTestMacrosAreNamedTestFunctions(t *testing.T) {
	file := parseCpp(t, `
TEST(Parser, HandlesEmptyInput) { int x = 1; }
TEST_F(ParserFixture, HandlesComments) {}
`)
	require.Len(t, file.Stmts.StmtFunction, 2)
	assert.Equal(t, "Parser.HandlesEmptyInput", file.Stmts.StmtFunction[0].Name.Short)
	assert.Equal(t, "ParserFixture.HandlesComments", file.Stmts.StmtFunction[1].Name.Short)
	assert.True(t, file.GetIsTest(), "gtest macros mark the file as a test file")
}

func TestCppCatch2MacrosAreNamedTestFunctions(t *testing.T) {
	file := parseCpp(t, `
#include <catch2/catch_test_macros.hpp>
TEST_CASE("empty input is rejected") { int x = 1; }
SCENARIO("repository round-trip") {}
`)
	require.Len(t, file.Stmts.StmtFunction, 2)
	assert.Equal(t, "empty input is rejected", file.Stmts.StmtFunction[0].Name.Short)
	assert.Equal(t, "repository round-trip", file.Stmts.StmtFunction[1].Name.Short)
	assert.True(t, file.GetIsTest())
}

func TestCppVariadicParameters(t *testing.T) {
	file := parseCpp(t, `
template <typename... Args>
void compose(Args&&... args) { compose(args...); }
`)
	require.Len(t, file.Stmts.StmtFunction, 1)
	require.Len(t, file.Stmts.StmtFunction[0].Parameters, 1)
	assert.Equal(t, "args", file.Stmts.StmtFunction[0].Parameters[0].Name)
}

func TestCppClassMembersAndMethodDeclarations(t *testing.T) {
	file := parseCpp(t, `
class Session {
public:
    void open();
    void close() { open(); }
private:
    int a, b, *c;
    Transport& link_;
};
`)
	require.Len(t, file.Stmts.StmtClass, 1)
	session := file.Stmts.StmtClass[0]
	// the method declaration is a member function only once defined
	require.Len(t, session.Stmts.StmtFunction, 1)
	assert.Equal(t, "close", session.Stmts.StmtFunction[0].Name.Short)
	// every declarator of a multi-declaration field is an attribute, and
	// method declarations are not
	assert.Equal(t, []string{"a", "b", "c", "link_"}, operandNames(session.Operands))
}

func TestCppTestFileDetection(t *testing.T) {
	runner := CppRunner{}
	for _, name := range []string{
		"widget-test.cc", "widget.test.cpp", "widget_test.cxx", "widget_tests.cpp",
		"WidgetTest.cpp", "WidgetTests.cpp", "TestWidget.cpp", "test_widget.cc", "tests.cpp",
	} {
		assert.True(t, runner.isTestFile("/src/"+name, nil), "%s should be detected as a test file", name)
	}
	for _, name := range []string{"widget.cpp", "latest.cpp", "contest.cpp", "widget.testsuite.cpp"} {
		assert.False(t, runner.isTestFile("/src/"+name, []byte("int main() { return 0; }")), "%s should not be detected as a test file", name)
	}
	assert.True(t, runner.isTestFile("/project/tests/helper.cpp", nil), "files under tests/ are test files")
	assert.True(t, runner.isTestFile("/src/widget.cpp", []byte(`#include <gtest/gtest.h>
int main() { return 0; }`)), "gtest includes mark the file as a test file")
	assert.False(t, runner.isTestFile("/src/widget.cpp", []byte(`int latest() { return CONTEST(1); }`)), "TEST( must be a call of the identifier TEST")
}

func TestCppHeaderClaiming(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"a.cpp": "int f() { return 1; }",
		"b.cc":  "int f() { return 1; }",
		"c.cxx": "int f() { return 1; }",
		"d.hpp": "int f() { return 1; }",
		"e.hh":  "int f() { return 1; }",
		"f.hxx": "int f() { return 1; }",
		"plain.h": `
/* plain C header */
struct point { int x; int y; };
int add(int a, int b);
`,
		"cxx.h": `
#pragma once
namespace widgets { class Relay { public: void update(); }; }
template <typename T> T identity(T value);
`,
	}
	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644))
	}
	cfg := configuration.NewConfiguration()
	require.NoError(t, cfg.SetSourcesToAnalyzePath([]string{dir}))
	runner := &CppRunner{Configuration: cfg}
	found := runner.getFileList().Files
	require.Len(t, found, 7)
	for _, path := range found {
		assert.NotEqual(t, "plain.h", filepath.Base(path), "plain C headers are not claimed")
	}
}

func TestCppConfiguredExtensionsAreUsed(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "widget.inl"), []byte("int f() { return 1; }"), 0o644))
	cfg := configuration.NewConfiguration()
	cfg.Extensions = map[string][]string{"cpp": {".inl"}}
	require.NoError(t, cfg.SetSourcesToAnalyzePath([]string{dir}))
	runner := &CppRunner{Configuration: cfg}
	require.Len(t, runner.getFileList().Files, 1)
}

func functionNames(functions []*pb.StmtFunction) []string {
	result := make([]string, 0, len(functions))
	for _, fn := range functions {
		result = append(result, fn.Name.Short)
	}
	return result
}

func operandNames(operands []*pb.StmtOperand) []string {
	result := make([]string, 0, len(operands))
	for _, operand := range operands {
		result = append(result, operand.Name)
	}
	return result
}

func operatorNames(operators []*pb.StmtOperator) []string {
	result := make([]string, 0, len(operators))
	for _, operator := range operators {
		result = append(result, operator.Name)
	}
	return result
}
