package csharp

import (
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	Treesitter "github.com/ast-metrics/ast-metrics/internal/engine/treesitter"
	sitter "github.com/smacker/go-tree-sitter"
	tsCSharp "github.com/smacker/go-tree-sitter/csharp"
)

type TreeSitterAdapter struct {
	src []byte
	// root caches the tree shared by the runner, to avoid re-parsing
	root *sitter.Node
	// ns caches the declared namespace (parsed lazily from src)
	ns       string
	nsParsed bool
	// srcLines holds the source split into lines, so that the extractors called
	// once per function do not split the whole file again for each of them
	srcLines Treesitter.LineCache
}

func NewTreeSitterAdapter(src []byte) *TreeSitterAdapter   { return &TreeSitterAdapter{src: src} }
func (a *TreeSitterAdapter) SetSource(src []byte)          { a.src = src; a.root = nil }
func (a *TreeSitterAdapter) SetRootNode(root *sitter.Node) { a.root = root }
func (a *TreeSitterAdapter) Language() *sitter.Language    { return tsCSharp.GetLanguage() }

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
	// namespace_declaration bodies are reached through the fallback recursion;
	// the namespace name itself comes from ModuleNameFromPath
	csModules = &Treesitter.TypeSet{Language: tsCSharp.GetLanguage, Types: []string{"compilation_unit"}}

	// structs, records (including record struct) and enums are class-like
	// containers in C#: they hold fields and methods, so they count as classes
	// for metrics
	csClasses = &Treesitter.TypeSet{Language: tsCSharp.GetLanguage, Types: []string{"class_declaration", "struct_declaration", "record_declaration", "enum_declaration"}}

	csInterfaces = &Treesitter.TypeSet{Language: tsCSharp.GetLanguage, Types: []string{"interface_declaration"}}

	// property accessors (get/set/init) and lambdas are intentionally not named
	// functions; their bodies still contribute decisions through the fallback
	// recursion
	csFunctions = &Treesitter.TypeSet{Language: tsCSharp.GetLanguage, Types: []string{"method_declaration", "constructor_declaration", "local_function_statement", "destructor_declaration"}}
)

func (a *TreeSitterAdapter) IsModule(n *sitter.Node) bool { return csModules.Has(n) }

func (a *TreeSitterAdapter) IsClass(n *sitter.Node) bool { return csClasses.Has(n) }

func (a *TreeSitterAdapter) IsInterface(n *sitter.Node) bool { return csInterfaces.Has(n) }

func (a *TreeSitterAdapter) IsFunction(n *sitter.Node) bool { return csFunctions.Has(n) }

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
	// body field covers: declaration_list (types), block (methods),
	// arrow_expression_clause (expression-bodied members),
	// enum_member_declaration_list (enums)
	if body := n.ChildByFieldName("body"); body != nil {
		return body
	}
	for _, t := range []string{"declaration_list", "enum_member_declaration_list", "block", "arrow_expression_clause"} {
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
	return firstChildOfType(n, "parameter_list")
}

func (a *TreeSitterAdapter) EachParamIdent(params *sitter.Node, yield func(string)) {
	if params == nil || a.src == nil {
		return
	}
	for i := 0; i < int(params.ChildCount()); i++ {
		p := params.Child(i)
		if p.Type() != "parameter" {
			continue
		}
		// skip type and ref/out/in modifiers; only the name field is a parameter name
		if nm := p.ChildByFieldName("name"); nm != nil {
			yield(text(a.src, nm))
		}
	}
}

// ModuleNameFromPath ignores the file path and returns the declared namespace
// (e.g. "App.Services"), parsed once from the source. Handles both block
// namespaces and file-scoped namespaces. When several block namespaces are
// declared, the first one is used (the shared Visitor stores a single
// namespace per file). Empty string when no namespace is declared.
func (a *TreeSitterAdapter) ModuleNameFromPath(path string) string {
	if a.nsParsed {
		return a.ns
	}
	a.nsParsed = true
	if a.src == nil {
		return ""
	}
	parser := sitter.NewParser()
	parser.SetLanguage(a.Language())
	tree := parser.Parse(nil, a.src)
	if tree == nil {
		return ""
	}
	var find func(n *sitter.Node) string
	find = func(n *sitter.Node) string {
		if n.Type() == "namespace_declaration" || n.Type() == "file_scoped_namespace_declaration" {
			if nm := n.ChildByFieldName("name"); nm != nil {
				return text(a.src, nm)
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			if found := find(n.Child(i)); found != "" {
				return found
			}
		}
		return ""
	}
	a.ns = find(tree.RootNode())
	return a.ns
}

func (a *TreeSitterAdapter) AttachQualified(parentClass string, fn string) string {
	if parentClass == "" {
		return fn
	}
	return parentClass + "." + fn
}

// NamespaceSeparator joins the namespace and the class name with "." (C# style).
func (a *TreeSitterAdapter) NamespaceSeparator() string { return "." }

func (a *TreeSitterAdapter) EachChildBody(body *sitter.Node, yield func(*sitter.Node)) {
	if body == nil {
		return
	}
	switch body.Type() {
	case "switch_body":
		// yield only the case sections
		for i := 0; i < int(body.ChildCount()); i++ {
			ch := body.Child(i)
			if ch.Type() == "switch_section" {
				yield(ch)
			}
		}
	case "switch_expression":
		// switch expressions have no body node: arms are direct children
		for i := 0; i < int(body.ChildCount()); i++ {
			ch := body.Child(i)
			if ch.Type() == "switch_expression_arm" {
				yield(ch)
			}
		}
	default:
		for i := 0; i < int(body.ChildCount()); i++ {
			yield(body.Child(i))
		}
	}
}

// csDecisions maps the C# grammar onto the shared complexity model.
//
// Like Java, C# has no else_clause node: the else branch is the `alternative`
// field of if_statement and needs no rule of its own. The grammar inlines the
// labels of a switch_section as bare `case` / `default` tokens, so branches are
// counted on those tokens (guarded by their parent below) rather than on the
// section, which lets a section carrying several labels count each of them.
// `??` is deliberately not a logical operator here, as in PHP and TypeScript.
var csDecisions = &Treesitter.DecisionSpec{
	Language: tsCSharp.GetLanguage,
	If:       []string{"if_statement"},
	Loop:     []string{"for_statement", "foreach_statement", "while_statement", "do_statement"},
	Switch:   []string{"switch_statement", "switch_expression"},
	Case:     []string{"case", "switch_expression_arm"},
	Default:  []string{"default"},
	Catch:    []string{"catch_clause"},
	Ternary:  []string{"conditional_expression"},
	Logical:  []string{"binary_expression"},
	Ops:      []string{"&&", "||"},
}

func (a *TreeSitterAdapter) Decision(n *sitter.Node) Treesitter.DecisionKind {
	kind := csDecisions.Classify(n, a.src)
	switch kind {
	case Treesitter.DecCase, Treesitter.DecDefault:
		// `case` and `default` are also keywords outside of a switch (goto
		// case, the default(T) expression): only a switch label is a branch
		if n.Type() == "case" || n.Type() == "default" {
			if p := n.Parent(); p == nil || p.Type() != "switch_section" {
				return Treesitter.DecNone
			}
		}
		// `_ => ...` is the catch-all arm of a switch expression
		if kind == Treesitter.DecCase && Treesitter.HasChildOfType(n, "discard") {
			return Treesitter.DecDefault
		}
	case Treesitter.DecNone:
		if Treesitter.IsElseBranch(n, "if_statement") {
			return Treesitter.DecElse
		}
	}
	return kind
}

func (a *TreeSitterAdapter) LogicalOperator(n *sitter.Node) string {
	return csDecisions.LogicalOperator(n, a.src)
}

// Imports returns the dependencies written on a node. A using directive
// names a namespace, or a type under an alias. Every other reference to a
// type is returned with an empty Module: C# writes types by their simple
// name, which the analysis resolves through the usings of the file, its own
// namespace and the enclosing ones, the way the compiler does. The visitor
// calls this on every node it visits and files the result under the class and
// the method the node sits in.
func (a *TreeSitterAdapter) Imports(n *sitter.Node) []Treesitter.ImportItem {
	if n == nil {
		return nil
	}
	if n.Type() != "using_directive" {
		return a.typeReferences(n)
	}
	// using System;                       -> {Module: "System"}
	// using static System.Math;          -> {Module: "System.Math"}
	// global using System.Linq;          -> {Module: "System.Linq"}
	// using Foo = System.Text.Builder;   -> {Module: "System.Text.Builder", Name: "Foo"}
	alias := ""
	module := ""
	if nm := n.ChildByFieldName("name"); nm != nil {
		// alias form: the name field is the alias identifier
		alias = text(a.src, nm)
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		switch ch.Type() {
		case "qualified_name":
			module = text(a.src, ch)
		case "identifier":
			// plain single-segment using; for the alias form the alias is the
			// name field, the target is the qualified_name/identifier after "="
			if alias != "" && text(a.src, ch) == alias && module == "" {
				// this child IS the alias; skip, the target comes after "="
				continue
			}
			module = text(a.src, ch)
		}
	}
	if module == "" {
		return nil
	}
	return []Treesitter.ImportItem{{Module: module, Name: alias}}
}

// typeReferences returns the types a node refers to, by the name written in
// the source: a simple name with an empty Module, a qualified name split into
// its namespace and its type.
//
// The visitor walks the body of a type and of a method, not their
// declaration: what is written on the declaration (base list, attributes,
// parameter and return types) is read when the visitor lands on it, what is
// written in a body when it lands on each declaration or expression carrying
// a type.
func (a *TreeSitterAdapter) typeReferences(n *sitter.Node) []Treesitter.ImportItem {
	items := []Treesitter.ImportItem{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if idx := strings.LastIndex(name, "."); idx >= 0 {
			items = append(items, Treesitter.ImportItem{Module: name[:idx], Name: name[idx+1:]})
			return
		}
		items = append(items, Treesitter.ImportItem{Module: "", Name: name})
	}
	// typesUnder collects the types written under a type node: through
	// nullable, array and tuple wrappers, and the arguments of a generic.
	var typesUnder func(x *sitter.Node)
	typesUnder = func(x *sitter.Node) {
		if x == nil {
			return
		}
		switch x.Type() {
		case "identifier":
			add(text(a.src, x))
			return
		case "qualified_name":
			add(text(a.src, x))
			return
		case "generic_name":
			// the generic itself, then its arguments
			for i := 0; i < int(x.NamedChildCount()); i++ {
				ch := x.NamedChild(i)
				if ch.Type() == "identifier" {
					add(text(a.src, ch))
				} else {
					typesUnder(ch)
				}
			}
			return
		case "predefined_type", "implicit_type", "var", "pointer_type", "function_pointer_type":
			return
		}
		for i := 0; i < int(x.NamedChildCount()); i++ {
			typesUnder(x.NamedChild(i))
		}
	}
	attributes := func(x *sitter.Node) {
		var walk func(y *sitter.Node)
		walk = func(y *sitter.Node) {
			if y == nil {
				return
			}
			if y.Type() == "attribute" {
				if name := y.ChildByFieldName("name"); name != nil {
					add(text(a.src, name))
				}
				return
			}
			for i := 0; i < int(y.NamedChildCount()); i++ {
				walk(y.NamedChild(i))
			}
		}
		walk(x)
	}
	switch n.Type() {
	case "class_declaration", "struct_declaration", "record_declaration", "record_struct_declaration", "enum_declaration", "interface_declaration":
		for i := 0; i < int(n.NamedChildCount()); i++ {
			ch := n.NamedChild(i)
			switch ch.Type() {
			case "base_list", "parameter_list", "type_parameter_constraints_clause":
				typesUnder(ch)
			case "attribute_list":
				attributes(ch)
			}
		}
	case "method_declaration", "constructor_declaration", "local_function_statement", "destructor_declaration",
		"operator_declaration", "conversion_operator_declaration", "delegate_declaration", "indexer_declaration":
		for i := 0; i < int(n.NamedChildCount()); i++ {
			ch := n.NamedChild(i)
			switch ch.Type() {
			case "block", "arrow_expression_clause", "identifier", "modifier", "type_parameter_list":
				continue
			case "attribute_list":
				attributes(ch)
			default:
				typesUnder(ch)
			}
		}
	case "variable_declaration", "parameter", "object_creation_expression", "cast_expression", "declaration_pattern",
		"catch_declaration", "property_declaration", "event_declaration", "event_field_declaration", "typeof_expression",
		"default_expression", "array_creation_expression", "stackalloc_expression", "recursive_pattern", "type_pattern",
		"as_expression", "is_expression", "sizeof_expression":
		if t := n.ChildByFieldName("type"); t != nil {
			typesUnder(t)
		}
		if n.Type() == "property_declaration" || n.Type() == "event_field_declaration" {
			for i := 0; i < int(n.NamedChildCount()); i++ {
				if ch := n.NamedChild(i); ch.Type() == "attribute_list" {
					attributes(ch)
				}
			}
		}
	case "member_access_expression":
		// Money.Zero, OrderStatus.Pending: a capitalised receiver that is
		// not a variable is a type by convention
		if object := n.ChildByFieldName("expression"); object != nil && object.Type() == "identifier" {
			if name := text(a.src, object); looksLikeCSharpType(name) {
				add(name)
			}
		}
	}
	if len(items) == 0 {
		return nil
	}
	return items
}

// looksLikeCSharpType tells whether a simple name is written like a type:
// capitalised, and not a constant written all in capitals.
func looksLikeCSharpType(name string) bool {
	return name != "" && name[0] >= 'A' && name[0] <= 'Z' && strings.ToUpper(name) != name
}

// CountComments counts C# comment lines (//, /// and /* */) in the given range.
// CommentMarkers declares C# comment tokens: "//" and "/* */" only.
// "#" introduces preprocessor directives (#region, #if), which are code, not comments.
// csharpStatements maps the C# grammar onto the shared logical-lines model.
//
// `switch_section` holds the branches of a switch and is a label, not an
// instruction. `field_declaration`, `property_declaration`,
// `method_declaration`, `using_directive` and `namespace_declaration` declare
// members: a `using` directive at the top of a file declares nothing that runs,
// while a `using` block inside a method does, which is why only
// `using_statement` is listed.
var csharpStatements = &Treesitter.StatementSpec{
	Language: tsCSharp.GetLanguage,
	Statement: []string{
		"expression_statement", "local_declaration_statement",
		"if_statement",
		"for_statement", "foreach_statement", "while_statement", "do_statement",
		"switch_statement", "try_statement",
		"return_statement", "break_statement", "continue_statement",
		"throw_statement", "yield_statement", "goto_statement",
		"labeled_statement", "lock_statement", "using_statement",
		"checked_statement", "unsafe_statement", "fixed_statement",
		"local_function_statement",
	},
}

func (a *TreeSitterAdapter) Statement(n *sitter.Node) Treesitter.StatementKind {
	return csharpStatements.Classify(n)
}

// CommentSyntax declares C# comment tokens: "//" (and the "///" doc form, which
// starts with it) and "/* */". "#" introduces a preprocessor directive, which is
// code, not a comment.
func (a *TreeSitterAdapter) CommentSyntax() engine.CommentSyntax {
	return engine.CommentSyntax{
		Line:       []string{"//"},
		BlockOpen:  "/*",
		BlockClose: "*/",
		Quote:      []rune{'"', '\''},
		// a raw string literal holds a value over several lines
		RawString: []string{`"""`},
	}
}

// csharpOperatorTokens lists the anonymous token types counted as Halstead
// operators: arithmetic, comparison, logical, bitwise, assignments, the member
// access, the argument separator, the subscript, the ternary and the keywords
// that drive the control flow. Keywords count as operators: without them, a
// body made of plain statements ("return this.items;") would hold none at all,
// and its Halstead volume would collapse to zero.
//
// The ternary reports its "?" only: its ":" would count the same operator
// twice, and a "case" label already reports its keyword.
var csharpOperatorTokens = map[string]bool{
	"+": true, "-": true, "*": true, "/": true, "%": true,
	"==": true, "!=": true, "<": true, ">": true, "<=": true, ">=": true,
	"&&": true, "||": true, "!": true, "??": true,
	"&": true, "|": true, "^": true, "~": true,
	"<<": true, ">>": true, ">>>": true,
	"=": true, "+=": true, "-=": true, "*=": true, "/=": true, "%=": true,
	"&=": true, "|=": true, "^=": true, "<<=": true, ">>=": true, ">>>=": true,
	"??=": true, "++": true, "--": true, "=>": true,
	".": true, "?.": true, "->": true, "::": true, ",": true, "[": true, "?": true,
	"return": true, "if": true, "else": true, "for": true, "foreach": true,
	"in": true, "while": true, "do": true, "switch": true, "case": true,
	"default": true, "break": true, "continue": true, "goto": true,
	"new": true, "is": true, "as": true, "sizeof": true, "nameof": true,
	"throw": true, "try": true, "catch": true, "finally": true,
	"lock": true, "await": true, "yield": true, "checked": true, "unchecked": true,
}

// csharpOperandTypes lists the named node types counted as Halstead operands.
// Literals are left out on purpose: the cohesion metrics read the operands,
// and two methods sharing the literal 0 are not cohesive.
var csharpOperandTypes = map[string]bool{"identifier": true}

// csharpCallTypes lists the node types counted as one call operator. Object
// creation is left out: it already reports its "new" keyword.
var csharpCallTypes = map[string]bool{"invocation_expression": true}

// csharpPruneTypes lists the node types never walked: a type is not an
// operand, and two methods returning a "string" are not cohesive. A qualified
// name only appears in a type or namespace position; member access has a node
// of its own.
var csharpPruneTypes = map[string]bool{
	"predefined_type": true, "implicit_type": true, "generic_name": true,
	"type_argument_list": true, "type_parameter_list": true,
	"array_type": true, "array_rank_specifier": true, "nullable_type": true,
	"pointer_type": true, "ref_type": true, "tuple_type": true,
	"qualified_name": true, "base_list": true, "attribute_list": true,
	"modifier": true, "type_parameter_constraints_clause": true,
}

// csharpChainTypes lists the member access node types. A C# method call has a
// node of its own ("invocation_expression") and is not a member access.
var csharpChainTypes = map[string]bool{"member_access_expression": true}

var csharpOperandSpec = Treesitter.OperandSpec{
	OperatorTokens: csharpOperatorTokens,
	OperandTypes:   csharpOperandTypes,
	CallTypes:      csharpCallTypes,
	PruneTypes:     csharpPruneTypes,
	ChainTypes:     csharpChainTypes,
	// no Receiver: the current object is the keyword "this"
}

// ExtractOperatorsOperands collects Halstead operators and operands from the
// AST within the given 1-based inclusive line range. A member access is a
// single operand ("this.items", "System.Console"), and an access through
// "this" reads the member whatever is done with it afterwards.
func (a *TreeSitterAdapter) ExtractOperatorsOperands(src []byte, startLine, endLine int) ([]string, []string) {
	root, source := a.ensureRoot(src)
	if root == nil {
		return nil, nil
	}
	return csharpOperandSpec.Extract(root, source, startLine, endLine)
}

// ExtractMethodCalls extracts method calls like this.Foo, base.Bar from C# source.
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
		ln = stripCSharpStrings(ln)
		if idx := strings.Index(ln, "//"); idx >= 0 {
			ln = ln[:idx]
		}
		for _, prefix := range []string{"this.", "base."} {
			rest := ln
			for {
				idx := strings.Index(rest, prefix)
				if idx < 0 {
					break
				}
				after := rest[idx+len(prefix):]
				end := 0
				for end < len(after) && (after[end] == '_' || (after[end] >= 'a' && after[end] <= 'z') || (after[end] >= 'A' && after[end] <= 'Z') || (after[end] >= '0' && after[end] <= '9')) {
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

// stripCSharpStrings removes content inside string and char literals to avoid
// false positives in comment/operator scanning.
func stripCSharpStrings(s string) string {
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
