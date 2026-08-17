package java

import (
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	Treesitter "github.com/ast-metrics/ast-metrics/internal/engine/treesitter"
	sitter "github.com/smacker/go-tree-sitter"
	tsJava "github.com/smacker/go-tree-sitter/java"
)

type TreeSitterAdapter struct {
	src []byte
	// root caches the tree shared by the runner, to avoid re-parsing
	root *sitter.Node
	// pkg caches the declared package name (parsed lazily from src)
	pkg       string
	pkgParsed bool
	// srcLines holds the source split into lines, so that the extractors called
	// once per function do not split the whole file again for each of them
	srcLines Treesitter.LineCache
}

func NewTreeSitterAdapter(src []byte) *TreeSitterAdapter   { return &TreeSitterAdapter{src: src} }
func (a *TreeSitterAdapter) SetSource(src []byte)          { a.src = src; a.root = nil }
func (a *TreeSitterAdapter) SetRootNode(root *sitter.Node) { a.root = root }
func (a *TreeSitterAdapter) Language() *sitter.Language    { return tsJava.GetLanguage() }

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

// These four questions are asked on every node of every walk, so they are
// answered by symbol id rather than by node type name. See
// internal/engine/treesitter/symbols.go.
var (
	javaModules = &Treesitter.TypeSet{Language: tsJava.GetLanguage, Types: []string{"program"}}

	// enums, records and annotation types are class-like containers in Java:
	// they hold fields and methods, so they count as classes for metrics
	javaClasses = &Treesitter.TypeSet{Language: tsJava.GetLanguage, Types: []string{"class_declaration", "enum_declaration", "record_declaration", "annotation_type_declaration"}}

	javaInterfaces = &Treesitter.TypeSet{Language: tsJava.GetLanguage, Types: []string{"interface_declaration"}}

	// lambdas are intentionally not named functions: they have no name and would
	// pollute method counts, and their bodies still contribute decisions to the
	// enclosing method through the fallback recursion
	javaFunctions = &Treesitter.TypeSet{Language: tsJava.GetLanguage, Types: []string{"method_declaration", "constructor_declaration"}}
)

func (a *TreeSitterAdapter) IsModule(n *sitter.Node) bool { return javaModules.Has(n) }

func (a *TreeSitterAdapter) IsClass(n *sitter.Node) bool { return javaClasses.Has(n) }

func (a *TreeSitterAdapter) IsInterface(n *sitter.Node) bool { return javaInterfaces.Has(n) }

func (a *TreeSitterAdapter) IsFunction(n *sitter.Node) bool { return javaFunctions.Has(n) }

func (a *TreeSitterAdapter) NodeName(n *sitter.Node) string {
	if a.src == nil || n == nil {
		return ""
	}
	if nm := n.ChildByFieldName("name"); nm != nil {
		return text(a.src, nm)
	}
	if id := firstChildOfType(n, "identifier"); id != nil {
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
	for _, t := range []string{"class_body", "interface_body", "enum_body", "annotation_type_body", "constructor_body", "block"} {
		if b := firstChildOfType(n, t); b != nil {
			return b
		}
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
	return firstChildOfType(n, "formal_parameters")
}

func (a *TreeSitterAdapter) EachParamIdent(params *sitter.Node, yield func(string)) {
	if params == nil || a.src == nil {
		return
	}
	for i := 0; i < int(params.ChildCount()); i++ {
		p := params.Child(i)
		switch p.Type() {
		case "formal_parameter", "receiver_parameter":
			if nm := p.ChildByFieldName("name"); nm != nil {
				yield(text(a.src, nm))
			}
		case "spread_parameter":
			// int... rest -> (spread_parameter (variable_declarator name: identifier))
			if vd := firstChildOfType(p, "variable_declarator"); vd != nil {
				if nm := vd.ChildByFieldName("name"); nm != nil {
					yield(text(a.src, nm))
				}
			}
		}
	}
}

// ModuleNameFromPath ignores the file path and returns the declared package
// name (e.g. "com.example.app"), parsed once from the source. Empty string
// for the default package.
func (a *TreeSitterAdapter) ModuleNameFromPath(path string) string {
	if a.pkgParsed {
		return a.pkg
	}
	a.pkgParsed = true
	if a.src == nil {
		return ""
	}
	parser := sitter.NewParser()
	parser.SetLanguage(a.Language())
	tree := parser.Parse(nil, a.src)
	if tree == nil {
		return ""
	}
	root := tree.RootNode()
	for i := 0; i < int(root.ChildCount()); i++ {
		ch := root.Child(i)
		if ch.Type() != "package_declaration" {
			continue
		}
		for j := 0; j < int(ch.ChildCount()); j++ {
			c := ch.Child(j)
			if c.Type() == "scoped_identifier" || c.Type() == "identifier" {
				a.pkg = text(a.src, c)
				return a.pkg
			}
		}
	}
	return ""
}

func (a *TreeSitterAdapter) AttachQualified(parentClass string, fn string) string {
	if parentClass == "" {
		return fn
	}
	return parentClass + "." + fn
}

// NamespaceSeparator joins the package and the class name with "." (Java style).
func (a *TreeSitterAdapter) NamespaceSeparator() string { return "." }

func (a *TreeSitterAdapter) EachChildBody(body *sitter.Node, yield func(*sitter.Node)) {
	if body == nil {
		return
	}
	switch body.Type() {
	case "switch_block":
		// yield only the case groups: colon style groups and arrow rules
		for i := 0; i < int(body.ChildCount()); i++ {
			ch := body.Child(i)
			if ch.Type() == "switch_block_statement_group" || ch.Type() == "switch_rule" {
				yield(ch)
			}
		}
	default:
		for i := 0; i < int(body.ChildCount()); i++ {
			yield(body.Child(i))
		}
	}
}

// javaDecisions maps the Java grammar onto the shared complexity model.
//
// Java has no else_clause node: the else branch is the `alternative` field of
// if_statement, holding either a block (a plain else, free) or another
// if_statement (an else-if, counted as the if it is). Branches are counted on
// switch_label, which exists in both the classic and the arrow form, so that
// two labels sharing one body (`case 1: case 2:`) count as the two entry
// points they are. switch_expression covers statements and expressions alike.
var javaDecisions = &Treesitter.DecisionSpec{
	Language: tsJava.GetLanguage,
	If:       []string{"if_statement"},
	Loop:     []string{"for_statement", "enhanced_for_statement", "while_statement", "do_statement"},
	Switch:   []string{"switch_expression"},
	Case:     []string{"switch_label"},
	Catch:    []string{"catch_clause"},
	Ternary:  []string{"ternary_expression"},
	Logical:  []string{"binary_expression"},
	Ops:      []string{"&&", "||"},
}

func (a *TreeSitterAdapter) Decision(n *sitter.Node) Treesitter.DecisionKind {
	kind := javaDecisions.Classify(n, a.src)
	if kind == Treesitter.DecCase && Treesitter.HasChildOfType(n, "default") {
		return Treesitter.DecDefault
	}
	if kind == Treesitter.DecNone && Treesitter.IsElseBranch(n, "if_statement") {
		return Treesitter.DecElse
	}
	return kind
}

func (a *TreeSitterAdapter) LogicalOperator(n *sitter.Node) string {
	return javaDecisions.LogicalOperator(n, a.src)
}

// Imports returns the dependencies written on a node. An import names the
// package and the class it brings in. Every other reference to a type is
// returned with an empty Module: Java writes most types by their simple name,
// which the analysis resolves against the imports of the file, then the
// package of the file, the way the compiler does. The visitor calls this on
// every node it visits and files the result under the class and the method the
// node sits in.
func (a *TreeSitterAdapter) Imports(n *sitter.Node) []Treesitter.ImportItem {
	if n == nil {
		return nil
	}
	if n.Type() != "import_declaration" {
		return a.typeReferences(n)
	}
	var path string
	isWildcard := false
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		switch ch.Type() {
		case "scoped_identifier", "identifier":
			path = text(a.src, ch)
		case "asterisk":
			isWildcard = true
		}
	}
	if path == "" {
		return nil
	}
	if isWildcard {
		// import java.util.*;
		return []Treesitter.ImportItem{{Module: path, Name: ""}}
	}
	// import java.util.List; / import static org.junit.Assert.assertEquals;
	if idx := strings.LastIndex(path, "."); idx >= 0 {
		return []Treesitter.ImportItem{{Module: path[:idx], Name: path[idx+1:]}}
	}
	return []Treesitter.ImportItem{{Module: path, Name: ""}}
}

// typeReferences returns the types a node refers to, by the name written in
// the source: a simple name with an empty Module, a qualified name split into
// its package and its class.
//
// The visitor walks the body of a class and of a method, not their
// declaration: the types written on the declaration itself (superclass,
// interfaces, annotations, parameter and return types, thrown exceptions) are
// read when the visitor lands on the declaration, the ones written in a body
// when it lands on each type_identifier of the body.
func (a *TreeSitterAdapter) typeReferences(n *sitter.Node) []Treesitter.ImportItem {
	items := []Treesitter.ImportItem{}
	add := func(name string) {
		if name == "" {
			return
		}
		if idx := strings.LastIndex(name, "."); idx >= 0 {
			items = append(items, Treesitter.ImportItem{Module: name[:idx], Name: name[idx+1:]})
			return
		}
		items = append(items, Treesitter.ImportItem{Module: "", Name: name})
	}
	// typesUnder collects every type written under a node.
	var typesUnder func(x *sitter.Node)
	typesUnder = func(x *sitter.Node) {
		if x == nil {
			return
		}
		switch x.Type() {
		case "type_identifier":
			add(text(a.src, x))
			return
		case "scoped_type_identifier":
			add(text(a.src, x))
			return
		case "marker_annotation", "annotation":
			if name := x.ChildByFieldName("name"); name != nil {
				add(text(a.src, name))
			}
			return
		}
		for i := 0; i < int(x.NamedChildCount()); i++ {
			typesUnder(x.NamedChild(i))
		}
	}
	switch n.Type() {
	case "class_declaration", "interface_declaration", "enum_declaration", "record_declaration", "annotation_type_declaration":
		for i := 0; i < int(n.NamedChildCount()); i++ {
			ch := n.NamedChild(i)
			switch ch.Type() {
			case "superclass", "super_interfaces", "extends_interfaces", "modifiers", "formal_parameters", "type_parameters":
				typesUnder(ch)
			}
		}
	case "method_declaration", "constructor_declaration":
		for i := 0; i < int(n.NamedChildCount()); i++ {
			ch := n.NamedChild(i)
			switch ch.Type() {
			case "block", "constructor_body", "identifier":
				continue
			default:
				typesUnder(ch)
			}
		}
	case "type_identifier":
		// inside a body; the ones under a declaration were read with it, and
		// the last segment of a qualified name goes with the whole name
		if parent := n.Parent(); parent != nil && parent.Type() == "scoped_type_identifier" {
			return nil
		}
		add(text(a.src, n))
	case "scoped_type_identifier":
		if parent := n.Parent(); parent != nil && parent.Type() == "scoped_type_identifier" {
			return nil
		}
		add(text(a.src, n))
	case "marker_annotation", "annotation":
		if name := n.ChildByFieldName("name"); name != nil {
			add(text(a.src, name))
		}
	case "method_invocation", "field_access":
		// Util.go(), Const.MAX: a capitalised receiver is a class by
		// convention, a variable would be written in lower case
		if object := n.ChildByFieldName("object"); object != nil {
			switch object.Type() {
			case "identifier":
				if name := text(a.src, object); looksLikeJavaType(name) {
					add(name)
				}
			case "field_access":
				// a.b.Foo.bar(): a qualified class name written inline
				if name := text(a.src, object); looksLikeJavaQualifiedType(name) {
					add(name)
				}
			}
		}
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

// looksLikeJavaType tells whether a simple name is written like a class.
func looksLikeJavaType(name string) bool {
	return name != "" && name[0] >= 'A' && name[0] <= 'Z' && strings.ToUpper(name) != name
}

// looksLikeJavaQualifiedType tells whether a dotted name is a package
// followed by a class: lower case segments then one capitalised.
func looksLikeJavaQualifiedType(name string) bool {
	idx := strings.LastIndex(name, ".")
	if idx <= 0 {
		return false
	}
	pkg, class := name[:idx], name[idx+1:]
	if !looksLikeJavaType(class) {
		return false
	}
	for _, segment := range strings.Split(pkg, ".") {
		if segment == "" || segment[0] < 'a' || segment[0] > 'z' {
			return false
		}
	}
	return true
}

// IsLogicalNode reports whether a node begins a logical line. In Java, local
// variable declarations are statements but their node type does not carry the
// "_statement" suffix.
// javaStatements maps the Java grammar onto the shared logical-lines model.
//
// `switch_expression` is the single node the grammar uses for a switch, whether
// it is written as a statement or as an expression. It carries no "_statement"
// suffix, which is exactly how the switch line used to go uncounted in Java
// while counting in the six other languages. `switch_label` and
// `switch_block_statement_group` are labels, and `field_declaration`,
// `static_initializer`, `method_declaration` and `import_declaration` declare
// members.
var javaStatements = &Treesitter.StatementSpec{
	Language: tsJava.GetLanguage,
	Statement: []string{
		"expression_statement", "local_variable_declaration",
		"if_statement",
		"for_statement", "enhanced_for_statement", "while_statement", "do_statement",
		"switch_expression", "try_statement", "try_with_resources_statement",
		"return_statement", "break_statement", "continue_statement",
		"throw_statement", "yield_statement", "assert_statement",
		"labeled_statement", "synchronized_statement",
	},
}

func (a *TreeSitterAdapter) Statement(n *sitter.Node) Treesitter.StatementKind {
	return javaStatements.Classify(n)
}

// CommentSyntax declares Java comment tokens: "//" and "/* */" only. "#" has no
// meaning in Java source.
func (a *TreeSitterAdapter) CommentSyntax() engine.CommentSyntax {
	return engine.CommentSyntax{
		Line:       []string{"//"},
		BlockOpen:  "/*",
		BlockClose: "*/",
		Quote:      []rune{'"', '\''},
		// a text block holds a value over several lines
		RawString: []string{`"""`},
	}
}

// javaOperatorTokens lists the anonymous token types counted as Halstead
// operators: arithmetic, comparison, logical, bitwise, assignments, the field
// access, the argument separator, the subscript, the ternary and the keywords
// that drive the control flow. Keywords count as operators: without them, a
// body made of plain statements ("return this.items;") would hold none at all,
// and its Halstead volume would collapse to zero.
//
// The ternary reports its "?" only: its ":" would count the same operator
// twice, and the ":" of an enhanced "for" is already covered by the "for".
var javaOperatorTokens = map[string]bool{
	"+": true, "-": true, "*": true, "/": true, "%": true,
	"==": true, "!=": true, "<": true, ">": true, "<=": true, ">=": true,
	"&&": true, "||": true, "!": true,
	"&": true, "|": true, "^": true, "~": true,
	"<<": true, ">>": true, ">>>": true,
	"=": true, "+=": true, "-=": true, "*=": true, "/=": true, "%=": true,
	"&=": true, "|=": true, "^=": true, "<<=": true, ">>=": true, ">>>=": true,
	"++": true, "--": true, "->": true, "::": true,
	".": true, ",": true, "[": true, "?": true, "...": true,
	"return": true, "if": true, "else": true, "for": true, "while": true,
	"do": true, "switch": true, "case": true, "default": true,
	"break": true, "continue": true, "yield": true,
	"new": true, "instanceof": true,
	"throw": true, "try": true, "catch": true, "finally": true,
	"synchronized": true, "assert": true,
}

// javaOperandTypes lists the named node types counted as Halstead operands.
// Literals are left out on purpose: the cohesion metrics read the operands,
// and two methods sharing the literal 0 are not cohesive.
var javaOperandTypes = map[string]bool{"identifier": true}

// javaCallTypes lists the node types counted as one call operator. Object
// creation is left out: it already reports its "new" keyword.
var javaCallTypes = map[string]bool{
	"method_invocation": true, "explicit_constructor_invocation": true,
}

// javaPruneTypes lists the node types never walked: a type is not an operand,
// and two methods returning a "String" are not cohesive. Modifiers,
// annotations and the "throws" clause describe the declaration, not what it
// computes.
var javaPruneTypes = map[string]bool{
	"type_identifier": true, "scoped_type_identifier": true,
	"generic_type": true, "type_arguments": true, "type_parameters": true,
	"array_type": true, "dimensions": true, "integral_type": true,
	"floating_point_type": true, "boolean_type": true, "void_type": true,
	"catch_type": true, "throws": true, "modifiers": true,
	"annotation": true, "marker_annotation": true, "wildcard": true,
	"superclass": true, "super_interfaces": true, "type_bound": true,
	"permits": true,
}

// javaChainTypes lists the field access node types. A Java method call has a
// node of its own ("method_invocation") and is not a field access.
var javaChainTypes = map[string]bool{"field_access": true}

var javaOperandSpec = Treesitter.OperandSpec{
	OperatorTokens: javaOperatorTokens,
	OperandTypes:   javaOperandTypes,
	CallTypes:      javaCallTypes,
	PruneTypes:     javaPruneTypes,
	ChainTypes:     javaChainTypes,
	// no Receiver: the current object is the keyword "this"
}

// ExtractOperatorsOperands collects Halstead operators and operands from the
// AST within the given 1-based inclusive line range. A field access is a
// single operand ("this.items", "System.out"), and an access through "this"
// reads the field whatever is done with it afterwards.
func (a *TreeSitterAdapter) ExtractOperatorsOperands(src []byte, startLine, endLine int) ([]string, []string) {
	root, source := a.ensureRoot(src)
	if root == nil {
		return nil, nil
	}
	return javaOperandSpec.Extract(root, source, startLine, endLine)
}

// ExtractMethodCalls extracts method calls like this.foo, super.bar from Java source.
func (a *TreeSitterAdapter) ExtractMethodCalls(src []byte, startLine, endLine int) []string {
	if src == nil || startLine <= 0 || endLine <= 0 || endLine < startLine {
		return nil
	}
	lines := a.srcLines.Lines(src)
	var calls []string
	for i := startLine - 1; i < endLine && i < len(lines); i++ {
		ln := strings.TrimSpace(lines[i])
		if ln == "" {
			continue
		}
		ln = stripJavaStrings(ln)
		if idx := strings.Index(ln, "//"); idx >= 0 {
			ln = ln[:idx]
		}
		for _, prefix := range []string{"this.", "super."} {
			rest := ln
			for {
				idx := strings.Index(rest, prefix)
				if idx < 0 {
					break
				}
				after := rest[idx+len(prefix):]
				end := 0
				for end < len(after) && (after[end] == '_' || after[end] == '$' || (after[end] >= 'a' && after[end] <= 'z') || (after[end] >= 'A' && after[end] <= 'Z') || (after[end] >= '0' && after[end] <= '9')) {
					end++
				}
				if end > 0 {
					calls = append(calls, prefix[:len(prefix)-1]+"."+after[:end])
				}
				rest = after[end:]
			}
		}
	}
	return calls
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

// stripJavaStrings removes content inside string and char literals to avoid
// false positives in comment/operator scanning.
func stripJavaStrings(s string) string {
	out := make([]rune, 0, len(s))
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
		if !inSq && c == '"' {
			inDq = !inDq
			continue
		}
		if !inDq && c == '\'' {
			inSq = !inSq
			continue
		}
		if inDq || inSq {
			continue
		}
		out = append(out, rune(c))
	}
	return string(out)
}
