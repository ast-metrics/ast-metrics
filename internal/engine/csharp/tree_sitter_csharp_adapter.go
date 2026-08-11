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

func (a *TreeSitterAdapter) IsModule(n *sitter.Node) bool {
	// namespace_declaration bodies are reached through the fallback recursion;
	// the namespace name itself comes from ModuleNameFromPath
	return n.Type() == "compilation_unit"
}

func (a *TreeSitterAdapter) IsClass(n *sitter.Node) bool {
	switch n.Type() {
	// structs, records (incl. record struct) and enums are class-like
	// containers in C#: they hold fields and methods, so treat them as
	// classes for metrics.
	case "class_declaration", "struct_declaration", "record_declaration", "enum_declaration":
		return true
	}
	return false
}

func (a *TreeSitterAdapter) IsInterface(n *sitter.Node) bool {
	return n.Type() == "interface_declaration"
}

func (a *TreeSitterAdapter) IsFunction(n *sitter.Node) bool {
	// Property accessors (get/set/init) and lambdas are intentionally not
	// treated as named functions; their bodies still contribute decisions via
	// the fallback recursion.
	switch n.Type() {
	case "method_declaration", "constructor_declaration", "local_function_statement", "destructor_declaration":
		return true
	}
	return false
}

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
	If:      []string{"if_statement"},
	Loop:    []string{"for_statement", "foreach_statement", "while_statement", "do_statement"},
	Switch:  []string{"switch_statement", "switch_expression"},
	Case:    []string{"case", "switch_expression_arm"},
	Default: []string{"default"},
	Catch:   []string{"catch_clause"},
	Ternary: []string{"conditional_expression"},
	Logical: []string{"binary_expression"},
	Ops:     []string{"&&", "||"},
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

func (a *TreeSitterAdapter) Imports(n *sitter.Node) []Treesitter.ImportItem {
	if n == nil || n.Type() != "using_directive" {
		return nil
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


// CountComments counts C# comment lines (//, /// and /* */) in the given range.
// CommentMarkers declares C# comment tokens: "//" and "/* */" only.
// "#" introduces preprocessor directives (#region, #if), which are code, not comments.
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
		clean := stripCSharpStrings(ln)
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
	lines := strings.Split(string(src), "\n")
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
