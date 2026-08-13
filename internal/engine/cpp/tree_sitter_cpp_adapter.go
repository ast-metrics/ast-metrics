package cpp

import (
	"path/filepath"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	Treesitter "github.com/ast-metrics/ast-metrics/internal/engine/treesitter"
	sitter "github.com/smacker/go-tree-sitter"
	tsCpp "github.com/smacker/go-tree-sitter/cpp"
)

type TreeSitterAdapter struct {
	src  []byte
	root *sitter.Node
}

func NewTreeSitterAdapter(src []byte) *TreeSitterAdapter   { return &TreeSitterAdapter{src: src} }
func (a *TreeSitterAdapter) SetSource(src []byte)          { a.src, a.root = src, nil }
func (a *TreeSitterAdapter) SetRootNode(root *sitter.Node) { a.root = root }
func (a *TreeSitterAdapter) Language() *sitter.Language    { return tsCpp.GetLanguage() }
func (a *TreeSitterAdapter) IsModule(n *sitter.Node) bool {
	return n != nil && n.Type() == "translation_unit"
}
func (a *TreeSitterAdapter) NamespaceSeparator() string { return "::" }

func (a *TreeSitterAdapter) IsClass(n *sitter.Node) bool {
	return n != nil && (n.Type() == "class_specifier" || n.Type() == "struct_specifier")
}

func (a *TreeSitterAdapter) IsFunction(n *sitter.Node) bool {
	return n != nil && n.Type() == "function_definition"
}

func (a *TreeSitterAdapter) NodeName(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	if a.IsClass(n) {
		return a.declaratorName(n.ChildByFieldName("name"))
	}
	if a.IsFunction(n) {
		return a.declaratorName(n.ChildByFieldName("declarator"))
	}
	return a.declaratorName(n.ChildByFieldName("name"))
}

// declaratorName follows C++'s nested declarator shapes. In particular, a
// function name may sit below pointer/reference, function, qualified,
// destructor, or operator declarators rather than being a direct child.
func (a *TreeSitterAdapter) declaratorName(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "identifier", "field_identifier", "type_identifier", "destructor_name", "operator_name", "literal_operator_name":
		return nodeText(a.src, n)
	case "qualified_identifier", "scoped_identifier", "template_function", "template_type":
		if name := n.ChildByFieldName("name"); name != nil {
			return a.declaratorName(name)
		}
	}
	for _, field := range []string{"declarator", "name"} {
		if child := n.ChildByFieldName(field); child != nil && child != n {
			if name := a.declaratorName(child); name != "" {
				return name
			}
		}
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		if name := a.declaratorName(n.NamedChild(i)); name != "" {
			return name
		}
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
	return firstChildOfType(n, "field_declaration_list", "compound_statement", "declaration_list")
}

func (a *TreeSitterAdapter) NodeParams(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	return firstDescendantOfType(n.ChildByFieldName("declarator"), "parameter_list")
}

func (a *TreeSitterAdapter) EachParamIdent(params *sitter.Node, yield func(string)) {
	if params == nil {
		return
	}
	for i := 0; i < int(params.NamedChildCount()); i++ {
		p := params.NamedChild(i)
		if p.Type() != "parameter_declaration" && p.Type() != "optional_parameter_declaration" {
			continue
		}
		if name := a.declaratorName(p.ChildByFieldName("declarator")); name != "" {
			yield(name)
		}
	}
}

func (a *TreeSitterAdapter) ModuleNameFromPath(path string) string {
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}
func (a *TreeSitterAdapter) AttachQualified(parent, fn string) string {
	if parent == "" {
		return fn
	}
	return parent + "::" + fn
}
func (a *TreeSitterAdapter) EachChildBody(body *sitter.Node, yield func(*sitter.Node)) {
	if body == nil {
		return
	}
	for i := 0; i < int(body.NamedChildCount()); i++ {
		yield(body.NamedChild(i))
	}
}

var cppDecisions = &Treesitter.DecisionSpec{
	If:     []string{"if_statement"},
	Loop:   []string{"for_statement", "for_range_loop", "while_statement", "do_statement"},
	Switch: []string{"switch_statement"}, Case: []string{"case_statement"}, Default: []string{"default_statement"},
	Catch: []string{"catch_clause"}, Ternary: []string{"conditional_expression"},
	Logical: []string{"binary_expression"}, Ops: []string{"&&", "||"},
}

func (a *TreeSitterAdapter) Decision(n *sitter.Node) Treesitter.DecisionKind {
	kind := cppDecisions.Classify(n, a.src)
	if kind == Treesitter.DecCase && Treesitter.HasChildOfType(n, "default") {
		return Treesitter.DecDefault
	}
	if kind == Treesitter.DecNone && Treesitter.IsElseBranch(n, "if_statement") {
		return Treesitter.DecElse
	}
	return kind
}
func (a *TreeSitterAdapter) LogicalOperator(n *sitter.Node) string {
	return cppDecisions.LogicalOperator(n, a.src)
}

var cppStatements = &Treesitter.StatementSpec{
	Statement:        []string{"expression_statement", "if_statement", "for_statement", "for_range_loop", "while_statement", "do_statement", "switch_statement", "try_statement", "return_statement", "break_statement", "continue_statement", "goto_statement", "throw_statement", "co_return_statement"},
	LocalDeclaration: []string{"declaration"},
}

func (a *TreeSitterAdapter) Statement(n *sitter.Node) Treesitter.StatementKind {
	return cppStatements.Classify(n)
}

func (a *TreeSitterAdapter) Imports(n *sitter.Node) []Treesitter.ImportItem {
	if n == nil || n.Type() != "preproc_include" {
		return nil
	}
	path := n.ChildByFieldName("path")
	if path == nil {
		path = firstChildOfType(n, "system_lib_string", "string_literal")
	}
	module := strings.Trim(nodeText(a.src, path), "<>\"")
	if module == "" {
		return nil
	}
	return []Treesitter.ImportItem{{Module: module}}
}

func (a *TreeSitterAdapter) CommentSyntax() engine.CommentSyntax {
	return engine.CommentSyntax{Line: []string{"//"}, BlockOpen: "/*", BlockClose: "*/", Quote: []rune{34, 39}, RawString: []string{"R\""}}
}

var cppOperators = map[string]bool{
	"+": true, "-": true, "*": true, "/": true, "%": true, "==": true, "!=": true, "<": true, ">": true, "<=": true, ">=": true,
	"&&": true, "||": true, "!": true, "&": true, "|": true, "^": true, "~": true, "<<": true, ">>": true, "=": true, "+=": true, "-=": true, "*=": true, "/=": true, "%=": true, "&=": true, "|=": true, "^=": true, "<<=": true, ">>=": true, "++": true, "--": true,
	".": true, "->": true, "::": true, ",": true, "[": true, "?": true, "return": true, "if": true, "else": true, "for": true, "while": true, "do": true, "switch": true, "case": true, "default": true, "break": true, "continue": true, "new": true, "delete": true, "throw": true, "try": true, "catch": true, "co_return": true,
}
var cppOperands = Treesitter.OperandSpec{
	OperatorTokens: cppOperators,
	OperandTypes:   map[string]bool{"identifier": true, "field_identifier": true},
	CallTypes:      map[string]bool{"call_expression": true},
	PruneTypes:     map[string]bool{"primitive_type": true, "type_identifier": true, "sized_type_specifier": true, "template_argument_list": true, "type_descriptor": true, "placeholder_type_specifier": true},
	ChainTypes:     map[string]bool{"field_expression": true},
}

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
	p := sitter.NewParser()
	p.SetLanguage(a.Language())
	a.root = p.Parse(nil, source).RootNode()
	return a.root, source
}
func (a *TreeSitterAdapter) ExtractOperatorsOperands(src []byte, startLine, endLine int) ([]string, []string) {
	root, source := a.ensureRoot(src)
	if root == nil {
		return nil, nil
	}
	return cppOperands.Extract(root, source, startLine, endLine)
}
func (a *TreeSitterAdapter) ExtractMethodCalls(src []byte, startLine, endLine int) []string {
	root, source := a.ensureRoot(src)
	if root == nil {
		return nil
	}
	return cppOperands.MethodCalls(root, source, startLine, endLine)
}

func (a *TreeSitterAdapter) ClassDirectOperands(n *sitter.Node) []string {
	body := a.NodeBody(n)
	if body == nil {
		return nil
	}
	var result []string
	for i := 0; i < int(body.NamedChildCount()); i++ {
		field := body.NamedChild(i)
		if field.Type() != "field_declaration" {
			continue
		}
		if declarator := field.ChildByFieldName("declarator"); declarator != nil {
			if name := a.declaratorName(declarator); name != "" {
				result = append(result, name)
			}
			continue
		}
		for j := 0; j < int(field.NamedChildCount()); j++ {
			child := field.NamedChild(j)
			if declarator := child.ChildByFieldName("declarator"); declarator != nil {
				if name := a.declaratorName(declarator); name != "" {
					result = append(result, name)
				}
			}
		}
	}
	return result
}

// ReceiverTypeName binds an out-of-class definition (C::f) back to C when the
// class is declared in the same translation unit.
func (a *TreeSitterAdapter) ReceiverTypeName(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	d := n.ChildByFieldName("declarator")
	q := firstDescendantOfType(d, "qualified_identifier")
	if q == nil {
		return ""
	}
	scope := q.ChildByFieldName("scope")
	if scope == nil {
		return ""
	}
	name := nodeText(a.src, scope)
	if idx := strings.LastIndex(name, "::"); idx >= 0 {
		name = name[idx+2:]
	}
	return name
}

func nodeText(src []byte, n *sitter.Node) string {
	if n == nil || src == nil {
		return ""
	}
	return string(src[n.StartByte():n.EndByte()])
}
func firstChildOfType(n *sitter.Node, types ...string) *sitter.Node {
	if n == nil {
		return nil
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		for _, t := range types {
			if n.NamedChild(i).Type() == t {
				return n.NamedChild(i)
			}
		}
	}
	return nil
}
func firstDescendantOfType(n *sitter.Node, typ string) *sitter.Node {
	if n == nil {
		return nil
	}
	if n.Type() == typ {
		return n
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		if found := firstDescendantOfType(n.NamedChild(i), typ); found != nil {
			return found
		}
	}
	return nil
}
