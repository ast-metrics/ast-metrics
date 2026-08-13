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
	switch n.Type() {
	case "import_statement":
		return a.importsFromImportStatement(n)
	case "export_statement":
		// A re-export (`export ... from 'module'`) is a dependency on that
		// module just like an import; a local export (no `from` clause) is not.
		return a.importsFromExportStatement(n)
	default:
		return nil
	}
}

// moduleOf reads the source module string of an import/export statement (the
// string literal in its "source" field, falling back to the first string
// child for grammars that don't expose the field).
func (a *TreeSitterAdapter) moduleOf(n *sitter.Node) string {
	if src := n.ChildByFieldName("source"); src != nil {
		return stripQuotes(text(a.src, src))
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if ch := n.Child(i); ch.Type() == "string" {
			return stripQuotes(text(a.src, ch))
		}
	}
	return ""
}

func (a *TreeSitterAdapter) importsFromImportStatement(n *sitter.Node) []Treesitter.ImportItem {
	module := a.moduleOf(n)
	if module == "" {
		return nil
	}
	items := []Treesitter.ImportItem{}

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

// importsFromExportStatement handles re-exports: `export * from 'module'`,
// `export * as ns from 'module'`, and `export { a, b as c } from 'module'`
// (optionally prefixed with `type`). These reference another module just like
// an import does, so barrel files (`export * from './components'`) are
// tracked as dependencies rather than silently dropped.
func (a *TreeSitterAdapter) importsFromExportStatement(n *sitter.Node) []Treesitter.ImportItem {
	if n.ChildByFieldName("source") == nil {
		// Local export (`export { d }`, `export const x = ...`): no module involved.
		return nil
	}
	module := a.moduleOf(n)
	if module == "" {
		return nil
	}
	items := []Treesitter.ImportItem{}
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		switch ch.Type() {
		case "export_clause":
			for j := 0; j < int(ch.ChildCount()); j++ {
				spec := ch.Child(j)
				if spec.Type() == "export_specifier" {
					if nm := spec.ChildByFieldName("name"); nm != nil {
						items = append(items, Treesitter.ImportItem{Module: module, Name: text(a.src, nm)})
					} else if id := firstChildOfType(spec, "identifier"); id != nil {
						items = append(items, Treesitter.ImportItem{Module: module, Name: text(a.src, id)})
					}
				}
			}
		case "namespace_export":
			// export * as ns from 'module'
			if id := firstChildOfType(ch, "identifier"); id != nil {
				items = append(items, Treesitter.ImportItem{Module: module, Name: text(a.src, id)})
			}
		}
	}

	// export * from 'module', or nothing more specific found: record as a plain module dependency.
	if len(items) == 0 {
		items = append(items, Treesitter.ImportItem{Module: module})
	}
	return items
}

// IsLogicalNode reports whether a node begins a logical line. In TypeScript,
// "const"/"let"/"var" declarations are statements but their node types do not
// carry the "_statement" suffix.
// tsStatements maps the TypeScript grammar onto the shared logical-lines model.
//
// `switch_case` and `switch_default` are labels; `class_declaration`,
// `interface_declaration`, `enum_declaration`, `type_alias_declaration` and
// `public_field_definition` declare members; `import_statement` and
// `export_statement` carry the "_statement" suffix but declare nothing that
// runs. An `else if` is spelled as an `else_clause` holding an `if_statement`,
// so the nested if is what counts.
var tsStatements = &Treesitter.StatementSpec{
	Statement: []string{
		"expression_statement",
		"lexical_declaration", "variable_declaration",
		"if_statement",
		"for_statement", "for_in_statement", "while_statement", "do_statement",
		"switch_statement", "try_statement",
		"return_statement", "break_statement", "continue_statement",
		"throw_statement", "labeled_statement", "with_statement", "debugger_statement",
	},
}

func (a *TreeSitterAdapter) Statement(n *sitter.Node) Treesitter.StatementKind {
	return tsStatements.Classify(n)
}

// CommentSyntax declares TypeScript comment tokens: "//" and "/* */" only.
// "#" introduces a private class field, which is code, not a comment.
// Backticks open a template literal, whose content must be ignored like any
// other string.
func (a *TreeSitterAdapter) CommentSyntax() engine.CommentSyntax {
	return engine.CommentSyntax{
		Line:       []string{"//"},
		BlockOpen:  "/*",
		BlockClose: "*/",
		Quote:      []rune{'"', '\''},
		RawString:  []string{"`"},
	}
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
