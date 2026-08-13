package rust

import (
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/engine"
)

func TestNewTreeSitterAdapter(t *testing.T) {
	src := []byte("fn main() {}")
	adapter := NewTreeSitterAdapter(src)

	if adapter == nil {
		t.Error("expected non-nil adapter")
	}
	if string(adapter.src) != "fn main() {}" {
		t.Errorf("expected source to be set, got %s", string(adapter.src))
	}
}

func TestTreeSitterAdapter_SetSource(t *testing.T) {
	adapter := &TreeSitterAdapter{}
	src := []byte("fn test() {}")

	adapter.SetSource(src)

	if string(adapter.src) != "fn test() {}" {
		t.Errorf("expected source 'fn test() {}', got %s", string(adapter.src))
	}
}

func TestTreeSitterAdapter_Language(t *testing.T) {
	adapter := &TreeSitterAdapter{}
	lang := adapter.Language()

	if lang == nil {
		t.Error("expected non-nil language")
	}
}

func TestTreeSitterAdapter_NodeName_NilNode(t *testing.T) {
	adapter := &TreeSitterAdapter{src: []byte("test")}
	name := adapter.NodeName(nil)

	if name != "" {
		t.Errorf("expected empty name for nil node, got %s", name)
	}
}

func TestTreeSitterAdapter_NodeName_NilSource(t *testing.T) {
	adapter := &TreeSitterAdapter{}
	// Can't create a real node without parsing, so test with nil
	name := adapter.NodeName(nil)

	if name != "" {
		t.Errorf("expected empty name for nil source, got %s", name)
	}
}

// countComments runs the shared line scanner with the comment syntax Rust
// declares, which is what the engine does when measuring a file.
func countComments(lines []string) int32 {
	adapter := NewTreeSitterAdapter(nil)
	return engine.CountLinesOfCode(lines, 1, len(lines), adapter.CommentSyntax()).CommentLinesOfCode
}

func TestTreeSitterAdapter_CountComments(t *testing.T) {
	lines := []string{
		"//! Module doc",
		"/// Item doc",
		"// plain comment",
		"/*",
		" block body",
		"*/",
		"fn add(a: i32, b: i32) -> i32 {",
		"    let url = \"http://example.com\"; // not stripped away wrongly",
		"    a + b",
		"}",
	}

	cnt := countComments(lines)
	// 3 line comments + 3 block lines (opener, body, closer); the trailing
	// "//" after code is not counted (full-line comments only)
	if cnt != 6 {
		t.Errorf("expected 6 comment lines, got %d", cnt)
	}

	// A "//" inside a string literal must not be counted
	inString := []string{"let s = \"//not-a-comment\";"}
	if got := countComments(inString); got != 0 {
		t.Errorf("expected 0 comment lines for string content, got %d", got)
	}

	// Lifetimes must not corrupt the scan
	lifetime := []string{"fn f<'a>(x: &'a str) -> &'a str {", "// comment", "}"}
	if got := countComments(lifetime); got != 1 {
		t.Errorf("expected 1 comment line with lifetimes present, got %d", got)
	}
}

func TestTreeSitterAdapter_ExtractOperatorsOperands(t *testing.T) {
	src := []byte(`fn add(a: i32, b: i32) -> i32 {
    let x = a + b;
    x * 2
}
`)
	adapter := NewTreeSitterAdapter(src)
	ops, operands := adapter.ExtractOperatorsOperands(src, 1, 4)

	// the "," of the parameter list, ->, =, + and *
	if len(ops) != 5 {
		t.Fatalf("expected 5 operators, got %d: %v", len(ops), ops)
	}
	// add, a, b (signature), x, a, b, x, 2
	if len(operands) != 8 {
		t.Fatalf("expected 8 operands, got %d: %v", len(operands), operands)
	}
}
