package cpp

import (
	"testing"

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
