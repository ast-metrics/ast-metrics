package grammar_test

import (
	"fmt"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	tsPhp "github.com/ast-metrics/ast-metrics/internal/engine/php/grammar"
)

func testParse(t *testing.T, label, src string) bool {
	t.Helper()
	parser := sitter.NewParser()
	parser.SetLanguage(tsPhp.GetLanguage())
	b := []byte(src)
	tree := parser.Parse(nil, b)
	root := tree.RootNode()
	s := root.String()
	if len(s) > 400 { s = s[:400] }
	fmt.Printf("--- %s ---\nHas errors: %v\nTree: %s\n\n", label, root.HasError(), s)
	return !root.HasError()
}

func TestPhp84PropertyHooks(t *testing.T) {
	ok := testParse(t, "property hooks (PHP 8.4)", `<?php
class User {
    public string $name {
        get => strtoupper($this->name);
        set(string $v) => $this->name = $v;
    }
}`)
	if !ok {
		t.Error("expected no parse errors for PHP 8.4 property hooks")
	}
}

func TestPhp85PipeOperator(t *testing.T) {
	testParse(t, "pipe operator (PHP 8.5)", `<?php
$result = 'hello' |> strtoupper(...);`)
}

func TestPhp84AsymmetricVisibility(t *testing.T) {
	ok := testParse(t, "asymmetric visibility (PHP 8.4)", `<?php
class Foo {
    public private(set) string $bar = 'baz';
}`)
	if !ok {
		t.Error("expected no parse errors for PHP 8.4 asymmetric visibility")
	}
}
