package rust

import (
	"path/filepath"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	Treesitter "github.com/ast-metrics/ast-metrics/internal/engine/treesitter"
	sitter "github.com/smacker/go-tree-sitter"
	tsRust "github.com/smacker/go-tree-sitter/rust"
)

type TreeSitterAdapter struct {
	src  []byte
	root *sitter.Node
}

func NewTreeSitterAdapter(src []byte) *TreeSitterAdapter { return &TreeSitterAdapter{src: src} }
func (a *TreeSitterAdapter) SetSource(src []byte)        { a.src = src }
func (a *TreeSitterAdapter) SetRootNode(root *sitter.Node) {
	a.root = root
}

func (a *TreeSitterAdapter) Language() *sitter.Language { return tsRust.GetLanguage() }

func (a *TreeSitterAdapter) NodeName(n *sitter.Node) string {
	if a.src == nil || n == nil {
		return ""
	}
	// functions and methods: field "name"
	if name := n.ChildByFieldName("name"); name != nil {
		return a.text(name)
	}
	// impl blocks and type items: take identifier
	if id := firstChildOfType(n, "type_identifier"); id != nil {
		return a.text(id)
	}
	if id := firstChildOfType(n, "identifier"); id != nil {
		return a.text(id)
	}
	// qualified path fallback: scoped_identifier segments
	if q := firstChildOfType(n, "scoped_identifier"); q != nil {
		txt := a.text(q)
		if i := strings.LastIndex(txt, "::"); i >= 0 && i+2 < len(txt) {
			return txt[i+2:]
		}
		return txt
	}
	return ""
}

func (a *TreeSitterAdapter) NodeBody(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	// function_item / method_item → body (block)
	if b := n.ChildByFieldName("body"); b != nil {
		return b
	}
	// impl_item block body
	if b := firstChildOfType(n, "declaration_list"); b != nil {
		return b
	}
	// trait_item body
	if b := firstChildOfType(n, "trait_item"); b != nil {
		return b
	}
	// generic block fallback
	if b := firstChildOfType(n, "block"); b != nil {
		return b
	}
	return nil
}

func (a *TreeSitterAdapter) NodeParams(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	// Rust grammar uses "parameters" under function_item and method_item
	if p := n.ChildByFieldName("parameters"); p != nil {
		return p
	}
	return firstChildOfType(n, "parameters")
}

func (a *TreeSitterAdapter) EachParamIdent(params *sitter.Node, yield func(string)) {
	if params == nil || a.src == nil {
		return
	}
	var walk func(*sitter.Node)
	walk = func(x *sitter.Node) {
		if x == nil {
			return
		}
		typ := x.Type()
		// identifiers inside parameter patterns
		if typ == "identifier" || typ == "type_identifier" || typ == "shorthand_field_identifier" {
			yield(a.text(x))
		}
		// self parameter
		if typ == "self" || typ == "self_parameter" {
			yield("self")
			return
		}
		// pattern_identifier covers simple `x: T`
		if typ == "pattern_identifier" {
			yield(a.text(x))
		}
		for i := 0; i < int(x.ChildCount()); i++ {
			walk(x.Child(i))
		}
	}
	walk(params)
}

func (a *TreeSitterAdapter) IsModule(n *sitter.Node) bool {
	return n.Type() == "source_file"
}

func (a *TreeSitterAdapter) IsClass(n *sitter.Node) bool {
	// Rust has no classes; treat struct, enum, union and trait as class-like
	// containers. An `impl` block is not one of them: it declares no type, it
	// holds the methods of a type declared elsewhere, so its methods are bound
	// to that type by ReceiverTypeName below.
	switch n.Type() {
	case "struct_item", "enum_item", "union_item", "trait_item":
		return true
	}
	return false
}

// ReceiverTypeName returns the type a method belongs to when it is declared in
// an `impl` block rather than inside the type itself.
//
// Rust always declares methods this way, so without it a struct would hold no
// method at all: every class-level metric would describe its field list and
// nothing else.
func (a *TreeSitterAdapter) ReceiverTypeName(n *sitter.Node) string {
	if n == nil || !a.IsFunction(n) {
		return ""
	}
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Type() != "impl_item" {
			continue
		}
		// `impl Trait for Type` carries both; the methods belong to Type
		if t := p.ChildByFieldName("type"); t != nil {
			return lastPathSegment(a.text(t))
		}
		return ""
	}
	return ""
}

// lastPathSegment keeps the type name out of a qualified path (`a::b::T` -> `T`)
// and drops any generic arguments, so that `impl Vec<T>` binds to `Vec`.
func lastPathSegment(name string) string {
	if i := strings.Index(name, "<"); i > 0 {
		name = name[:i]
	}
	if i := strings.LastIndex(name, "::"); i >= 0 {
		name = name[i+2:]
	}
	return strings.TrimSpace(name)
}

func (a *TreeSitterAdapter) IsFunction(n *sitter.Node) bool {
	switch n.Type() {
	case "function_item", "function_signature_item", "method_item":
		return true
	}
	return false
}

func (a *TreeSitterAdapter) ModuleNameFromPath(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

func (a *TreeSitterAdapter) AttachQualified(parent string, fn string) string {
	if parent == "" {
		return fn
	}
	return parent + "::" + fn
}

func (a *TreeSitterAdapter) EachChildBody(body *sitter.Node, yield func(*sitter.Node)) {
	if body == nil {
		return
	}
	switch body.Type() {
	case "block", "declaration_list":
		for i := 0; i < int(body.ChildCount()); i++ {
			yield(body.Child(i))
		}
	case "match_expression":
		// enumerate match arms
		for i := 0; i < int(body.ChildCount()); i++ {
			n := body.Child(i)
			if n.Type() == "match_block" || n.Type() == "match_body" {
				for j := 0; j < int(n.ChildCount()); j++ {
					arm := n.Child(j)
					if arm.Type() == "match_arm" {
						yield(arm)
					}
				}
			}
		}
	default:
		for i := 0; i < int(body.ChildCount()); i++ {
			yield(body.Child(i))
		}
	}
}

// rustDecisions maps the Rust grammar onto the shared complexity model.
//
// Rust has no ternary and no exception handler. `if let` and `while let` are
// the conditional forms of `if` and `while` and count as such. An `else if` is
// an else_clause holding an if_expression: the else costs nothing and the
// nested if is counted on its own. The `_ => ...` arm is demoted to a Default
// by Decision below.
//
// The `?` operator is counted as an if, because that is what it is: `foo()?`
// returns from the function when foo fails, so it opens the same branch as the
// `if err != nil { return err }` it replaces in Go. Leaving it out would make
// idiomatic Rust look simpler than the very same code written elsewhere. It is
// a try_expression, a node distinct from the `?Sized` of a trait bound, so a
// relaxed bound is never mistaken for a branch.
var rustDecisions = &Treesitter.DecisionSpec{
	If:      []string{"if_expression", "if_let_expression", "try_expression"},
	Else:    []string{"else_clause"},
	Loop:    []string{"for_expression", "while_expression", "while_let_expression", "loop_expression"},
	Switch:  []string{"match_expression"},
	Case:    []string{"match_arm"},
	Logical: []string{"binary_expression"},
	Ops:     []string{"&&", "||"},
}

func (a *TreeSitterAdapter) Decision(n *sitter.Node) Treesitter.DecisionKind {
	kind := rustDecisions.Classify(n, a.src)
	if kind == Treesitter.DecCase && isWildcardArm(n, a.src) {
		return Treesitter.DecDefault
	}
	return kind
}

func (a *TreeSitterAdapter) LogicalOperator(n *sitter.Node) string {
	return rustDecisions.LogicalOperator(n, a.src)
}

// isWildcardArm reports whether a match arm is the catch-all one (`_ => ...`).
func isWildcardArm(n *sitter.Node, src []byte) bool {
	pattern := firstChildOfType(n, "match_pattern")
	if pattern == nil || int(pattern.EndByte()) > len(src) {
		return false
	}
	return strings.TrimSpace(string(src[pattern.StartByte():pattern.EndByte()])) == "_"
}

func (a *TreeSitterAdapter) Imports(n *sitter.Node) []Treesitter.ImportItem {
	if n == nil {
		return nil
	}
	if n.Type() != "use_declaration" {
		return nil
	}
	return a.parseUse(n)
}

func (a *TreeSitterAdapter) parseUse(n *sitter.Node) []Treesitter.ImportItem {
	items := []Treesitter.ImportItem{}
	add := func(full string) {
		full = strings.TrimSpace(full)
		if full == "" {
			return
		}
		// cut alias if present: "foo::bar as Baz"
		full = strings.Split(full, " as ")[0]
		mod, name := splitModuleLeaf(full, "::")
		items = append(items, Treesitter.ImportItem{Module: mod, Name: name})
	}
	var walk func(*sitter.Node, string)
	walk = func(x *sitter.Node, prefix string) {
		if x == nil {
			return
		}
		switch x.Type() {
		case "use_tree":
			// may contain scoped_identifier, use_list, use_as_clause
		case "scoped_identifier", "identifier", "crate", "super", "self":
			path := a.text(x)
			if prefix != "" {
				path = strings.TrimSuffix(prefix, "::") + "::" + strings.TrimPrefix(path, "::")
			}
			add(path)
			return
		case "use_list":
			// grouped: foo::{bar, baz as Qux}
			// prefix is set by preceding scoped_identifier
		case "use_as_clause":
			// "path as Alias" → child 0 is path, we ignore alias for identity
			if x.ChildCount() > 0 {
				path := a.text(x.Child(0))
				if prefix != "" {
					path = strings.TrimSuffix(prefix, "::") + "::" + strings.TrimPrefix(path, "::")
				}
				add(path)
				return
			}
		}
		// derive group prefix if parent has a scoped_identifier
		if x.Type() == "use_list" || x.Type() == "use_tree" {
			p := prefix
			if q := firstChildOfType(x, "scoped_identifier"); q != nil {
				p = a.text(q)
			}
			for i := 0; i < int(x.ChildCount()); i++ {
				walk(x.Child(i), p)
			}
			return
		}
		for i := 0; i < int(x.ChildCount()); i++ {
			walk(x.Child(i), prefix)
		}
	}
	walk(n, "")
	return dedup(items)
}

// helpers

func (a *TreeSitterAdapter) text(n *sitter.Node) string {
	if n == nil || a.src == nil {
		return ""
	}
	return string(a.src[n.StartByte():n.EndByte()])
}

func firstChildOfType(n *sitter.Node, t string) *sitter.Node {
	if n == nil {
		return nil
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		if ch.Type() == t {
			return ch
		}
	}
	return nil
}

func splitModuleLeaf(full string, sep string) (string, string) {
	full = strings.Trim(full, sep)
	if i := strings.LastIndex(full, sep); i >= 0 {
		return full[:i], full[i+len(sep):]
	}
	return full, ""
}

func dedup(in []Treesitter.ImportItem) []Treesitter.ImportItem {
	if len(in) <= 1 {
		return in
	}
	seen := map[string]struct{}{}
	out := in[:0]
	for _, it := range in {
		key := it.Module + " " + it.Name
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, it)
	}
	return out
}

// rustStatements maps the Rust grammar onto the shared logical-lines model.
//
// Rust is expression-oriented: `if`, `for`, `match` and the rest are
// expressions, and only some of them end up wrapped in an `expression_statement`.
// They are therefore listed directly. Listing them also covers the `let x = if
// ... ` form for free, since both nodes start on the same line and a line is
// counted once.
//
// `match_arm` is absent: an arm is the label of a branch, like a `case` in the
// six other languages, and what it holds counts on its own lines.
//
// `const_item` and `static_item` are local declarations: at file scope they
// declare a member of the module, inside a function they are instructions.
var rustStatements = &Treesitter.StatementSpec{
	Statement: []string{
		"expression_statement", "let_declaration",
		"if_expression",
		"for_expression", "while_expression", "loop_expression",
		"match_expression",
		"return_expression", "break_expression", "continue_expression",
		"yield_expression", "try_expression", "unsafe_block",
	},
	LocalDeclaration: []string{"const_item", "static_item", "type_item"},
}

// Statement classifies a node against the shared logical-lines model, adding
// the one construct no node type can express: the tail expression of a block.
//
// The last expression of a block is the value that block yields, which is how
// Rust spells a `return`, and it can be any expression node at all. Without it,
// `fn f() -> i32 { self.x }` would hold no logical line while the same function
// written anywhere else holds one.
func (a *TreeSitterAdapter) Statement(n *sitter.Node) Treesitter.StatementKind {
	if kind := rustStatements.Classify(n); kind != Treesitter.NotAStatement {
		return kind
	}
	if a.isTailExpression(n) {
		return Treesitter.IsStatement
	}
	return Treesitter.NotAStatement
}

// isTailExpression reports whether n is the value a block yields.
func (a *TreeSitterAdapter) isTailExpression(n *sitter.Node) bool {
	if n == nil || !n.IsNamed() {
		return false
	}
	switch n.Type() {
	case "attribute_item", "inner_attribute_item", "line_comment", "block_comment", "block":
		return false
	}
	parent := n.Parent()
	if parent == nil || parent.Type() != "block" {
		return false
	}
	return lastNamedChild(parent) == n.StartByte()
}

// lastNamedChild returns the start byte of the last named child of n, or a
// value no node can have when it has none.
func lastNamedChild(n *sitter.Node) uint32 {
	last := ^uint32(0)
	for i := 0; i < int(n.ChildCount()); i++ {
		if ch := n.Child(i); ch.IsNamed() {
			last = ch.StartByte()
		}
	}
	return last
}

// CommentSyntax declares Rust comment tokens: "//" (which also opens the "///"
// and "//!" doc forms) and "/* */". "#" introduces an attribute
// (#[derive(...)]), which is code, not a comment. A single quote also opens a
// lifetime ('a), which has no closing quote to pair it with, so it must not be
// read as a string delimiter.
func (a *TreeSitterAdapter) CommentSyntax() engine.CommentSyntax {
	return engine.CommentSyntax{
		Line:          []string{"//"},
		BlockOpen:     "/*",
		BlockClose:    "*/",
		Quote:         []rune{'"'},
		LifetimeQuote: true,
	}
}

// rustOperatorTokens lists the anonymous token types counted as Halstead
// operators: arithmetic, comparison, logical, bitwise, ranges, error
// propagation, field access, paths, casts, the argument separator, the
// subscript and the keywords that drive the control flow. Keywords count as
// operators: without them, a body made of plain statements
// ("return self.items.clone()") would hold none at all, and its Halstead
// volume would collapse to zero.
var rustOperatorTokens = map[string]bool{
	"+": true, "-": true, "*": true, "/": true, "%": true,
	"==": true, "!=": true, "<": true, ">": true, "<=": true, ">=": true,
	"&&": true, "||": true, "!": true,
	"&": true, "|": true, "^": true, "<<": true, ">>": true,
	"=": true, "+=": true, "-=": true, "*=": true, "/=": true, "%=": true,
	"&=": true, "|=": true, "^=": true, "<<=": true, ">>=": true,
	"..": true, "..=": true, "?": true, ".": true, "::": true, "->": true, "=>": true,
	"as": true, ",": true, "[": true,
	"return": true, "if": true, "else": true, "match": true, "for": true,
	"in": true, "while": true, "loop": true, "break": true, "continue": true,
	"await": true, "yield": true, "move": true, "ref": true,
}

// rustOperandTypes lists the named node types counted as Halstead operands:
// identifiers and literals. String content is counted rather than the whole
// string node, so quotes are ignored.
var rustOperandTypes = map[string]bool{
	"identifier": true, "field_identifier": true, "shorthand_field_identifier": true,
	"self": true, "integer_literal": true, "float_literal": true, "char_literal": true,
	"string_content": true, "boolean_literal": true,
}

// rustCallTypes lists the node types counted as one call operator. A macro
// invocation already reports its "!" and is not counted twice.
var rustCallTypes = map[string]bool{"call_expression": true}

// rustPruneTypes lists the node types never walked: a type is not an operand,
// and the "<" and ">" of "Vec<String>" are not comparisons.
var rustPruneTypes = map[string]bool{
	"type_identifier": true, "primitive_type": true, "generic_type": true,
	"type_arguments": true, "type_parameters": true,
	"scoped_type_identifier": true, "reference_type": true,
	"pointer_type": true, "array_type": true, "tuple_type": true,
	"dynamic_type": true, "function_type": true, "abstract_type": true,
	"bounded_type": true, "removed_trait_bound": true, "lifetime": true,
	"where_clause": true, "unit_type": true,
}

// rustOperandSpec reads a field access as two operands joined by the "."
// operator ("self.items" gives "self" and "items"): the cohesion metrics rely
// on the bare field name, since "self" is reported as an operand of its own.
var rustOperandSpec = Treesitter.OperandSpec{
	OperatorTokens: rustOperatorTokens,
	OperandTypes:   rustOperandTypes,
	CallTypes:      rustCallTypes,
	PruneTypes:     rustPruneTypes,
}

// ExtractOperatorsOperands collects Halstead operators and operands from the
// AST within the given 1-based inclusive line range.
func (a *TreeSitterAdapter) ExtractOperatorsOperands(src []byte, startLine, endLine int) ([]string, []string) {
	root := a.root
	source := a.src
	if root == nil {
		if source == nil {
			source = src
		}
		if source == nil {
			return nil, nil
		}
		parser := sitter.NewParser()
		parser.SetLanguage(a.Language())
		tree := parser.Parse(nil, source)
		root = tree.RootNode()
	}
	return rustOperandSpec.Extract(root, source, startLine, endLine)
}
