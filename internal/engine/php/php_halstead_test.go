package php

import (
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/stretchr/testify/assert"
)

// The PHP operator extraction used to be a line scanner driven by a list of
// symbols. It knew nothing of the keywords, of the calls nor of the argument
// separators, so "return array_keys($this->items);" reported zero operators:
// the Halstead volume of the method collapsed to zero, and with it every metric
// derived from it.
func TestPhpOperatorsOfAPlainReturn(t *testing.T) {
	phpSource := `<?php
class Cart {
	private array $items = [];

	public function keys(): array {
		return array_keys($this->items);
	}
}
`
	result, err := engine.CreateTestFileWithCode(&PhpRunner{}, phpSource)
	assert.Nil(t, err, "Expected no error, got %s", err)

	method := result.Stmts.StmtClass[0].Stmts.StmtFunction[0]

	operators := []string{}
	for _, op := range method.Operators {
		operators = append(operators, op.Name)
	}
	// the ":" of the return type, the "return", the call and the "->" of the
	// attribute it reads
	assert.Equal(t, []string{":", "return", "()", "->"}, operators)

	operands := []string{}
	for _, operand := range method.Operands {
		operands = append(operands, operand.Name)
	}
	// the attribute access is a single operand, the form LCOM4 expects
	assert.Equal(t, []string{"this.items"}, operands)
}

func TestPhpAccessorsAreNotPerfectlyMaintainable(t *testing.T) {
	phpSource := `<?php
class Cart {
	private array $items = [];
	private int $total = 0;

	public function keys(): array {
		return array_keys($this->items);
	}

	public function total(): int {
		return $this->total;
	}
}
`
	result, err := engine.CreateTestFileWithCode(&PhpRunner{}, phpSource)
	assert.Nil(t, err, "Expected no error, got %s", err)
	analyzer.AnalyzeFile(result)

	class := result.Stmts.StmtClass[0]
	for _, method := range class.Stmts.StmtFunction {
		volume := method.Stmts.Analyze.Volume
		if assert.NotNil(t, volume.HalsteadVolume, "no volume for %s", method.Name.Short) {
			assert.Greater(t, *volume.HalsteadVolume, float64(0),
				"a method holding a call and a return has a volume: %s", method.Name.Short)
		}
	}

	// 171 is the ceiling of the maintainability index: a class whose volume is
	// zero reaches it, whatever it does
	assert.NotNil(t, class.Stmts.Analyze.Maintainability.MaintainabilityIndex)
	assert.Less(t, *class.Stmts.Analyze.Maintainability.MaintainabilityIndex, float64(171))
}

// TestPhpNamedArgumentsAndPipesSurviveTheAstWalk covers two operators the
// grammar buries inside a node of its own: the ":" of a named argument, and the
// "|>" of PHP 8.5.
func TestPhpNamedArgumentsAndPipesSurviveTheAstWalk(t *testing.T) {
	phpSource := `<?php
function test() {
	$object = new MyClass(userId: 1, userName: "John");
	$result = 'Hello'
		|> strtoupper(...)
		|> trim(...);
	return $result;
}
`
	adapter := NewTreeSitterAdapter([]byte(phpSource))
	ops, _ := adapter.ExtractOperatorsOperands([]byte(phpSource), 1, 8)

	counts := map[string]int{}
	for _, op := range ops {
		counts[op]++
	}
	assert.Equal(t, 2, counts[":"], "expected the two named arguments to report a ':', got %v", ops)
	assert.Equal(t, 2, counts["|>"], "expected the two pipes to report a '|>', got %v", ops)
	assert.Equal(t, 0, counts["|"], "the halves of a pipe must not be reported on their own, got %v", ops)
	assert.Equal(t, 0, counts[">"], "the halves of a pipe must not be reported on their own, got %v", ops)
}
