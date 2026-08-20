package php

import (
	"bytes"
	"strings"
	"unicode/utf8"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	Treesitter "github.com/ast-metrics/ast-metrics/internal/engine/treesitter"
	sitter "github.com/smacker/go-tree-sitter"
	tsPhp "github.com/smacker/go-tree-sitter/php"
)

type TreeSitterAdapter struct {
	src      []byte
	root     *sitter.Node // shared root from runner to avoid re-parsing
	ns       string
	aliases  map[string]string
	computed bool
	// srcLines holds the source split into lines, so that the extractors called
	// once per function do not split the whole file again for each of them
	srcLines Treesitter.LineCache
	// pipes holds the lines carrying a mis-parsed "|>", found once per file
	pipes      []int
	pipesFound bool
}

func NewTreeSitterAdapter(src []byte) *TreeSitterAdapter { return &TreeSitterAdapter{src: src} }
func (a *TreeSitterAdapter) SetSource(src []byte) {
	a.src = src
	a.root = nil
	a.computed = false
	a.ns = ""
	a.pipes = nil
	a.pipesFound = false
}
func (a *TreeSitterAdapter) SetRootNode(root *sitter.Node) { a.root = root }

func (a *TreeSitterAdapter) Language() *sitter.Language { return tsPhp.GetLanguage() }

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

// ---- Structure detection ----
//
// These four questions are asked on every node of every walk, so they are
// answered by symbol id rather than by node type name. See
// internal/engine/treesitter/symbols.go.
var (
	phpModules    = &Treesitter.TypeSet{Language: tsPhp.GetLanguage, Types: []string{"program"}}
	phpClasses    = &Treesitter.TypeSet{Language: tsPhp.GetLanguage, Types: []string{"class_declaration", "trait_declaration", "enum_declaration"}}
	phpInterfaces = &Treesitter.TypeSet{Language: tsPhp.GetLanguage, Types: []string{"interface_declaration"}}
	phpFunctions  = &Treesitter.TypeSet{Language: tsPhp.GetLanguage, Types: []string{"function_definition", "method_declaration"}}
)

func (a *TreeSitterAdapter) IsModule(n *sitter.Node) bool { return phpModules.Has(n) }

func (a *TreeSitterAdapter) IsClass(n *sitter.Node) bool { return phpClasses.Has(n) }

// Optional interface awareness for Visitor
func (a *TreeSitterAdapter) IsInterface(n *sitter.Node) bool { return phpInterfaces.Has(n) }

func (a *TreeSitterAdapter) IsFunction(n *sitter.Node) bool { return phpFunctions.Has(n) }

// ---- Attributes ----
func (a *TreeSitterAdapter) NodeName(n *sitter.Node) string {
	if a.src == nil || n == nil {
		return ""
	}
	var s string
	if name := n.ChildByFieldName("name"); name != nil {
		s = a.text(name)
	} else if id := firstChildOfType(n, "name"); id != nil { // some tokens are wrapped in name
		s = a.text(id)
	} else if id := firstChildOfType(n, "identifier"); id != nil {
		s = a.text(id)
	}
	if s == "" {
		return "@non-utf8"
	}
	// if contains non-utf8 bytes, normalize name to @non-utf8 to keep legacy behavior
	if !utf8.ValidString(s) {
		return "@non-utf8"
	}
	return s
}

func (a *TreeSitterAdapter) NodeBody(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	if b := n.ChildByFieldName("body"); b != nil { // method/class bodies
		return b
	}
	// common bodies
	if b := firstChildOfType(n, "compound_statement"); b != nil {
		return b
	}
	if b := firstChildOfType(n, "declaration_list"); b != nil {
		return b
	}
	return nil
}

func (a *TreeSitterAdapter) NodeParams(n *sitter.Node) *sitter.Node {
	if n == nil {
		return nil
	}
	if p := n.ChildByFieldName("parameters"); p != nil { // function_definition
		return p
	}
	if p := firstChildOfType(n, "parameters"); p != nil {
		return p
	}
	if p := firstChildOfType(n, "parameter_list"); p != nil {
		return p
	}
	return nil
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
		// PHP parameter var names appear as variable_name → name token "$x"
		if x.Type() == "variable_name" || x.Type() == "name" || x.Type() == "variable" {
			yield(a.text(x))
		}
		for i := 0; i < int(x.ChildCount()); i++ {
			walk(x.Child(i))
		}
	}
	walk(params)
}

// ---- Namespace/module helpers ----
func (a *TreeSitterAdapter) ModuleNameFromPath(path string) string {
	// For PHP we try to return the declared namespace if present; otherwise no module
	if ns := a.findNamespace(); ns != "" {
		return ns
	}
	return ""
}

func (a *TreeSitterAdapter) AttachQualified(parentClass, fn string) string {
	if parentClass == "" {
		return fn
	}
	return parentClass + "::" + fn
}

func (a *TreeSitterAdapter) EachChildBody(body *sitter.Node, yield func(*sitter.Node)) {
	if body == nil {
		return
	}
	for i := 0; i < int(body.ChildCount()); i++ {
		yield(body.Child(i))
	}
}

// ---- Decisions & Loops ----

// phpDecisions maps the PHP grammar onto the shared complexity model.
//
// PHP writes `else if` as an else_clause holding an if_statement, and `elseif`
// as a dedicated else_if_clause. Only the second is an Elif here: in the first
// form the nested if_statement is itself counted, so counting the else_clause
// too would count the same branch twice.
//
// `?:` and `??` are both conditional_expression / binary_expression, but only
// the ternary is a branch of the control flow; `??` is left out to stay aligned
// with lizard, phploc and PMD, and with the languages that have no such
// operator.
var phpDecisions = &Treesitter.DecisionSpec{
	Language: tsPhp.GetLanguage,
	If:       []string{"if_statement"},
	Elif:     []string{"else_if_clause"},
	Else:     []string{"else_clause"},
	Loop:     []string{"while_statement", "for_statement", "foreach_statement", "do_statement"},
	Switch:   []string{"switch_statement", "match_expression"},
	Case:     []string{"case_statement", "match_conditional_expression"},
	Default:  []string{"default_statement", "match_default_expression"},
	Catch:    []string{"catch_clause"},
	Ternary:  []string{"conditional_expression"},
	Logical:  []string{"binary_expression"},
	Ops:      []string{"&&", "||", "and", "or"},
}

func (a *TreeSitterAdapter) Decision(n *sitter.Node) Treesitter.DecisionKind {
	return phpDecisions.Classify(n, a.src)
}

func (a *TreeSitterAdapter) LogicalOperator(n *sitter.Node) string {
	return phpDecisions.LogicalOperator(n, a.src)
}

// ---- Dependencies ----
//
// The dependencies of a PHP file are read off the tree, node by node, as the
// visitor walks it: the visitor calls Imports on every node it visits and
// attaches what comes back to the class and the function it is in. A name is
// a dependency where the language makes it one:
//
//   - what a class extends, implements or uses as a trait, and the attributes
//     on it;
//   - the type of a parameter, a property, a return value or a caught
//     exception, in every place a type may be written (methods, functions,
//     closures, arrow functions, promoted properties);
//   - the class of an object created with new, called or read statically
//     (Foo::bar(), Foo::CONST, Foo::$prop, Foo::class), or tested with
//     instanceof.
//
// Every name is resolved the way PHP resolves it: a fully qualified name as
// it is, an imported name through its import or its alias, any other name in
// the namespace of the file. self, static, parent and the primitive types are
// not dependencies. Every use counts, so that a class using another in three
// places weighs three times one that uses it once.

// phpPrimitiveTypes lists the names written where a class name could be that
// name no class.
var phpPrimitiveTypes = map[string]bool{
	"int": true, "integer": true, "float": true, "double": true, "string": true,
	"bool": true, "boolean": true, "array": true, "callable": true, "iterable": true,
	"void": true, "mixed": true, "object": true, "null": true, "never": true,
	"false": true, "true": true, "resource": true,
	"self": true, "static": true, "parent": true,
}

// Imports returns the dependencies written on a node, resolved to qualified
// names. Module and Name both carry the qualified name of the class: PHP has
// no package a class would be imported from.
func (a *TreeSitterAdapter) Imports(n *sitter.Node) []Treesitter.ImportItem {
	if n == nil {
		return nil
	}
	if !a.computed {
		a.collectAliases()
	}
	names := []string{}
	typeName := func(x *sitter.Node) {
		if x == nil {
			return
		}
		switch x.Type() {
		case "name", "qualified_name":
			names = append(names, a.text(x))
		}
	}
	// namesUnder collects the class names listed under a clause.
	namesUnder := func(x *sitter.Node) {
		if x == nil {
			return
		}
		for i := 0; i < int(x.NamedChildCount()); i++ {
			typeName(x.NamedChild(i))
		}
	}
	// typesUnder collects the named types under a node: a parameter list, a
	// union or an optional type, a return type, a catch type list.
	var typesUnder func(x *sitter.Node)
	typesUnder = func(x *sitter.Node) {
		if x == nil {
			return
		}
		if x.Type() == "named_type" {
			typeName(x.NamedChild(0))
			return
		}
		for i := 0; i < int(x.NamedChildCount()); i++ {
			typesUnder(x.NamedChild(i))
		}
	}
	switch n.Type() {
	case "class_declaration", "enum_declaration", "trait_declaration", "interface_declaration":
		// the declaration itself: what it extends and implements, and its
		// attributes; the body is visited on its own
		for i := 0; i < int(n.NamedChildCount()); i++ {
			ch := n.NamedChild(i)
			switch ch.Type() {
			case "base_clause", "class_interface_clause":
				namesUnder(ch)
			case "attribute_list":
				a.attributeNames(ch, &names)
			}
		}
	case "method_declaration", "function_definition":
		// the signature: parameter types, return type, attributes; the body
		// is visited on its own
		for i := 0; i < int(n.NamedChildCount()); i++ {
			ch := n.NamedChild(i)
			switch ch.Type() {
			case "compound_statement":
				continue
			case "attribute_list":
				a.attributeNames(ch, &names)
			default:
				typesUnder(ch)
			}
		}
	case "arrow_function", "anonymous_function_creation_expression":
		// the closure is a node of the body: its signature is read here, its
		// own body is visited after it
		for i := 0; i < int(n.NamedChildCount()); i++ {
			ch := n.NamedChild(i)
			switch ch.Type() {
			case "formal_parameters", "named_type", "optional_type", "union_type", "intersection_type", "disjunctive_normal_form_type":
				typesUnder(ch)
			}
		}
	case "property_declaration":
		for i := 0; i < int(n.NamedChildCount()); i++ {
			ch := n.NamedChild(i)
			switch ch.Type() {
			case "named_type", "optional_type", "union_type", "intersection_type", "disjunctive_normal_form_type":
				typesUnder(ch)
			case "attribute_list":
				a.attributeNames(ch, &names)
			}
		}
	case "catch_clause":
		if types := n.ChildByFieldName("type"); types != nil {
			typesUnder(types)
		} else {
			typesUnder(firstChildOfType(n, "type_list"))
		}
	case "object_creation_expression":
		// new Foo, new \Bar\Baz, new class extends Base {}
		if n.NamedChildCount() > 0 {
			first := n.NamedChild(0)
			// PHP 8.4 lets a method be called on a new object without
			// parentheses, new Foo($x)->bar(): the bundled grammar reads it
			// as the creation of what "Foo($x)->bar" returns, and the class
			// sits at the bottom of that chain
			for first != nil && first.NamedChildCount() > 0 {
				switch first.Type() {
				case "member_access_expression", "member_call_expression", "nullsafe_member_access_expression", "nullsafe_member_call_expression", "function_call_expression":
					first = first.NamedChild(0)
					continue
				}
				break
			}
			switch first.Type() {
			case "name", "qualified_name":
				names = append(names, a.text(first))
			case "attribute_list", "base_clause", "class_interface_clause":
				for i := 0; i < int(n.NamedChildCount()); i++ {
					ch := n.NamedChild(i)
					if ch.Type() == "base_clause" || ch.Type() == "class_interface_clause" {
						namesUnder(ch)
					}
				}
			}
		}
	case "scoped_call_expression", "class_constant_access_expression", "scoped_property_access_expression":
		// Foo::bar(), Foo::CONST, Foo::class, Foo::$prop
		if n.NamedChildCount() > 0 {
			typeName(n.NamedChild(0))
		}
	case "use_declaration":
		// inside a class body: the traits brought in. The names of the
		// conflict resolution list are not walked: they name methods.
		for i := 0; i < int(n.NamedChildCount()); i++ {
			typeName(n.NamedChild(i))
		}
	case "binary_expression":
		if a.isInstanceof(n) && n.NamedChildCount() >= 2 {
			typeName(n.NamedChild(int(n.NamedChildCount()) - 1))
		}
	}
	if len(names) == 0 {
		return nil
	}
	items := make([]Treesitter.ImportItem, 0, len(names))
	for _, name := range names {
		if class := a.resolveClassName(name); class != "" {
			items = append(items, Treesitter.ImportItem{Module: class, Name: class})
		}
	}
	return items
}

// attributeNames collects the classes named by the attributes of a list.
func (a *TreeSitterAdapter) attributeNames(list *sitter.Node, names *[]string) {
	var walk func(x *sitter.Node)
	walk = func(x *sitter.Node) {
		if x == nil {
			return
		}
		if x.Type() == "attribute" {
			if x.NamedChildCount() > 0 {
				first := x.NamedChild(0)
				if first.Type() == "name" || first.Type() == "qualified_name" {
					*names = append(*names, a.text(first))
				}
			}
			return
		}
		for i := 0; i < int(x.NamedChildCount()); i++ {
			walk(x.NamedChild(i))
		}
	}
	walk(list)
}

// isInstanceof tells whether a binary expression is an instanceof test: the
// operator is an anonymous token between the two operands.
func (a *TreeSitterAdapter) isInstanceof(n *sitter.Node) bool {
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		if !ch.IsNamed() && a.text(ch) == "instanceof" {
			return true
		}
	}
	return false
}

// resolveClassName resolves a name written in the source the way PHP does,
// and returns an empty string for a name that is no class.
func (a *TreeSitterAdapter) resolveClassName(name string) string {
	name = strings.TrimSpace(strings.TrimPrefix(name, "?"))
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "\\") {
		return strings.TrimPrefix(name, "\\")
	}
	if phpPrimitiveTypes[strings.ToLower(name)] {
		return ""
	}
	// an unqualified name imported as is, or the first segment of a
	// qualified name imported as a namespace (use App\Shared; Shared\Model)
	if full, ok := a.aliases[name]; ok {
		return full
	}
	if i := strings.Index(name, "\\"); i > 0 {
		if full, ok := a.aliases[name[:i]]; ok {
			return full + name[i:]
		}
	}
	// a few classes of the global namespace, written without a leading
	// backslash by habit: kept as they are, so that a file that forgot the
	// backslash still points at the class it means
	switch name {
	case "stdClass", "InvalidArgumentException":
		return name
	}
	if a.ns != "" {
		return a.ns + "\\" + name
	}
	return name
}

// collectAliases reads the namespace and the imports of the file: what each
// imported name stands for, with its alias when it has one. Imports of
// functions and constants are left aside: they name no class.
func (a *TreeSitterAdapter) collectAliases() {
	if a.computed {
		return
	}
	a.computed = true
	a.aliases = map[string]string{}
	if a.src == nil {
		return
	}
	root := a.root
	if root == nil {
		parser := sitter.NewParser()
		parser.SetLanguage(tsPhp.GetLanguage())
		root = parser.Parse(nil, a.src).RootNode()
	}
	// The declarations sit at the top of the file, or one level down inside
	// a braced namespace.
	var scan func(n *sitter.Node)
	scan = func(n *sitter.Node) {
		for i := 0; i < int(n.NamedChildCount()); i++ {
			ch := n.NamedChild(i)
			switch ch.Type() {
			case "namespace_definition":
				if nm := firstChildOfType(ch, "namespace_name"); nm != nil {
					a.ns = a.text(nm)
				}
				if body := firstChildOfType(ch, "compound_statement"); body != nil {
					scan(body)
				}
			case "namespace_use_declaration":
				a.collectUseDeclaration(ch)
			}
		}
	}
	scan(root)
}

// collectUseDeclaration records the imports of one use statement.
func (a *TreeSitterAdapter) collectUseDeclaration(n *sitter.Node) {
	// use function foo; use const BAR;
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		if !ch.IsNamed() {
			switch a.text(ch) {
			case "function", "const":
				return
			}
		}
	}
	record := func(full string, aliasNode *sitter.Node) {
		full = strings.TrimPrefix(strings.TrimSpace(full), "\\")
		if full == "" {
			return
		}
		alias := ""
		if aliasNode != nil {
			if nm := firstChildOfType(aliasNode, "name"); nm != nil {
				alias = a.text(nm)
			}
		}
		if alias == "" {
			alias = full
			if i := strings.LastIndex(full, "\\"); i >= 0 {
				alias = full[i+1:]
			}
		}
		a.aliases[alias] = full
	}
	prefix := ""
	for i := 0; i < int(n.NamedChildCount()); i++ {
		ch := n.NamedChild(i)
		switch ch.Type() {
		case "namespace_name":
			// use App\Shared\{...}: the prefix of a group
			prefix = a.text(ch)
		case "namespace_use_clause":
			var full string
			var aliasNode *sitter.Node
			for j := 0; j < int(ch.NamedChildCount()); j++ {
				part := ch.NamedChild(j)
				switch part.Type() {
				case "qualified_name", "name":
					full = a.text(part)
				case "namespace_aliasing_clause":
					aliasNode = part
				}
			}
			record(full, aliasNode)
		case "namespace_use_group":
			for j := 0; j < int(ch.NamedChildCount()); j++ {
				clause := ch.NamedChild(j)
				if clause.Type() != "namespace_use_group_clause" {
					continue
				}
				var full string
				var aliasNode *sitter.Node
				for k := 0; k < int(clause.NamedChildCount()); k++ {
					part := clause.NamedChild(k)
					switch part.Type() {
					case "namespace_name", "qualified_name", "name":
						full = a.text(part)
					case "namespace_aliasing_clause":
						aliasNode = part
					}
				}
				if prefix != "" {
					full = prefix + "\\" + full
				}
				record(full, aliasNode)
			}
		}
	}
}

// ---- Class operands (properties) ----
// ClassDirectOperands returns the direct attributes (properties) declared in the given class node.
// It scans only the class body and collects variable names from property declarations.
func (a *TreeSitterAdapter) ClassDirectOperands(n *sitter.Node) []string {
	if n == nil {
		return nil
	}
	body := a.NodeBody(n)
	if body == nil {
		return nil
	}
	props := []string{}
	add := func(name string) {
		if name != "" {
			props = append(props, name)
		}
	}
	var walkCollect func(*sitter.Node)
	walkCollect = func(x *sitter.Node) {
		if x == nil {
			return
		}
		t := x.Type()
		// property_declaration covers typical cases; class_property_declaration may appear in some grammar versions
		if t == "property_declaration" || t == "class_property_declaration" {
			// collect variable_name under property_element list
			var dive func(*sitter.Node)
			dive = func(y *sitter.Node) {
				if y == nil {
					return
				}
				if y.Type() == "variable_name" {
					add(a.text(y))
				}
				for i := 0; i < int(y.ChildCount()); i++ {
					dive(y.Child(i))
				}
			}
			dive(x)
			return
		}
		// Avoid deep traversal elsewhere to keep it limited to direct children property declarations
	}
	for i := 0; i < int(body.ChildCount()); i++ {
		walkCollect(body.Child(i))
	}
	// normalize: drop leading $ if present
	for i, p := range props {
		props[i] = normalizePhpOperand(p)
	}
	return props
}

// ---- helpers ----
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

// phpOperatorTokens lists the anonymous token types counted as Halstead
// operators: arithmetic, string concatenation, comparison, logical, bitwise,
// assignments, the accesses, the argument separator, the subscript, the
// ternary and the keywords that drive the control flow.
//
// Keywords count as operators. Without them, a body made of plain statements
// ("return array_keys($this->items);") holds no operator at all: the Halstead
// volume collapses to zero, and since the maintainability index decreases with
// the logarithm of the volume, it turns every such method into a perfect
// score.
//
// Declaration keywords are left out on purpose ("function", "class",
// "private", "use"): they describe the shape of the code, they do not operate
// on anything. Type keywords never reach this map either, since the type
// positions are pruned.
//
// The ":" covers the named arguments ("f(userId: 1)") and the alternative
// syntax ("if (...):"). It also lands on the ":" of a return type and on the
// second half of a ternary, which each add one occurrence of an operator the
// method already has: the effect on the volume is marginal, and worth the
// named arguments.
var phpOperatorTokens = map[string]bool{
	"+": true, "-": true, "*": true, "/": true, "%": true, "**": true, ".": true,
	"==": true, "===": true, "!=": true, "!==": true, "<>": true, "<=>": true,
	"<": true, ">": true, "<=": true, ">=": true,
	"&&": true, "||": true, "!": true, "??": true, "?": true,
	"and": true, "or": true, "xor": true,
	"&": true, "|": true, "^": true, "~": true, "<<": true, ">>": true,
	"=": true, "+=": true, "-=": true, "*=": true, "/=": true, "%=": true,
	".=": true, "**=": true, "??=": true, "&=": true, "|=": true, "^=": true,
	"<<=": true, ">>=": true,
	"++": true, "--": true, "@": true,
	"->": true, "?->": true, "::": true, "=>": true, "...": true,
	",": true, "[": true, ":": true, "|>": true,
	"return": true, "if": true, "else": true, "elseif": true, "endif": true,
	"while": true, "do": true, "for": true, "foreach": true, "as": true,
	"switch": true, "case": true, "default": true, "match": true,
	"break": true, "continue": true, "goto": true,
	"new": true, "clone": true, "instanceof": true,
	"throw": true, "try": true, "catch": true, "finally": true,
	"echo": true, "print": true, "yield": true,
	"isset": true, "unset": true, "empty": true, "list": true,
	"require": true, "require_once": true, "include": true, "include_once": true,
	"exit": true, "die": true, "global": true,
}

// phpOperandTypes lists the named node types counted as Halstead operands.
// Only variables are counted: PHP names them with a leading "$", which tells
// them apart from the function, class and constant names the grammar also
// reports as "name" nodes. Literals are left out like in the other languages:
// the cohesion metrics read the operands, and two methods sharing the literal
// 0 are not cohesive.
var phpOperandTypes = map[string]bool{"variable_name": true}

// phpCallTypes lists the node types counted as one call operator. Object
// creation is left out: it already reports its "new" keyword.
var phpCallTypes = map[string]bool{
	"function_call_expression": true, "member_call_expression": true,
	"nullsafe_member_call_expression": true, "scoped_call_expression": true,
}

// phpPruneTypes lists the node types never walked: a type is not an operand,
// and two methods typed "string" are not cohesive. Modifiers and attributes
// describe the declaration, not what it computes.
var phpPruneTypes = map[string]bool{
	"primitive_type": true, "named_type": true, "optional_type": true,
	"union_type": true, "intersection_type": true, "type_list": true,
	"visibility_modifier": true, "static_modifier": true,
	"abstract_modifier": true, "final_modifier": true, "readonly_modifier": true,
	"var_modifier": true, "attribute_list": true, "base_clause": true,
	"class_interface_clause": true,
}

// phpChainTypes lists the attribute access node types. A PHP method call has
// a node of its own ("member_call_expression") and is not an attribute access.
var phpChainTypes = map[string]bool{
	"member_access_expression": true, "nullsafe_member_access_expression": true,
}

// phpLeafTypes declares "variable_name" as a single name inside an access
// chain: the grammar splits it into a "$" token and a name.
var phpLeafTypes = map[string]bool{"variable_name": true}

var phpOperandSpec = Treesitter.OperandSpec{
	OperatorTokens: phpOperatorTokens,
	OperandTypes:   phpOperandTypes,
	CallTypes:      phpCallTypes,
	PruneTypes:     phpPruneTypes,
	ChainTypes:     phpChainTypes,
	LeafTypes:      phpLeafTypes,
	Normalize:      normalizePhpOperand,
	// "$this" is the PHP equivalent of the Go receiver: normalizing it is what
	// lets the cohesion metrics tell an attribute access from a local variable
	Receiver: "$this",
}

// ExtractOperatorsOperands collects Halstead operators and operands from the
// AST within the given 1-based inclusive line range. An attribute access is a
// single operand ("this.items", "obj.name"), and an access through "$this"
// reads the attribute whatever is done with it afterwards: "$this->items" and
// "$this->items[$k]" both read "this.items".
func (a *TreeSitterAdapter) ExtractOperatorsOperands(src []byte, startLine, endLine int) ([]string, []string) {
	root, source := a.ensureRoot(src)
	if root == nil {
		return nil, nil
	}
	ops, operands := phpOperandSpec.Extract(root, source, startLine, endLine)
	return foldPhpPipes(a.pipeLines(root, source), ops, startLine, endLine), operands
}

// foldPhpPipes repairs the PHP 8.5 pipe operator. The bundled grammar predates
// it and reads `'x' |> trim(...)` as a binary expression whose operator is ">"
// and whose left side ends with an ERROR node holding a lone "|". Reporting two
// operators where the source writes one would inflate both the vocabulary and
// the length, so each such pair is folded back into a single "|>".
func foldPhpPipes(pipeLines []int, ops []string, startLine, endLine int) []string {
	pipes := 0
	for _, line := range pipeLines {
		if line >= startLine && line <= endLine {
			pipes++
		}
	}
	if pipes == 0 {
		return ops
	}
	// the Halstead metrics read the operators as a multiset, so dropping the
	// halves wherever they sit is enough
	folded := make([]string, 0, len(ops))
	remainingBars, remainingGreater := pipes, pipes
	for _, op := range ops {
		if op == "|" && remainingBars > 0 {
			remainingBars--
			continue
		}
		if op == ">" && remainingGreater > 0 {
			remainingGreater--
			continue
		}
		folded = append(folded, op)
	}
	for i := 0; i < pipes; i++ {
		folded = append(folded, "|>")
	}
	return folded
}

// pipeLines returns the lines of the file carrying a mis-parsed "|>", found
// once for the whole file.
//
// Looking for them per function would walk down from the root for each of them,
// so a file would pay a pass over its top-level declarations once per function.
// The operator is also rare enough that most files are answered by looking for
// the two characters in the source.
func (a *TreeSitterAdapter) pipeLines(root *sitter.Node, src []byte) []int {
	if a.pipesFound {
		return a.pipes
	}
	a.pipesFound = true

	if !bytes.Contains(src, []byte("|>")) {
		return nil
	}

	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		if n.Type() == "binary_expression" && isPhpPipe(n, src) {
			a.pipes = append(a.pipes, int(n.StartPoint().Row)+1)
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(root)

	return a.pipes
}

// isPhpPipe reports whether a binary expression is a mis-parsed "|>".
func isPhpPipe(n *sitter.Node, src []byte) bool {
	operator := n.ChildByFieldName("operator")
	if operator == nil || operator.Type() != ">" {
		return false
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		child := n.Child(i)
		if child.Type() == "ERROR" && int(child.EndByte()) <= len(src) &&
			strings.TrimSpace(string(src[child.StartByte():child.EndByte()])) == "|" {
			return true
		}
	}
	return false
}

// ExtractMethodCalls scans the function body range and returns normalized method calls
// Examples recognized:
//
//	$this->foo(   => this.foo
//	$obj->bar(    => obj.bar
//	parent::baz(  => parent.baz
//	self::qux(    => self.qux
//	static::zap(  => static.zap
func (a *TreeSitterAdapter) ExtractMethodCalls(src []byte, startLine, endLine int) []string {
	if src == nil || startLine <= 0 || endLine <= 0 || endLine < startLine {
		return nil
	}
	lines := a.srcLines.Lines(src)
	res := []string{}
	add := func(s string) {
		if s != "" {
			res = append(res, s)
		}
	}
	// simple scanning using string ops; skip inside comments/strings via stripStrings
	for i := startLine - 1; i < endLine && i < len(lines); i++ {
		orig := strings.TrimSpace(lines[i])
		if orig == "" {
			continue
		}
		line := stripStrings(orig)
		// Convert arrow and scope for easier parsing but we need original to detect pattern
		// 1) $this->name(
		idx := 0
		for idx < len(line) {
			p := strings.Index(line[idx:], "$this->")
			if p < 0 {
				break
			}
			p += idx
			j := p + len("$this->")
			// read identifier
			k := j
			for k < len(line) && ((line[k] >= 'a' && line[k] <= 'z') || (line[k] >= 'A' && line[k] <= 'Z') || (line[k] >= '0' && line[k] <= '9') || line[k] == '_') {
				k++
			}
			if k < len(line) && line[k] == '(' {
				name := line[j:k]
				if name != "" {
					add("this." + name)
				}
			}
			idx = k
		}
		// 2) $obj->name(
		idx = 0
		for idx < len(line) {
			p := strings.Index(line[idx:], "$")
			if p < 0 {
				break
			}
			p += idx
			// read var name
			j := p + 1
			for j < len(line) && ((line[j] >= 'a' && line[j] <= 'z') || (line[j] >= 'A' && line[j] <= 'Z') || (line[j] >= '0' && line[j] <= '9') || line[j] == '_') {
				j++
			}
			if j+2 < len(line) && line[j] == '-' && line[j+1] == '>' {
				// read method name
				k := j + 2
				for k < len(line) && ((line[k] >= 'a' && line[k] <= 'z') || (line[k] >= 'A' && line[k] <= 'Z') || (line[k] >= '0' && line[k] <= '9') || line[k] == '_') {
					k++
				}
				if k < len(line) && line[k] == '(' {
					obj := line[p:j]
					meth := line[j+2 : k]
					if obj != "$this" { // $this handled above
						add(strings.TrimPrefix(obj, "$") + "." + meth)
					}
					idx = k
					continue
				}
			}
			idx = j
		}
		// 3) parent::/self::/static:: name(
		for _, kw := range []string{"parent::", "self::", "static::"} {
			idx = 0
			for idx < len(line) {
				p := strings.Index(line[idx:], kw)
				if p < 0 {
					break
				}
				p += idx
				j := p + len(kw)
				k := j
				for k < len(line) && ((line[k] >= 'a' && line[k] <= 'z') || (line[k] >= 'A' && line[k] <= 'Z') || (line[k] >= '0' && line[k] <= '9') || line[k] == '_') {
					k++
				}
				if k < len(line) && line[k] == '(' {
					base := strings.TrimSuffix(kw, "::")
					add(base + "." + line[j:k])
				}
				idx = k
			}
		}
	}
	return res
}

// stripStrings removes content inside single or double quotes
// CountComments counts PHP-style comment lines in the given range
// phpStatements maps the PHP grammar onto the shared logical-lines model.
//
// `case_statement` and `default_statement` carry the "_statement" suffix but are
// labels: they hold the branches of a switch, they are not instructions. A
// class constant and a property are members, so `const_declaration` and
// `property_declaration` are absent. `else_if_clause` is listed because an
// elseif carries a condition of its own, exactly like the `if` it extends,
// while `else_clause`, `catch_clause` and `finally_clause` are branch headers.
var phpStatements = &Treesitter.StatementSpec{
	Language: tsPhp.GetLanguage,
	Statement: []string{
		"expression_statement", "echo_statement", "unset_statement",
		"if_statement", "else_if_clause",
		"for_statement", "foreach_statement", "while_statement", "do_statement",
		"switch_statement", "try_statement",
		"return_statement", "break_statement", "continue_statement", "goto_statement",
		"global_declaration", "function_static_declaration", "static_variable_declaration",
		"declare_statement", "unset_expression",
	},
}

func (a *TreeSitterAdapter) Statement(n *sitter.Node) Treesitter.StatementKind {
	return phpStatements.Classify(n)
}

// CommentSyntax declares PHP comment tokens: "//", "#" and "/* */".
func (a *TreeSitterAdapter) CommentSyntax() engine.CommentSyntax {
	return engine.CommentSyntax{
		Line:       []string{"//", "#"},
		BlockOpen:  "/*",
		BlockClose: "*/",
		Quote:      []rune{'"', '\''},
	}
}

func stripStrings(s string) string {
	out := make([]rune, 0, len(s))
	inSingle := false
	inDouble := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' { // escape
			if i+1 < len(s) {
				i++
			}
			continue
		}
		if !inDouble && c == '\'' {
			inSingle = !inSingle
			continue
		}
		if !inSingle && c == '"' {
			inDouble = !inDouble
			continue
		}
		if inSingle || inDouble {
			continue
		}
		out = append(out, rune(c))
	}
	return string(out)
}

func normalizePhpOperand(name string) string {
	if name == "" {
		return name
	}
	// handle $this->prop and $var->prop
	if strings.HasPrefix(name, "$this->") {
		return "this." + strings.TrimPrefix(name, "$this->")
	}
	// parent/self/static static props: parent::$a
	if strings.HasPrefix(name, "parent::$") {
		return "parent." + strings.TrimPrefix(name, "parent::$")
	}
	if strings.HasPrefix(name, "self::$") {
		return "self." + strings.TrimPrefix(name, "self::$")
	}
	if strings.HasPrefix(name, "static::$") {
		return "static." + strings.TrimPrefix(name, "static::$")
	}
	// generic object access $obj->a
	if strings.HasPrefix(name, "$") && strings.Contains(name, "->") {
		name = strings.TrimPrefix(name, "$")
		return strings.ReplaceAll(name, "->", ".")
	}
	// simple variable $a
	if strings.HasPrefix(name, "$") {
		return strings.TrimPrefix(name, "$")
	}
	return name
}

func (a *TreeSitterAdapter) findNamespace() string {
	// If the aliases were collected already, the namespace came with them
	if a.computed {
		return a.ns
	}
	if a.src == nil {
		return ""
	}
	root := a.root
	if root == nil {
		parser := sitter.NewParser()
		parser.SetLanguage(tsPhp.GetLanguage())
		tree := parser.Parse(nil, a.src)
		root = tree.RootNode()
	}
	var ns string
	var walk func(*sitter.Node)
	walk = func(n *sitter.Node) {
		if n == nil || ns != "" {
			return
		}
		if n.Type() == "namespace_definition" {
			if nm := firstChildOfType(n, "namespace_name"); nm != nil {
				ns = a.text(nm)
			}
			return
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(root)
	a.ns = ns
	return ns
}
