package cpp

import (
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
	// A class_specifier/struct_specifier without a field_declaration_list is a
	// forward declaration (`class Engine;`): it declares no members, and
	// counting it would shadow the real definition with an empty class.
	return n != nil &&
		(n.Type() == "class_specifier" || n.Type() == "struct_specifier") &&
		firstChildOfType(n, "field_declaration_list") != nil
}

// IsFunction only accepts definitions with a real `{}` body.
//
// tree-sitter-cpp also parses several member declarations as
// function_definition: a field with an initializer (`int n_ = 0;` becomes a
// function_definition with a pure_virtual_clause), and defaulted, deleted and
// pure virtual members (`= default`, `= delete`, `= 0`). None of them carry a
// compound_statement body, so requiring one rejects them all. Regions opened
// by an unexpanded macro (`FMT_BEGIN_NAMESPACE ...`) do carry a body but parse
// with ERROR nodes and keyword names, so they are rejected too. Finally, the
// body statement of a Catch2 macro (`TEST_CASE("...") { ... }`) parses as a
// stray compound_statement following the macro call, and is claimed as a test
// function.
func (a *TreeSitterAdapter) IsFunction(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	switch n.Type() {
	case "function_definition":
		body := n.ChildByFieldName("body")
		if body == nil || body.Type() != "compound_statement" {
			return false
		}
		if a.hasErrorDescendant(n) || isCppKeyword(a.declaratorName(n.ChildByFieldName("declarator"))) {
			return false
		}
		return true
	case "compound_statement":
		return a.testMacroBodyName(n) != ""
	}
	return false
}

// cppKeywords are tokens that can end up as a "name" when the parser recovers
// from an unexpanded macro: `FMT_BEGIN_NAMESPACE` wrapping a namespace block
// produces a function_definition whose declarator degenerates to the
// `namespace` keyword. No real function carries one of these names.
var cppKeywords = map[string]bool{
	"class": true, "struct": true, "union": true, "enum": true, "namespace": true,
	"template": true, "typedef": true, "using": true, "typename": true, "concept": true,
	"requires": true, "export": true, "constexpr": true, "consteval": true, "static": true,
	"inline": true, "virtual": true, "explicit": true, "friend": true, "public": true,
	"private": true, "protected": true, "operator": true, "this": true, "void": true,
	"auto": true, "if": true, "for": true, "while": true, "switch": true, "return": true,
}

func isCppKeyword(name string) bool { return cppKeywords[strings.TrimSpace(name)] }

// hasErrorDescendant reports whether the parse of the subtree rooted at n
// recovered from a syntax error. A definition that parses cleanly in the
// supported C++ subset holds no ERROR node; macro fallout does.
func (a *TreeSitterAdapter) hasErrorDescendant(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	if n.IsError() || n.IsMissing() {
		return true
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if a.hasErrorDescendant(n.Child(i)) {
			return true
		}
	}
	return false
}

// testMacros are unit-test frameworks whose test cases look like function
// calls followed by a body, and are named from their arguments rather than
// from the macro itself.
var testMacros = map[string]bool{"TEST": true, "TEST_F": true, "TYPED_TEST": true, "TEST_CASE": true, "SCENARIO": true}

func (a *TreeSitterAdapter) NodeName(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	if a.IsClass(n) {
		return a.declaratorName(n.ChildByFieldName("name"))
	}
	if n.Type() == "compound_statement" {
		return a.testMacroBodyName(n)
	}
	if a.IsFunction(n) {
		if name := a.macroTestName(n); name != "" {
			return name
		}
		return a.declaratorName(n.ChildByFieldName("declarator"))
	}
	return a.declaratorName(n.ChildByFieldName("name"))
}

// NodeQualifiedName computes the short and qualified name of a class or
// function from its declaration site, prefixing the chain of enclosing
// namespace blocks. Out-of-class definitions keep the qualification as
// written (`void a::b::Foo::bar() {}` is `a::b::Foo::bar`) even when the
// class lives in another file, and method definitions declared inside a class
// follow the qualification of that class.
func (a *TreeSitterAdapter) NodeQualifiedName(n *sitter.Node) (string, string) {
	if n == nil {
		return "", ""
	}
	if a.IsClass(n) {
		short := a.declaratorName(n.ChildByFieldName("name"))
		return short, a.qualify(a.namespaceChain(n), short)
	}
	if n.Type() == "compound_statement" {
		short := a.testMacroBodyName(n)
		return short, a.qualify(a.namespaceChain(n), short)
	}
	if a.IsFunction(n) {
		if name := a.macroTestName(n); name != "" {
			return name, a.qualify(a.namespaceChain(n), name)
		}
		if qualified := a.qualifiedDefinitionName(n); qualified != "" {
			segments := strings.Split(qualified, "::")
			return segments[len(segments)-1], qualified
		}
		short := a.declaratorName(n.ChildByFieldName("declarator"))
		if class := a.enclosingClass(n); class != nil {
			_, classQualified := a.NodeQualifiedName(class)
			return short, a.AttachQualified(classQualified, short)
		}
		return short, a.qualify(a.namespaceChain(n), short)
	}
	name := a.declaratorName(n.ChildByFieldName("name"))
	return name, name
}

func (a *TreeSitterAdapter) qualify(namespace, name string) string {
	if namespace == "" || name == "" {
		return name
	}
	return namespace + "::" + name
}

// namespaceChain returns the "::"-joined names of the namespace_definition
// blocks enclosing n, outermost first. Nested blocks and the C++17
// `namespace a::b` form both flatten to their full chain. The walk stops at
// real function boundaries — but continues through the pseudo definitions
// produced by an unexpanded macro, whose namespace is still meaningful.
func (a *TreeSitterAdapter) namespaceChain(n *sitter.Node) string {
	var segments []string
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Type() == "function_definition" && a.IsFunction(p) {
			break
		}
		if p.Type() == "namespace_definition" {
			if name := p.ChildByFieldName("name"); name != nil {
				segments = append([]string{nodeText(a.src, name)}, segments...)
			}
		}
	}
	return strings.Join(segments, "::")
}

// enclosingClass returns the nearest class definition containing n, if any.
func (a *TreeSitterAdapter) enclosingClass(n *sitter.Node) *sitter.Node {
	for p := n.Parent(); p != nil; p = p.Parent() {
		if p.Type() == "function_definition" {
			return nil
		}
		if a.IsClass(p) {
			return p
		}
	}
	return nil
}

// qualifiedDefinitionName returns the qualified name as written in the
// declarator of an out-of-class definition (`a::b::Foo::bar`), or "" for a
// plain unqualified definition.
func (a *TreeSitterAdapter) qualifiedDefinitionName(fn *sitter.Node) string {
	d := fn.ChildByFieldName("declarator")
	if d == nil {
		return ""
	}
	if d.Type() == "function_declarator" {
		d = d.ChildByFieldName("declarator")
	}
	if d == nil || d.Type() != "qualified_identifier" {
		return ""
	}
	name := nodeText(a.src, d)
	name = strings.TrimPrefix(name, "::")
	if !strings.Contains(name, "::") || isCppKeyword(strings.TrimSuffix(name, "()")) {
		return ""
	}
	return name
}

// macroTestName names gtest-style test macros from their arguments:
// TEST(Suite, Name) becomes "Suite.Name". Returns "" when the function is not
// one of the known test macros.
func (a *TreeSitterAdapter) macroTestName(fn *sitter.Node) string {
	declarator := fn.ChildByFieldName("declarator")
	if declarator == nil {
		return ""
	}
	identifier := declarator
	if identifier.Type() == "function_declarator" {
		if inner := identifier.ChildByFieldName("declarator"); inner != nil {
			identifier = inner
		}
	}
	if identifier.Type() != "identifier" || !testMacros[nodeText(a.src, identifier)] {
		return ""
	}
	args := firstDescendantOfType(declarator, "parameter_list")
	if args == nil {
		return ""
	}
	var names []string
	for i := 0; i < int(args.NamedChildCount()); i++ {
		if text := nodeText(a.src, args.NamedChild(i)); text != "" {
			names = append(names, text)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return strings.Join(names, ".")
}

// testMacroBodyName recognizes the body of a Catch2-style test macro: a
// compound_statement whose previous sibling is a call to TEST_CASE or
// SCENARIO. The name comes from the first string argument of the call.
func (a *TreeSitterAdapter) testMacroBodyName(n *sitter.Node) string {
	parent := n.Parent()
	if parent == nil || (parent.Type() != "translation_unit" && parent.Type() != "declaration_list") {
		return ""
	}
	prev := n.PrevNamedSibling()
	if prev == nil || prev.Type() != "expression_statement" {
		return ""
	}
	call := firstDescendantOfType(prev, "call_expression")
	if call == nil {
		return ""
	}
	function := call.ChildByFieldName("function")
	if function == nil || function.Type() != "identifier" {
		return ""
	}
	macro := nodeText(a.src, function)
	if macro != "TEST_CASE" && macro != "SCENARIO" {
		return ""
	}
	if args := firstDescendantOfType(call, "argument_list"); args != nil {
		for i := 0; i < int(args.NamedChildCount()); i++ {
			if content := firstDescendantOfType(args.NamedChild(i), "string_content"); content != nil {
				if text := nodeText(a.src, content); text != "" {
					return text
				}
			}
		}
	}
	return macro
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
	case "operator_cast":
		// conversion functions carry no plain name: the type is the name.
		// The node text spans the empty parameter list ("operator bool()"),
		// which is not part of the name.
		return strings.TrimSuffix(nodeText(a.src, n), "()")
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
	if n.Type() == "compound_statement" {
		// the Catch2 macro body: the node is its own body
		return n
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
	if n.Type() == "compound_statement" {
		// the arguments of the Catch2 macro call precede the body
		if prev := n.PrevNamedSibling(); prev != nil {
			return firstDescendantOfType(prev, "argument_list")
		}
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
		if p.Type() != "parameter_declaration" && p.Type() != "optional_parameter_declaration" &&
			p.Type() != "variadic_parameter_declaration" {
			continue
		}
		if name := a.declaratorName(p.ChildByFieldName("declarator")); name != "" {
			yield(name)
		}
	}
}

// ModuleNameFromPath returns "": a C++ translation unit holds no single
// namespace. Classes and functions carry their own namespace qualification,
// computed per declaration in NodeQualifiedName.
func (a *TreeSitterAdapter) ModuleNameFromPath(path string) string {
	return ""
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
	if n == nil {
		return nil
	}
	if a.IsClass(n) {
		return a.classDependencies(n)
	}
	if n.Type() != "preproc_include" {
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

// classDependencies maps syntax-level type references in a class to the
// common external-dependency representation. It covers bases, fields, method
// signatures, construction and qualified static use without attempting C++
// name lookup or overload resolution.
//
// References written unqualified are resolved against the namespace of the
// class being analyzed, so `formatter` used inside `namespace fmt { ... }`
// points at `fmt::formatter`. Known noise is skipped: the parameters of the
// enclosing templates, anything rooted in `std`, template arguments and
// built-in types.
func (a *TreeSitterAdapter) classDependencies(class *sitter.Node) []Treesitter.ImportItem {
	_, selfQualified := a.NodeQualifiedName(class)
	self := a.declaratorName(class.ChildByFieldName("name"))
	namespace := a.namespaceChain(class)
	templateParameters := a.templateParameterNames(class)
	seen := map[string]bool{}
	var items []Treesitter.ImportItem
	add := func(name string) {
		name = strings.TrimSpace(name)
		name = strings.TrimPrefix(name, "::")
		if name == "" || name == self || name == selfQualified || isBuiltinCppType(name) ||
			name == "std" || strings.HasPrefix(name, "std::") || templateParameters[name] ||
			isCppMacroName(name) || seen[name] {
			return
		}
		// Resolve an unqualified reference against the namespace of the class:
		// the common case is a type of the same namespace as the class using it.
		if !strings.Contains(name, "::") && namespace != "" {
			name = namespace + "::" + name
		}
		if name == selfQualified || seen[name] {
			return
		}
		seen[name] = true
		short := name
		if idx := strings.LastIndex(short, "::"); idx >= 0 {
			short = short[idx+2:]
		}
		items = append(items, Treesitter.ImportItem{Module: name, Name: short})
	}

	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}
		// A nested class owns its own dependencies.
		if node != class && a.IsClass(node) {
			return
		}
		switch node.Type() {
		case "type_identifier":
			parent := node.Parent()
			if parent == nil || (parent.Type() != "qualified_identifier" && parent.Type() != "template_type") {
				add(nodeText(a.src, node))
			}
		case "template_type":
			// a template instantiation: the dependency is the template name,
			// without its arguments (`ns::Mixin<X>` is `ns::Mixin`)
			if parent := node.Parent(); parent == nil || parent.Type() != "qualified_identifier" {
				if name := node.ChildByFieldName("name"); name != nil {
					add(nodeText(a.src, name))
				}
			}
		case "qualified_identifier":
			// In a type position this is the full dependency (ns::Type). In
			// an expression such as Logger::instance(), the scope is the type.
			if node.Parent() != nil && isCppTypePosition(node.Parent().Type()) {
				add(a.qualifiedTypeName(node))
			} else if scope := node.ChildByFieldName("scope"); scope != nil {
				add(a.referenceSegment(scope))
			}
		}
		for i := 0; i < int(node.NamedChildCount()); i++ {
			walk(node.NamedChild(i))
		}
	}
	walk(class)
	return items
}

// qualifiedTypeName renders the name of a qualified reference without its
// template arguments: `std::vector<int>` is `std::vector`, and
// `a::ns::Mixin<X>` is `a::ns::Mixin`.
func (a *TreeSitterAdapter) qualifiedTypeName(q *sitter.Node) string {
	var parts []string
	if scope := q.ChildByFieldName("scope"); scope != nil {
		parts = append(parts, a.referenceSegment(scope))
	}
	if name := q.ChildByFieldName("name"); name != nil {
		parts = append(parts, a.referenceSegment(name))
	}
	return strings.Join(parts, "::")
}

func (a *TreeSitterAdapter) referenceSegment(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "template_type":
		if name := n.ChildByFieldName("name"); name != nil {
			return nodeText(a.src, name)
		}
		return ""
	case "qualified_identifier":
		return a.qualifiedTypeName(n)
	}
	return nodeText(a.src, n)
}

// templateParameterNames collects the names introduced by the template
// declarations enclosing the class. Those names are parameters, not types: a
// reference to `T` inside `template <typename T> class Foo` is not a
// dependency of Foo.
func (a *TreeSitterAdapter) templateParameterNames(class *sitter.Node) map[string]bool {
	names := map[string]bool{}
	for p := class.Parent(); p != nil; p = p.Parent() {
		if p.Type() == "function_definition" {
			break
		}
		if p.Type() == "template_declaration" {
			if params := p.ChildByFieldName("parameters"); params != nil {
				collectIdentifierTexts(a.src, params, names)
			}
		}
	}
	return names
}

func collectIdentifierTexts(src []byte, n *sitter.Node, names map[string]bool) {
	if n == nil {
		return
	}
	switch n.Type() {
	case "identifier", "type_identifier":
		names[nodeText(src, n)] = true
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		collectIdentifierTexts(src, n.NamedChild(i), names)
	}
}

func isCppTypePosition(nodeType string) bool {
	switch nodeType {
	case "base_class_clause", "field_declaration", "parameter_declaration", "optional_parameter_declaration",
		"function_definition", "declaration", "new_expression", "type_descriptor", "template_argument_list":
		return true
	}
	return false
}

func isBuiltinCppType(name string) bool {
	switch name {
	case "void", "bool", "char", "char8_t", "char16_t", "char32_t", "wchar_t",
		"short", "int", "long", "float", "double", "signed", "unsigned", "auto", "decltype",
		"size_t", "ssize_t", "ptrdiff_t", "nullptr_t":
		return true
	}
	return strings.HasPrefix(name, "int") && strings.HasSuffix(name, "_t") ||
		strings.HasPrefix(name, "uint") && strings.HasSuffix(name, "_t")
}

// isCppMacroName reports whether the name follows the C++ macro convention
// (SCREAMING_CASE). Uppercase identifiers in type position are unexpanded
// macros (`FMT_CONSTEXPR auto format(...)`), not types.
func isCppMacroName(name string) bool {
	if idx := strings.LastIndex(name, "::"); idx >= 0 {
		name = name[idx+2:]
	}
	if len(name) < 2 {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
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

// ClassDirectOperands lists the data members declared directly in the class
// body. Method declarations (`void open();`) are not attributes, and a
// multi-declarator field (`int a, b, *c;`) contributes each of its names.
func (a *TreeSitterAdapter) ClassDirectOperands(n *sitter.Node) []string {
	body := a.NodeBody(n)
	if body == nil {
		return nil
	}
	var result []string
	for i := 0; i < int(body.NamedChildCount()); i++ {
		field := body.NamedChild(i)
		switch field.Type() {
		case "field_declaration":
			for j := 0; j < int(field.ChildCount()); j++ {
				if field.FieldNameForChild(j) != "declarator" {
					continue
				}
				declarator := field.Child(j)
				if declarator == nil {
					continue
				}
				// a declarator with parameters declares a method, not a field
				if firstDescendantOfType(declarator, "function_declarator") != nil {
					continue
				}
				if name := a.declaratorName(declarator); name != "" && !isCppKeyword(name) {
					result = append(result, name)
				}
			}
		case "function_definition":
			// the grammar parses a field with an `=` initializer as a
			// bodyless function_definition whose declarator is the bare
			// field identifier (`int n_ = 0;`): it is a field
			declarator := field.ChildByFieldName("declarator")
			if declarator == nil || declarator.Type() != "field_identifier" {
				continue
			}
			if name := a.declaratorName(declarator); name != "" {
				result = append(result, name)
			}
		}
	}
	return result
}

// ReceiverTypeName binds an out-of-class definition (C::f) back to C when the
// class is declared in the same translation unit. It returns the short name
// of the class, so the shared receiver binding can match it against the
// classes of the file. When the class lives in another file the definition
// keeps the qualification as written (Ns::Class::method).
func (a *TreeSitterAdapter) ReceiverTypeName(n *sitter.Node) string {
	qualified := a.qualifiedDefinitionName(n)
	if qualified == "" {
		return ""
	}
	segments := strings.Split(qualified, "::")
	if len(segments) < 2 {
		return ""
	}
	receiver := segments[len(segments)-2]
	if isCppKeyword(receiver) {
		return ""
	}
	return receiver
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
