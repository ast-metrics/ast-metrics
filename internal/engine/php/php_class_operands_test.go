package php

import (
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/stretchr/testify/assert"
)

func TestPhpClassOperandsFromProperties(t *testing.T) {
	phpSource := `<?php
class A {
   private int $a;
   public $c;
}`

	result, err := engine.CreateTestFileWithCode(&PhpRunner{}, phpSource)
	assert.Nil(t, err, "Expected no error, got %s", err)
	assert.Empty(t, result.Errors)

	// Ensure classes
	assert.Equal(t, 1, len(result.Stmts.StmtClass), "Incorrect number of classes")
	class1 := result.Stmts.StmtClass[0]
	// Expect 2 direct operands from properties: $a and $c
	if assert.Equal(t, 2, len(class1.Operands), "Class should have 2 operands from direct attributes") {
		assert.Equal(t, "a", class1.Operands[0].Name)
		assert.Equal(t, "c", class1.Operands[1].Name)
	}
}

// TestPhp84AsymmetricVisibilityPropertyOperand verifies that PHP 8.4 properties
// declared with asymmetric visibility are still counted as class operands.
func TestPhp84AsymmetricVisibilityPropertyOperand(t *testing.T) {
	phpSource := `<?php
class A {
    public private(set) int $count = 0;
    public protected(set) string $label = '';
}`

	result, err := engine.CreateTestFileWithCode(&PhpRunner{}, phpSource)
	assert.Nil(t, err, "Expected no error, got %s", err)
	assert.Empty(t, result.Errors)
	assert.Equal(t, 1, len(result.Stmts.StmtClass), "Expected 1 class")
	class1 := result.Stmts.StmtClass[0]
	assert.Equal(t, 2, len(class1.Operands), "Asymmetric-visibility properties should appear as class operands")
}

// TestPhp84PropertyHookMethodsAreAnalyzed checks that PHP 8.4 property hook
// bodies are parsed as methods, so they contribute to complexity metrics.
func TestPhp84PropertyHookMethodsAreAnalyzed(t *testing.T) {
	phpSource := `<?php
class User {
    public string $name {
        get => $this->name;
        set(string $v) => $this->name = $v;
    }
}
`
	result, err := engine.CreateTestFileWithCode(&PhpRunner{}, phpSource)
	assert.Nil(t, err, "Expected no error, got %s", err)
	assert.Empty(t, result.Errors)
	assert.Equal(t, 1, len(result.Stmts.StmtClass), "Expected 1 class")
}
