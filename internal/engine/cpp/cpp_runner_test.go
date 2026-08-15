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
	require.Len(t, fn.Parameters, 2)
	assert.Equal(t, "left", fn.Parameters[0].Name)
	assert.Equal(t, "right", fn.Parameters[1].Name)
	assert.NotNil(t, fn.Location)
	assert.Equal(t, "C++", file.ProgrammingLanguage)
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
	require.Len(t, widget.Stmts.StmtFunction, 3)
	assert.Equal(t, []string{"Widget", "~Widget", "value"}, functionNames(widget.Stmts.StmtFunction))
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
	require.Len(t, file.Stmts.StmtClass[0].Stmts.StmtFunction, 1)
	assert.Equal(t, "update", file.Stmts.StmtClass[0].Stmts.StmtFunction[0].Name.Short)
	assert.Contains(t, functionNames(file.Stmts.StmtFunction), "utility")
}

func TestCppDiscoveryExtensionsDoNotClaimGenericHeader(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.cpp", "b.cc", "c.cxx", "d.hpp", "e.hh", "f.hxx", "generic.h"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("int f() { return 1; }"), 0o644))
	}
	cfg := configuration.NewConfiguration()
	require.NoError(t, cfg.SetSourcesToAnalyzePath([]string{dir}))
	runner := &CppRunner{Configuration: cfg}
	files := runner.getFileList().Files
	require.Len(t, files, 6)
	for _, path := range files {
		assert.NotEqual(t, ".h", filepath.Ext(path))
	}
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
