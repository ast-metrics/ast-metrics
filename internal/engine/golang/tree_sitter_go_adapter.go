package golang

import (
	"path/filepath"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	Treesitter "github.com/ast-metrics/ast-metrics/internal/engine/treesitter"
	sitter "github.com/smacker/go-tree-sitter"
	tsGo "github.com/smacker/go-tree-sitter/golang"
)

type TreeSitterAdapter struct {
	src  []byte
	root *sitter.Node
}

func NewTreeSitterAdapter(src []byte) *TreeSitterAdapter   { return &TreeSitterAdapter{src: src} }
func (a *TreeSitterAdapter) SetSource(src []byte)          { a.src = src }
func (a *TreeSitterAdapter) SetRootNode(root *sitter.Node) { a.root = root }
func (a *TreeSitterAdapter) Language() *sitter.Language    { return tsGo.GetLanguage() }

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

func (a *TreeSitterAdapter) IsModule(n *sitter.Node) bool { return n.Type() == "source_file" }
func (a *TreeSitterAdapter) IsClass(n *sitter.Node) bool {
	return n.Type() == "type_declaration" && firstChildOfType(n, "type_spec") != nil && firstDescendantOfType(n, "type_identifier") != nil && firstDescendantOfType(n, "type_parameter_list") == nil && firstDescendantOfType(n, "struct_type") != nil
}
func (a *TreeSitterAdapter) IsFunction(n *sitter.Node) bool {
	return n.Type() == "function_declaration" || n.Type() == "method_declaration"
}

func (a *TreeSitterAdapter) NodeName(n *sitter.Node) string {
	switch n.Type() {
	case "function_declaration":
		if id := firstChildOfType(n, "identifier"); id != nil {
			return text(a.src, id)
		}
	case "method_declaration":
		if id := firstChildOfType(n, "field_identifier"); id != nil {
			return text(a.src, id)
		}
	case "type_declaration":
		if id := firstDescendantOfType(n, "type_identifier"); id != nil {
			return text(a.src, id)
		}
	}
	return ""
}

func (a *TreeSitterAdapter) NodeBody(n *sitter.Node) *sitter.Node {
	switch n.Type() {
	case "function_declaration":
		return firstChildOfType(n, "block")
	case "method_declaration":
		return firstChildOfType(n, "block")
	case "type_declaration":
		return firstDescendantOfType(n, "field_declaration_list")
	}
	return nil
}

func (a *TreeSitterAdapter) NodeParams(n *sitter.Node) *sitter.Node {
	switch n.Type() {
	case "function_declaration", "method_declaration":
		// on a method, the first parameter_list child holds the receiver, not the
		// parameters: rely on the field name to get the real ones
		if p := n.ChildByFieldName("parameters"); p != nil {
			return p
		}
		return firstChildOfType(n, "parameter_list")
	}
	return nil
}

// ReceiverTypeName returns the type a method is bound to: "Counter" for
// `func (c *Counter) Add(n int)`. It is empty for a plain function. The visitor
// uses it to attach the method to its struct, which Go declares separately.
func (a *TreeSitterAdapter) ReceiverTypeName(n *sitter.Node) string {
	if n == nil || n.Type() != "method_declaration" {
		return ""
	}
	receiver := n.ChildByFieldName("receiver")
	if receiver == nil {
		return ""
	}
	if t := firstDescendantOfType(receiver, "type_identifier"); t != nil {
		return text(a.src, t)
	}
	return ""
}

func (a *TreeSitterAdapter) EachParamIdent(params *sitter.Node, yield func(string)) {
	if params == nil {
		return
	}
	for i := 0; i < int(params.ChildCount()); i++ {
		p := params.Child(i)
		if p.Type() == "parameter_declaration" {
			// collect identifiers under this param decl
			eachDescendantOfType(p, "identifier", func(id *sitter.Node) { yield(text(a.src, id)) })
		}
	}
}

func (a *TreeSitterAdapter) ModuleNameFromPath(path string) string {
	// Prefer actual Go package name from source, fallback to file base name
	if a != nil && a.src != nil {
		lines := strings.Split(string(a.src), "\n")
		for _, ln := range lines {
			trim := strings.TrimSpace(ln)
			if trim == "" || strings.HasPrefix(trim, "//") {
				continue
			}
			// strip block comment openers quickly (best-effort)
			if strings.HasPrefix(trim, "/*") {
				continue
			}
			if strings.HasPrefix(trim, "package ") {
				pkg := strings.TrimSpace(strings.TrimPrefix(trim, "package "))
				// remove inline comment if any
				if idx := strings.Index(pkg, "//"); idx >= 0 {
					pkg = strings.TrimSpace(pkg[:idx])
				}
				if idx := strings.Index(pkg, "/*"); idx >= 0 {
					pkg = strings.TrimSpace(pkg[:idx])
				}
				if pkg != "" {
					return pkg
				}
			}
		}
	}
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return base
}

func (a *TreeSitterAdapter) AttachQualified(parent string, fn string) string {
	if parent == "" {
		return fn
	}
	return parent + "." + fn
}

func (a *TreeSitterAdapter) EachChildBody(body *sitter.Node, yield func(*sitter.Node)) {
	if body == nil {
		return
	}
	for i := 0; i < int(body.ChildCount()); i++ {
		yield(body.Child(i))
	}
}

// goDecisions maps the Go grammar onto the shared complexity model.
//
// Go has no ternary and no exception handler, so those kinds never occur. The
// `else` branch is not a node of its own: it is the `alternative` field of
// if_statement, holding either a block (a plain else, which costs nothing) or
// another if_statement (an else-if, counted as the if it is). `range_clause`
// and `for_clause` are children of `for_statement`, which is the single loop
// node of the grammar, so only the latter is listed.
var goDecisions = &Treesitter.DecisionSpec{
	If:      []string{"if_statement"},
	Loop:    []string{"for_statement"},
	Switch:  []string{"expression_switch_statement", "type_switch_statement", "select_statement"},
	Case:    []string{"expression_case", "type_case", "communication_case"},
	Default: []string{"default_case"},
	Logical: []string{"binary_expression"},
	Ops:     []string{"&&", "||"},
}

func (a *TreeSitterAdapter) Decision(n *sitter.Node) Treesitter.DecisionKind {
	kind := goDecisions.Classify(n, a.src)
	if kind == Treesitter.DecNone && Treesitter.IsElseBranch(n, "if_statement") {
		return Treesitter.DecElse
	}
	return kind
}

func (a *TreeSitterAdapter) LogicalOperator(n *sitter.Node) string {
	return goDecisions.LogicalOperator(n, a.src)
}

func (a *TreeSitterAdapter) Imports(n *sitter.Node) []Treesitter.ImportItem {
	if n == nil {
		return nil
	}
	items := []Treesitter.ImportItem{}
	switch n.Type() {
	case "import_declaration":
		// walk import specs
		var walk func(*sitter.Node)
		walk = func(x *sitter.Node) {
			if x == nil {
				return
			}
			if x.Type() == "import_spec" {
				var module string
				var alias string
				// path is string_literal
				if p := firstDescendantOfType(x, "interpreted_string_literal"); p != nil {
					module = strings.Trim(text(a.src, p), "`\"")
				} else if q := firstDescendantOfType(x, "raw_string_literal"); q != nil {
					module = strings.Trim(text(a.src, q), "`\"")
				}
				// alias is optional identifier as first child
				if id := firstChildOfType(x, "identifier"); id != nil {
					alias = text(a.src, id)
				}
				name := alias
				if name == "" {
					// default to last segment
					if idx := strings.LastIndex(module, "/"); idx >= 0 {
						name = module[idx+1:]
					} else {
						name = module
					}
				}
				if module != "" {
					items = append(items, Treesitter.ImportItem{Module: module, Name: name})
				}
			}
			for i := 0; i < int(x.ChildCount()); i++ {
				walk(x.Child(i))
			}
		}
		walk(n)
	}
	return items
}

// helpers
func text(src []byte, n *sitter.Node) string { return string(src[n.StartByte():n.EndByte()]) }
func firstChildOfType(n *sitter.Node, t string) *sitter.Node {
	for i := 0; i < int(n.ChildCount()); i++ {
		if c := n.Child(i); c.Type() == t {
			return c
		}
	}
	return nil
}
func firstDescendantOfType(n *sitter.Node, t string) *sitter.Node {
	var res *sitter.Node
	eachDescendantOfType(n, t, func(n *sitter.Node) {
		if res == nil {
			res = n
		}
	})
	return res
}
func eachDescendantOfType(n *sitter.Node, t string, yield func(*sitter.Node)) {
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		if c.Type() == t {
			yield(c)
		}
		eachDescendantOfType(c, t, yield)
	}
}

// goOperatorTokens lists the anonymous token types counted as Halstead
// operators: arithmetic, comparison, logical, bitwise, assignments, channel
// operations, the selector, the argument separator, the index and the keywords
// that drive the control flow. Keywords count as operators: without them, a
// body made of plain statements ("return c.items") would hold none at all, and
// its Halstead volume would collapse to zero.
var goOperatorTokens = map[string]bool{
	"+": true, "-": true, "*": true, "/": true, "%": true,
	"==": true, "!=": true, "<": true, ">": true, "<=": true, ">=": true,
	"&&": true, "||": true, "!": true,
	"&": true, "|": true, "^": true, "<<": true, ">>": true, "&^": true,
	"=": true, ":=": true, "+=": true, "-=": true, "*=": true, "/=": true, "%=": true,
	"&=": true, "|=": true, "^=": true, "<<=": true, ">>=": true, "&^=": true,
	"++": true, "--": true, "<-": true,
	".": true, ",": true, "[": true, "...": true,
	"return": true, "if": true, "else": true, "for": true, "range": true,
	"switch": true, "case": true, "default": true, "select": true,
	"break": true, "continue": true, "fallthrough": true, "goto": true,
	"go": true, "defer": true,
}

// goOperandTypes lists the named node types counted as Halstead operands.
// Literals are left out on purpose: the cohesion metrics read the operands,
// and two methods sharing the literal 0 are not cohesive.
var goOperandTypes = map[string]bool{
	"identifier": true, "field_identifier": true,
}

// goPruneTypes lists the node types never walked: a type is not an operand,
// and two methods sharing the type "int" are not cohesive.
var goPruneTypes = map[string]bool{
	"type_identifier": true, "qualified_type": true, "pointer_type": true,
	"slice_type": true, "array_type": true, "map_type": true, "channel_type": true,
	"function_type": true, "struct_type": true, "interface_type": true,
	"generic_type": true, "type_arguments": true, "type_parameter_list": true,
	"type_declaration": true,
}

// goPruneFields lists the children never walked, by field name: the receiver
// and the result of a method hold types, not operands.
var goPruneFields = map[string]bool{
	"receiver": true, "result": true,
}

var goChainTypes = map[string]bool{"selector_expression": true}

// goCallTypes lists the node types counted as one call operator. A type
// conversion ("int64(x)") reads as a call in the Go grammar, which is fine:
// both apply an operator to an expression.
var goCallTypes = map[string]bool{"call_expression": true}

func (a *TreeSitterAdapter) operandSpec(root *sitter.Node, src []byte, startLine int) Treesitter.OperandSpec {
	return Treesitter.OperandSpec{
		OperatorTokens: goOperatorTokens,
		OperandTypes:   goOperandTypes,
		CallTypes:      goCallTypes,
		PruneTypes:     goPruneTypes,
		PruneFields:    goPruneFields,
		ChainTypes:     goChainTypes,
		// the receiver of the method is the Go equivalent of "this":
		// normalizing it is what lets the cohesion metrics tell an attribute
		// access from a local variable
		Receiver: goReceiverIdent(root, src, startLine),
	}
}

// ExtractOperatorsOperands collects Halstead operators and operands from the
// AST within the given 1-based inclusive line range.
func (a *TreeSitterAdapter) ExtractOperatorsOperands(src []byte, startLine, endLine int) ([]string, []string) {
	root, source := a.ensureRoot(src)
	if root == nil {
		return nil, nil
	}
	return a.operandSpec(root, source, startLine).Extract(root, source, startLine, endLine)
}

// ExtractMethodCalls returns the methods called on the receiver of the method
// declared at startLine, normalized as "this.Name": `e.reset()` gives
// "this.reset". Calls made on another variable or on a package say nothing
// about the cohesion of the struct and are not reported.
func (a *TreeSitterAdapter) ExtractMethodCalls(src []byte, startLine, endLine int) []string {
	root, source := a.ensureRoot(src)
	if root == nil {
		return nil
	}
	spec := a.operandSpec(root, source, startLine)
	if spec.Receiver == "" {
		return nil
	}
	return spec.MethodCalls(root, source, startLine, endLine)
}

// goReceiverIdent returns the name of the receiver of the method declared at
// startLine: "e" for `func (e *Example) Increment()`. It is empty for a plain
// function, or for a method that does not name its receiver.
func goReceiverIdent(root *sitter.Node, src []byte, startLine int) string {
	method := goMethodAtLine(root, startLine)
	if method == nil {
		return ""
	}
	receiver := method.ChildByFieldName("receiver")
	if receiver == nil {
		return ""
	}
	decl := firstChildOfType(receiver, "parameter_declaration")
	if decl == nil {
		return ""
	}
	name := decl.ChildByFieldName("name")
	if name == nil || name.Type() != "identifier" {
		return ""
	}
	if ident := text(src, name); ident != "_" {
		return ident
	}
	return ""
}

// goMethodAtLine returns the method declared on the given 1-based line, or nil.
func goMethodAtLine(n *sitter.Node, line int) *sitter.Node {
	if int(n.EndPoint().Row)+1 < line || int(n.StartPoint().Row)+1 > line {
		return nil
	}
	if n.Type() == "method_declaration" && int(n.StartPoint().Row)+1 == line {
		return n
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if found := goMethodAtLine(n.Child(i), line); found != nil {
			return found
		}
	}
	return nil
}

// goStatements maps the Go grammar onto the shared logical-lines model.
//
// Go spells a package-level constant and a local one with the same node, so
// `const_declaration`, `var_declaration` and `type_declaration` are listed as
// local declarations: at file scope they declare a member of the package, and
// only inside a function are they instructions. `short_var_declaration` has no
// such ambiguity, it exists only in a function body.
//
// The case clauses of a switch and of a select are labels, not instructions:
// what they hold counts on its own lines.
var goStatements = &Treesitter.StatementSpec{
	Statement: []string{
		"assignment_statement", "expression_statement", "inc_statement", "dec_statement",
		"short_var_declaration", "send_statement",
		"if_statement", "for_statement",
		"expression_switch_statement", "type_switch_statement", "select_statement",
		"return_statement", "break_statement", "continue_statement",
		"goto_statement", "fallthrough_statement", "labeled_statement",
		"go_statement", "defer_statement",
	},
	LocalDeclaration: []string{"var_declaration", "const_declaration", "type_declaration"},
}

func (a *TreeSitterAdapter) Statement(n *sitter.Node) Treesitter.StatementKind {
	return goStatements.Classify(n)
}

// CommentSyntax declares Go comment tokens: "//" and "/* */" only. "#" has no
// meaning in Go source. Backticks open a raw string literal, whose content must
// be ignored like any other string.
func (a *TreeSitterAdapter) CommentSyntax() engine.CommentSyntax {
	return engine.CommentSyntax{
		Line:       []string{"//"},
		BlockOpen:  "/*",
		BlockClose: "*/",
		Quote:      []rune{'"', '\''},
		RawString:  []string{"`"},
	}
}
