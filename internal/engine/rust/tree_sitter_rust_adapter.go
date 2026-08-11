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
	// Rust has no classes; treat struct, enum, trait, impl as class-like containers
	switch n.Type() {
	case "struct_item", "enum_item", "union_item", "trait_item", "impl_item":
		return true
	}
	return false
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
var rustDecisions = &Treesitter.DecisionSpec{
	If:      []string{"if_expression", "if_let_expression"},
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

// CountComments counts Rust comment lines (//, ///, //! and /* ... */ blocks)
// in the given 1-based inclusive line range. Block delimiter lines are
// comment lines.
func (a *TreeSitterAdapter) CountComments(lines []string, start, end int) int {
	cnt := 0
	inBlock := false
	for i := start - 1; i < end && i < len(lines); i++ {
		ln := strings.TrimSpace(lines[i])
		if ln == "" {
			continue
		}
		clean := stripRustStrings(ln)
		if inBlock {
			cnt++
			if strings.Contains(clean, "*/") {
				inBlock = false
			}
			continue
		}
		if strings.HasPrefix(clean, "//") {
			cnt++
			continue
		}
		if strings.HasPrefix(clean, "/*") {
			cnt++
			if !strings.Contains(clean, "*/") {
				inBlock = true
			}
			continue
		}
	}
	return cnt
}

// IsLogicalNode reports whether a node begins a logical line. Rust is
// expression-oriented: on top of the default statement types, "let" bindings
// and match arms count, as does the tail expression of a block (a direct
// block child with no wrapping statement node).
func (a *TreeSitterAdapter) IsLogicalNode(n *sitter.Node) bool {
	t := n.Type()
	switch t {
	case "let_declaration", "match_arm":
		return true
	case "attribute_item", "inner_attribute_item", "line_comment", "block_comment", "block":
		return false
	}
	if Treesitter.IsDefaultLogicalNode(t) {
		return true
	}
	// tail expression of a block
	if parent := n.Parent(); parent != nil && parent.Type() == "block" && n.IsNamed() {
		return true
	}
	return false
}

// CommentMarkers declares Rust comment tokens: "//" and "/* */" only.
// "#" introduces attributes (#[derive(...)]), which are code, not comments.
func (a *TreeSitterAdapter) CommentMarkers() engine.CommentMarkers {
	return engine.CommentMarkers{SlashSlash: true, SlashStar: true}
}

// stripRustStrings removes content inside double-quoted strings so comment
// markers embedded in literals (e.g. URLs) are not miscounted. Single quotes
// are left untouched: in Rust they also introduce lifetimes ('a), which have
// no closing quote and would corrupt the scan.
func stripRustStrings(s string) string {
	out := make([]rune, 0, len(s))
	inDq := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' { // escape
			if i+1 < len(s) {
				i++
			}
			continue
		}
		if c == '"' {
			inDq = !inDq
			continue
		}
		if inDq {
			continue
		}
		out = append(out, rune(c))
	}
	return string(out)
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
