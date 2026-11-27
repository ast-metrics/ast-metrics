package php

import (
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A syntax the grammar cannot read costs more than the construct itself: the
// error node swallows the declarations around it, and the file loses metrics it
// should have. These cover the syntax added since PHP 8.4, one case per
// language feature, and check both that the file parses and that what it
// declares is still found.
func TestPhpSyntaxUpToPhp86IsUnderstood(t *testing.T) {
	cases := map[string]struct {
		code      string
		classes   []string
		functions []string
	}{
		"property hooks (8.4)": {
			code: `<?php
class User {
    public string $name {
        get => ucfirst($this->raw);
        set (string $value) { $this->raw = trim($value); }
    }
    private string $raw = '';
}`,
			classes: []string{"User"},
		},
		"asymmetric visibility (8.4)": {
			code: `<?php
class Order {
    public private(set) string $reference = '';
    public function __construct(public protected(set) int $total) {}
}`,
			classes:   []string{"Order"},
			functions: []string{"Order::__construct"},
		},
		"new without parentheses (8.4)": {
			code: `<?php
function build(): string { return new Formatter()->format('x'); }`,
			functions: []string{"build"},
		},
		"pipe operator (8.5)": {
			code: `<?php
function slug(string $title): string {
    return $title |> trim(...) |> strtolower(...);
}`,
			functions: []string{"slug"},
		},
		"partial function application (8.6)": {
			code: `<?php
function supported(array $versions): array {
    $atLeast = version_compare(?, '8.6', '>=');
    return array_filter($versions, $atLeast);
}`,
			functions: []string{"supported"},
		},
		"readonly property defaults (8.6)": {
			code: `<?php
readonly class Config {
    public string $parser = 'tree-sitter';
    public function name(): string { return $this->parser; }
}`,
			classes:   []string{"Config"},
			functions: []string{"Config::name"},
		},
		"parameter doc comments (8.6)": {
			code: `<?php
function send(
    /** The address the message goes to. */ string $to,
    /** @var list<string> */ array $copies = [],
): bool { return true; }`,
			functions: []string{"send"},
		},
		"clone with arguments (8.6)": {
			code: `<?php
class Money {
    public function __construct(public readonly int $amount) {}
    public function withAmount(int $amount): static { return clone($this, ['amount' => $amount]); }
}`,
			classes:   []string{"Money"},
			functions: []string{"Money::__construct", "Money::withAmount"},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			file, err := engine.CreateTestFileWithCode(&PhpRunner{}, c.code)
			require.NoError(t, err)
			assert.Empty(t, file.Errors, "the file must parse")

			var classes, functions []string
			for _, class := range file.Stmts.StmtClass {
				classes = append(classes, class.Name.Qualified)
				for _, fn := range class.Stmts.StmtFunction {
					functions = append(functions, fn.Name.Qualified)
				}
			}
			for _, fn := range file.Stmts.StmtFunction {
				functions = append(functions, fn.Name.Qualified)
			}
			assert.Equal(t, c.classes, classes)
			assert.Equal(t, c.functions, functions)
		})
	}
}
