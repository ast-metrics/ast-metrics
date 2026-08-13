package cpp

import (
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCppControlFlowIncludesAndHalstead(t *testing.T) {
	file := parseCpp(t, `
#include <vector>
#include "relay.hpp"
int classify(const std::vector<int>& values, bool ready) {
    int total = 0;
    for (int value : values) {
        if (ready && value > 0 || value == 7) total += value;
        else if (!ready) total--;
    }
    int i = 0;
    while (i++ < 2) {}
    do { total++; } while (total < 3);
    switch (total) { case 1: break; case 2: break; default: break; }
    try { return ready ? total : 0; } catch (...) { return -1; }
}
`)
	require.Len(t, file.Stmts.StmtFunction, 1)
	fn := file.Stmts.StmtFunction[0]
	assert.Len(t, fn.Stmts.StmtDecisionIf, 2)
	assert.Len(t, fn.Stmts.StmtLoop, 3)
	assert.Len(t, fn.Stmts.StmtDecisionSwitch, 1)
	assert.Len(t, fn.Stmts.StmtDecisionCase, 2)
	assert.Len(t, fn.Stmts.StmtDecisionCatch, 1)
	assert.Len(t, fn.Stmts.StmtDecisionTernary, 1)
	assert.Len(t, fn.Stmts.StmtDecisionLogical, 2)
	require.Len(t, file.Stmts.StmtExternalDependencies, 2)
	assert.Equal(t, "vector", file.Stmts.StmtExternalDependencies[0].Namespace)
	assert.Equal(t, "relay.hpp", file.Stmts.StmtExternalDependencies[1].Namespace)
	ops := operatorNames(fn.Operators)
	for _, expected := range []string{"+=", ">", "&&", "||", "=", "return"} {
		assert.Contains(t, ops, expected)
	}
	for _, expected := range []string{"values", "ready", "total", "value"} {
		assert.Contains(t, operandNames(fn.Operands), expected)
	}
}

func TestCppEmbeddedModernSyntaxFixture(t *testing.T) {
	file := parseCpp(t, `
#include <Arduino.h>

class RelayController {
public:
    explicit RelayController(uint8_t pin) : pin_(pin) {}

    void update(bool enabled) {
        if (enabled && !active_) {
            digitalWrite(pin_, HIGH);
            active_ = true;
        } else if (!enabled && active_) {
            digitalWrite(pin_, LOW);
            active_ = false;
        }
    }

private:
    uint8_t pin_;
    bool active_{false};
};
`)
	require.Len(t, file.Stmts.StmtClass, 1)
	class := file.Stmts.StmtClass[0]
	assert.Equal(t, []string{"pin_", "active_"}, operandNames(class.Operands))
	require.Len(t, class.Stmts.StmtFunction, 2)
	update := class.Stmts.StmtFunction[1]
	assert.Equal(t, "update", update.Name.Short)
	assert.Len(t, update.Stmts.StmtDecisionIf, 2)
	assert.Len(t, update.Stmts.StmtDecisionLogical, 2)
	assert.Contains(t, operandNames(update.Operands), "active_")
}

func TestCppDeclaratorsAndModernSyntax(t *testing.T) {
	file := parseCpp(t, `
auto trailing(int value) -> int { return value; }
template <typename T> T identity(T&& value) { return static_cast<T&&>(value); }
struct Callable { int operator()(int x) const { return x; } };
`)
	assert.Contains(t, functionNames(file.Stmts.StmtFunction), "trailing")
	assert.Contains(t, functionNames(file.Stmts.StmtFunction), "identity")
	require.Len(t, file.Stmts.StmtClass, 1)
	require.Len(t, file.Stmts.StmtClass[0].Stmts.StmtFunction, 1)
	assert.Equal(t, "operator()", file.Stmts.StmtClass[0].Stmts.StmtFunction[0].Name.Short)
}

func TestCppClassDependenciesFromTypesConstructionAndStaticUse(t *testing.T) {
	file := parseCpp(t, `
class Transport {};
class BaseController {};
class Logger { public: static Logger& instance(); };

class Controller : public BaseController {
public:
    Controller(Transport& transport) : transport_(transport) {}
    Transport* replacement() {
        auto next = new Transport();
        Logger::instance();
        return next;
    }
private:
    Transport& transport_;
};
`)
	require.Len(t, file.Stmts.StmtClass, 4)
	controller := file.Stmts.StmtClass[3]
	deps := controller.Stmts.StmtExternalDependencies
	depNames := make([]string, 0, len(deps))
	for _, dep := range deps {
		depNames = append(depNames, dep.ClassName)
		assert.Equal(t, controller.Name.Qualified, dep.From)
	}
	assert.ElementsMatch(t, []string{"BaseController", "Transport", "Logger"}, depNames)
}

func TestCppClassDependenciesDriveCouplingMetrics(t *testing.T) {
	file := parseCpp(t, `
class Motor {};
class Controller {
    Motor* motor_;
public:
    explicit Controller(Motor* motor) : motor_(motor) {}
};
`)
	analyzer.AnalyzeFile(file)
	project := analyzer.NewAggregator([]*pb.File{file}, nil).Aggregates()
	controller := file.Stmts.StmtClass[1]
	motor := file.Stmts.StmtClass[0]
	require.NotNil(t, controller.Stmts.Analyze.Coupling)
	require.NotNil(t, motor.Stmts.Analyze.Coupling)
	assert.Equal(t, int32(1), controller.Stmts.Analyze.Coupling.Efferent)
	assert.GreaterOrEqual(t, motor.Stmts.Analyze.Coupling.Afferent, int32(1))
	assert.Greater(t, project.ByClass.EfferentCoupling.Sum, float64(0))
}
