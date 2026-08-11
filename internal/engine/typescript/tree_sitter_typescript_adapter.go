package typescript

import (
	"path/filepath"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	Treesitter "github.com/ast-metrics/ast-metrics/internal/engine/treesitter"
	sitter "github.com/smacker/go-tree-sitter"
	tsTsx "github.com/smacker/go-tree-sitter/typescript/tsx"
)

type TreeSitterAdapter struct {
	src  []byte
	root *sitter.Node
}

func NewTreeSitterAdapter(src []byte) *TreeSitterAdapter   { return &TreeSitterAdapter{src: src} }
func (a *TreeSitterAdapter) SetSource(src []byte)          { a.src = src }
func (a *TreeSitterAdapter) SetRootNode(root *sitter.Node) { a.root = root }
func (a *TreeSitterAdapter) Language() *sitter.Language    { return tsTsx.GetLanguage() }

// ensureRoot returns the tree shared by the runner, parsing the source when
// the adapter is used on its own (tests).
func (a *TreeSitterAdapter) ensureRoot(src []byte) (*sitter.Node, []byte) {
	source := a.src
	if source == nil {
		source = src
	}
	if a.root != nil {
		return a.root, source
	}
	if source == nil {
		return nil, nil
	}
	parser := sitter.NewParser()
	parser.SetLanguage(a.Language())
	a.root = parser.Parse(nil, source).RootNode()
	return a.root, source
}

func (a *TreeSitterAdapter) IsModule(n *sitter.Node) bool { return n.Type() == "program" }

func (a *TreeSitterAdapter) IsClass(n *sitter.Node) bool {
	switch n.Type() {
	case "class_declaration", "abstract_class_declaration", "enum_declaration":
		return true
	}
	return false
}

func (a *TreeSitterAdapter) IsInterface(n *sitter.Node) bool {
	return n.Type() == "interface_declaration"
}

func (a *TreeSitterAdapter) IsFunction(n *sitter.Node) bool {
	switch n.Type() {
	case "function_declaration", "method_definition", "generator_function_declaration":
		return true
	case "arrow_function":
		return true
	}
	return false
}

func (a *TreeSitterAdapter) NodeName(n *sitter.Node) string {
	if a.src == nil || n == nil {
		return ""
	}

	// Arrow functions get their name from the parent variable_declarator
	if n.Type() == "arrow_function" || n.Type() == "function" {
		p := n.Parent()
		if p != nil && p.Type() == "variable_declarator" {
			if nm := p.ChildByFieldName("name"); nm != nil {
				return text(a.src, nm)
			}
		}
		// Arrow as class property: parent is public_field_definition or property_definition
		if p != nil && (p.Type() == "public_field_definition" || p.Type() == "property_definition") {
			if nm := p.ChildByFieldName("name"); nm != nil {
				return text(a.src, nm)
			}
			if id := firstChildOfType(p, "property_identifier"); id != nil {
				return text(a.src, id)
			}
		}
		return ""
	}

	// method_definition: name field
	if n.Type() == "method_definition" {
		if nm := n.ChildByFieldName("name"); nm != nil {
			return text(a.src, nm)
		}
		if id := firstChildOfType(n, "property_identifier"); id != nil {
			return text(a.src, id)
		}
		return ""
	}

	// class_declaration, abstract_class_declaration, enum_declaration,
	// function_declaration, generator_function_declaration, interface_declaration
	if nm := n.ChildByFieldName("name"); nm != nil {
		return text(a.src, nm)
	}
	if id := firstChildOfType(n, "identifier"); id != nil {
		return text(a.src, id)
	}
	if id := firstChildOfType(n, "type_identifier"); id != nil {
		return text(a.src, id)
	}
	return ""
}

func (a *TreeSitterAdapter) NodeBody(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	if body := n.ChildByFieldName("body"); body != nil {
		return body
	}
	if b := firstChildOfType(n, "statement_block"); b != nil {
		return b
	}
	if b := firstChildOfType(n, "class_body"); b != nil {
		return b
	}
	if b := firstChildOfType(n, "enum_body"); b != nil {
		return b
	}
	if b := firstChildOfType(n, "object_type"); b != nil {
		return b
	}
	return nil
}

func (a *TreeSitterAdapter) NodeParams(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	if p := n.ChildByFieldName("parameters"); p != nil {
		return p
	}
	if p := firstChildOfType(n, "formal_parameters"); p != nil {
		return p
	}
	return nil
}

func (a *TreeSitterAdapter) EachParamIdent(params *sitter.Node, yield func(string)) {
	if params == nil || a.src == nil {
		return
	}
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		// Skip type annotations to avoid counting type names as parameters
		if n.Type() == "type_annotation" || n.Type() == "type_identifier" {
			return
		}
		if n.Type() == "identifier" || n.Type() == "shorthand_property_identifier_pattern" {
			yield(text(a.src, n))
			return
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(params)
}

func (a *TreeSitterAdapter) ModuleNameFromPath(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return base
}

func (a *TreeSitterAdapter) AttachQualified(parentClass string, fn string) string {
	if parentClass == "" {
		return fn
	}
	return parentClass + "." + fn
}

func (a *TreeSitterAdapter) EachChildBody(body *sitter.Node, yield func(*sitter.Node)) {
	if body == nil {
		return
	}
	switch body.Type() {
	case "switch_body":
		for i := 0; i < int(body.ChildCount()); i++ {
			ch := body.Child(i)
			if ch.Type() == "switch_case" || ch.Type() == "switch_default" {
				yield(ch)
			}
		}
	default:
		for i := 0; i < int(body.ChildCount()); i++ {
			yield(body.Child(i))
		}
	}
}

// tsDecisions maps the TypeScript / JavaScript grammar onto the shared
// complexity model.
//
// `for_in_statement` covers both `for..in` and `for..of`. An `else if` is an
// else_clause holding an if_statement: the else costs nothing and the nested
// if is counted on its own. `??` is deliberately not a logical operator here,
// for the same reason as in PHP.
var tsDecisions = &Treesitter.DecisionSpec{
	If:      []string{"if_statement"},
	Else:    []string{"else_clause"},
	Loop:    []string{"for_statement", "for_in_statement", "while_statement", "do_statement"},
	Switch:  []string{"switch_statement"},
	Case:    []string{"switch_case"},
	Default: []string{"switch_default"},
	Catch:   []string{"catch_clause"},
	Ternary: []string{"ternary_expression"},
	Logical: []string{"binary_expression"},
	Ops:     []string{"&&", "||"},
}

func (a *TreeSitterAdapter) Decision(n *sitter.Node) Treesitter.DecisionKind {
	return tsDecisions.Classify(n, a.src)
}

func (a *TreeSitterAdapter) LogicalOperator(n *sitter.Node) string {
	return tsDecisions.LogicalOperator(n, a.src)
}

func (a *TreeSitterAdapter) Imports(n *sitter.Node) []Treesitter.ImportItem {
	if n == nil {
		return nil
	}
	if n.Type() != "import_statement" {
		return nil
	}
	items := []Treesitter.ImportItem{}

	// Find the source module (the string literal at the end)
	var module string
	if src := n.ChildByFieldName("source"); src != nil {
		module = stripQuotes(text(a.src, src))
	} else {
		// fallback: find a string child
		for i := 0; i < int(n.ChildCount()); i++ {
			ch := n.Child(i)
			if ch.Type() == "string" {
				module = stripQuotes(text(a.src, ch))
				break
			}
		}
	}
	if module == "" {
		return nil
	}

	// Walk import clause children
	var walkClause func(*sitter.Node)
	walkClause = func(cl *sitter.Node) {
		if cl == nil {
			return
		}
		switch cl.Type() {
		case "import_clause":
			for i := 0; i < int(cl.ChildCount()); i++ {
				walkClause(cl.Child(i))
			}
		case "identifier":
			// default import: import X from 'module'
			items = append(items, Treesitter.ImportItem{Module: module, Name: text(a.src, cl)})
		case "named_imports":
			for i := 0; i < int(cl.ChildCount()); i++ {
				spec := cl.Child(i)
				if spec.Type() == "import_specifier" {
					if nm := spec.ChildByFieldName("name"); nm != nil {
						items = append(items, Treesitter.ImportItem{Module: module, Name: text(a.src, nm)})
					} else if id := firstChildOfType(spec, "identifier"); id != nil {
						items = append(items, Treesitter.ImportItem{Module: module, Name: text(a.src, id)})
					}
				}
			}
		case "namespace_import":
			// import * as X from 'module'
			if id := firstChildOfType(cl, "identifier"); id != nil {
				items = append(items, Treesitter.ImportItem{Module: module, Name: text(a.src, id)})
			} else {
				items = append(items, Treesitter.ImportItem{Module: module})
			}
		}
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		walkClause(n.Child(i))
	}

	// If no symbols found, record as plain module import
	if len(items) == 0 {
		items = append(items, Treesitter.ImportItem{Module: module})
	}
	return items
}

// IsLogicalNode reports whether a node begins a logical line. In TypeScript,
// "const"/"let"/"var" declarations are statements but their node types do not
// carry the "_statement" suffix.
func (a *TreeSitterAdapter) IsLogicalNode(n *sitter.Node) bool {
	switch n.Type() {
	case "lexical_declaration", "variable_declaration":
		return true
	}
	return Treesitter.IsDefaultLogicalNode(n.Type())
}

// CountComments counts TypeScript comment lines (// and /* */ and /** */) in the given range.
// CommentMarkers declares TypeScript comment tokens: "//" and "/* */" only.
// "#" introduces private class fields, which are code, not comments.
func (a *TreeSitterAdapter) CommentMarkers() engine.CommentMarkers {
	return engine.CommentMarkers{SlashSlash: true, SlashStar: true}
}

func (a *TreeSitterAdapter) CountComments(lines []string, start, end int) int {
	cnt := 0
	inBlock := false
	for i := start - 1; i < end && i < len(lines); i++ {
		ln := strings.TrimSpace(lines[i])
		if ln == "" {
			continue
		}
		clean := stripTSStrings(ln)
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
		if strings.HasPrefix(clean, "/*") || strings.HasPrefix(clean, "/**") {
			cnt++
			if !strings.Contains(clean, "*/") {
				inBlock = true
			}
			continue
		}
	}
	return cnt
}

// tsOperatorTokens lists the anonymous token types counted as Halstead
// operators: arithmetic, comparison, logical, bitwise, assignments, arrows,
// spreads, the member access, the argument separator, the subscript, the
// ternary and the keywords that drive the control flow. Keywords count as
// operators: without them, a body made of plain statements
// ("return this.items") would hold none at all, and its Halstead volume would
// collapse to zero. The "<" and ">" of generics never reach this map: the AST
// parses them as type arguments, which are pruned.
var tsOperatorTokens = map[string]bool{
	"+": true, "-": true, "*": true, "/": true, "%": true, "**": true,
	"==": true, "===": true, "!=": true, "!==": true,
	"<": true, ">": true, "<=": true, ">=": true,
	"&&": true, "||": true, "??": true, "!": true,
	"&": true, "|": true, "^": true, "~": true,
	"<<": true, ">>": true, ">>>": true,
	"=": true, "+=": true, "-=": true, "*=": true, "/=": true, "%=": true, "**=": true,
	"&&=": true, "||=": true, "??=": true, "&=": true, "|=": true, "^=": true,
	"<<=": true, ">>=": true, ">>>=": true,
	"++": true, "--": true, "=>": true, "...": true,
	".": true, "?.": true, ",": true, "[": true, "?": true,
	"return": true, "if": true, "else": true, "for": true, "of": true,
	"in": true, "while": true, "do": true, "switch": true, "case": true,
	"default": true, "break": true, "continue": true,
	"throw": true, "try": true, "catch": true, "finally": true,
	"new": true, "typeof": true, "instanceof": true, "delete": true,
	"void": true, "await": true, "yield": true, "as": true, "satisfies": true,
}

// tsOperandTypes lists the named node types counted as Halstead operands.
// Literals are left out on purpose: the cohesion metrics read the operands,
// and two methods sharing the literal 0 are not cohesive.
var tsOperandTypes = map[string]bool{
	"identifier":                            true,
	"property_identifier":                   true,
	"private_property_identifier":           true,
	"shorthand_property_identifier":         true,
	"shorthand_property_identifier_pattern": true,
}

// tsPruneTypes lists the node types never walked: a type annotation is not an
// operand, and two methods typed "number" are not cohesive. The list names
// every node the grammar reserves to type positions, so that a type reached
// outside of an annotation ("raw as Currency") is dropped too.
var tsPruneTypes = map[string]bool{
	"type_annotation": true, "type_arguments": true, "type_parameters": true,
	"type_alias_declaration": true, "interface_declaration": true,
	"type_identifier": true, "predefined_type": true,
	"object_type": true, "union_type": true, "intersection_type": true,
	"generic_type": true, "literal_type": true, "array_type": true,
	"tuple_type": true, "function_type": true, "constructor_type": true,
	"type_predicate": true, "type_query": true, "lookup_type": true,
	"index_type_query": true, "conditional_type": true,
	"template_literal_type": true, "readonly_type": true,
	"opting_type_annotation": true, "omitting_type_annotation": true,
	"asserts": true,
}

var tsChainTypes = map[string]bool{"member_expression": true}

// tsCallTypes lists the node types counted as one call operator. A "new"
// expression is left out: it already reports its "new" keyword.
var tsCallTypes = map[string]bool{"call_expression": true}

var tsOperandSpec = Treesitter.OperandSpec{
	OperatorTokens: tsOperatorTokens,
	OperandTypes:   tsOperandTypes,
	CallTypes:      tsCallTypes,
	PruneTypes:     tsPruneTypes,
	ChainTypes:     tsChainTypes,
	// no Receiver: the current object is the keyword "this"
}

// ExtractOperatorsOperands collects Halstead operators and operands from the
// AST within the given 1-based inclusive line range. A member access chain is
// a single operand ("this.total", "console.log"), and an access through
// "this" reads the attribute whatever is done with it afterwards:
// "this.items" and "this.items.length" both read "this.items".
func (a *TreeSitterAdapter) ExtractOperatorsOperands(src []byte, startLine, endLine int) ([]string, []string) {
	root, source := a.ensureRoot(src)
	if root == nil {
		return nil, nil
	}
	return tsOperandSpec.Extract(root, source, startLine, endLine)
}

// ExtractMethodCalls returns the methods called on the current object
// ("this.foo()", "super.bar()"). A plain read ("this.foo") is not a call: it
// is an attribute access, reported as an operand by ExtractOperatorsOperands.
func (a *TreeSitterAdapter) ExtractMethodCalls(src []byte, startLine, endLine int) []string {
	root, source := a.ensureRoot(src)
	if root == nil {
		return nil
	}
	return tsOperandSpec.MethodCalls(root, source, startLine, endLine)
}

// ClassDirectOperands scans class body for property declarations and returns property names.
func (a *TreeSitterAdapter) ClassDirectOperands(n *sitter.Node) []string {
	if n == nil || a.src == nil {
		return nil
	}
	body := a.NodeBody(n)
	if body == nil {
		return nil
	}
	var props []string
	for i := 0; i < int(body.ChildCount()); i++ {
		ch := body.Child(i)
		switch ch.Type() {
		case "public_field_definition", "property_definition":
			if nm := ch.ChildByFieldName("name"); nm != nil {
				props = append(props, text(a.src, nm))
			} else if id := firstChildOfType(ch, "property_identifier"); id != nil {
				props = append(props, text(a.src, id))
			}
		}
	}
	return props
}

// --- helpers ---

func text(src []byte, n *sitter.Node) string {
	if n == nil || src == nil {
		return ""
	}
	return string(src[n.StartByte():n.EndByte()])
}

func firstChildOfType(n *sitter.Node, t string) *sitter.Node {
	if n == nil {
		return nil
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if c := n.Child(i); c.Type() == t {
			return c
		}
	}
	return nil
}

func stripQuotes(s string) string {
	if len(s) >= 2 {
		if (s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '`' && s[len(s)-1] == '`') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// stripTSStrings removes content inside quotes to avoid false positives in comment/operator scanning.
func stripTSStrings(s string) string {
	out := make([]rune, 0, len(s))
	inBack := false
	inDq := false
	inSq := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' {
			if i+1 < len(s) {
				i++
			}
			continue
		}
		if !inDq && !inSq && c == '`' {
			inBack = !inBack
			continue
		}
		if !inBack && !inSq && c == '"' {
			inDq = !inDq
			continue
		}
		if !inBack && !inDq && c == '\'' {
			inSq = !inSq
			continue
		}
		if inBack || inDq || inSq {
			continue
		}
		out = append(out, rune(c))
	}
	return string(out)
}
