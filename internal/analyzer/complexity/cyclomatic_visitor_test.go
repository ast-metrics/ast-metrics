package analyzer

import (
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/golang"
	"github.com/ast-metrics/ast-metrics/internal/engine/php"
	"github.com/stretchr/testify/assert"
)

func TestItCalculateCyclomaticComplexityForGoLang(t *testing.T) {

	fileContent := `
    package main

    import "fmt"

    func example() {
        if true {
            if true {
                fmt.Println("Hello")
            }
        } else if true {
            fmt.Println("Hello")
        } else {
            fmt.Println("Hello")
        }
    }
    `

	parser := &golang.GolangRunner{}
	pbFile, err := engine.CreateTestFileWithCode(parser, fileContent)
	assert.Nil(t, err)

	visitor := CyclomaticComplexityVisitor{}
	ccn := visitor.Calculate(pbFile.Stmts)
	// baseline 1 + outer if + nested if + else-if; the else is free.
	// gocyclo and lizard both report 4 on this function.
	assert.Equal(t, int32(4), ccn)
}

func TestItCalculateCyclomaticComplexityForPhp(t *testing.T) {

	visitor := CyclomaticComplexityVisitor{}

	fileContent := `
    <?php
    namespace App;
    
    function example() {
        if (true) {
            if (true) {
                echo "Hello";
            }
        } else if (true) {
            echo "Hello";
        } else {
            echo "Hello";
        }
    }
    `

	parser := &php.PhpRunner{}
	pbFile, err := engine.CreateTestFileWithCode(parser, fileContent)
	assert.Nil(t, err)

	ccn := visitor.Calculate(pbFile.Stmts)
	assert.Equal(t, int32(4), ccn)
}

func TestItCalculateCyclomaticComplexityForComplexPhp(t *testing.T) {

	fileContent := `
    <?php
    namespace App;

    class Foo {
        
        public function example() {
            if (true) {
                if (true) {
                    echo "Hello";
                }
            } else if (true) {
                echo "Hello";
            } else {
                echo "Hello";
            }
        }

        public function example2() {
            if (true) {
                echo 'ok';
            } else {
                echo 'ko';
            }
        }
    }
    `

	parser := &php.PhpRunner{}
	pbFile, err := engine.CreateTestFileWithCode(parser, fileContent)
	assert.Nil(t, err)

	visitor := CyclomaticComplexityVisitor{}
	ccn := visitor.Calculate(pbFile.Stmts)
	assert.Equal(t, int32(6), ccn)
}

func TestItCalculateCyclomaticComplexityForMethodItself(t *testing.T) {

	fileContent := `
    <?php

    function example() {
        $a = 123;
        return $a;
    }
    `

	parser := &php.PhpRunner{}
	pbFile, err := engine.CreateTestFileWithCode(parser, fileContent)
	assert.Nil(t, err)

	visitor := CyclomaticComplexityVisitor{}
	ccn := visitor.Calculate(pbFile.Stmts)
	assert.Equal(t, int32(1), ccn)
}

func TestItCalculateCyclomaticComplexityForAllDecisionPoints(t *testing.T) {

	fileContent := `
    <?php

    namespace Foo;

    class Foo
    {
        public function example()
        {
            $a = 123;
            if ($a > 1) {
                if ($a > 2) {
                    // ...
                } elseif ($a > 3) {
                    // ...
                } else {
                    // ...
                }
            } elseif ($a > 4) {
                while ($a > 5) {
                    // ...
                }
                do {
                    // ...
                } while ($a > 6);
            } else {
                switch ($a) {
                    case 1:
                        // ...
                        break;
                    case 2:
                        // ...
                        break;
                    default:
                        // ...
                }
            }

            foreach ($a as $b) {
                // ...
            }

            for ($i = 0; $i < 10; $i++) {
                // ...
            }
        }
    }
    `

	parser := &php.PhpRunner{}
	pbFile, err := engine.CreateTestFileWithCode(parser, fileContent)
	assert.Nil(t, err)

	visitor := CyclomaticComplexityVisitor{}
	ccn := visitor.Calculate(pbFile.Stmts)
	// baseline 1 + 2 if + 3 elseif + while + do..while + 2 case + foreach + for.
	// The switch, its default and the two else branches are free.
	// lizard and phploc both report 11 on this method.
	assert.Equal(t, int32(11), ccn)
}

func TestItCalculatesMethodBaselineComplexityInsideClass(t *testing.T) {

	fileContent := `
    <?php

    class Foo {
        public function a() {
            return 1;
        }

        public function b() {
            $x = 2;
            return $x;
        }
    }
    `

	parser := &php.PhpRunner{}
	pbFile, err := engine.CreateTestFileWithCode(parser, fileContent)
	assert.Nil(t, err)

	assert.Len(t, pbFile.Stmts.StmtClass, 1)
	classNode := pbFile.Stmts.StmtClass[0]
	assert.Len(t, classNode.Stmts.StmtFunction, 2)

	visitor := CyclomaticComplexityVisitor{}

	// A class with two branchless methods should have CCN=2 (1 per method).
	assert.Equal(t, int32(2), visitor.Calculate(classNode.Stmts))
	// Each branchless method should have baseline cyclomatic complexity of 1 once visited in function context.
	for _, fn := range classNode.Stmts.StmtFunction {
		visitor.Visit(fn.Stmts, classNode.Stmts)
		assert.NotNil(t, fn.Stmts.Analyze)
		assert.NotNil(t, fn.Stmts.Analyze.Complexity)
		assert.Equal(t, int32(1), fn.Stmts.Analyze.Complexity.GetCyclomatic())
	}
}

func TestItCalculatesSingleMethodClassComplexityAsOne(t *testing.T) {

	fileContent := `
    <?php

    class Foo {
        public function only() {
            return 1;
        }
    }
    `

	parser := &php.PhpRunner{}
	pbFile, err := engine.CreateTestFileWithCode(parser, fileContent)
	assert.Nil(t, err)

	assert.Len(t, pbFile.Stmts.StmtClass, 1)
	classNode := pbFile.Stmts.StmtClass[0]
	assert.Len(t, classNode.Stmts.StmtFunction, 1)

	visitor := CyclomaticComplexityVisitor{}

	// Class complexity is the sum of its methods' cyclomatic complexity.
	assert.Equal(t, int32(1), visitor.Calculate(classNode.Stmts))

	fn := classNode.Stmts.StmtFunction[0]
	visitor.Visit(fn.Stmts, classNode.Stmts)
	assert.NotNil(t, fn.Stmts.Analyze)
	assert.NotNil(t, fn.Stmts.Analyze.Complexity)
	assert.Equal(t, int32(1), fn.Stmts.Analyze.Complexity.GetCyclomatic())
}
