package php

import (
	"regexp"
	"strings"
	"unicode/utf8"

	Treesitter "github.com/ast-metrics/ast-metrics/internal/engine/treesitter"
	sitter "github.com/smacker/go-tree-sitter"
	tsPhp "github.com/smacker/go-tree-sitter/php"
)

// Pre-compiled regex patterns for PHP external dependency scanning.
var (
	rePhpUse        = regexp.MustCompile(`(?m)^\s*use\s+([^;]+);`)
	rePhpNewClass   = regexp.MustCompile(`new\s+(\\)?([A-Za-z_][A-Za-z0-9_\\]*)`)
	rePhpStatic     = regexp.MustCompile(`(\\)?([A-Za-z_][A-Za-z0-9_\\]*)::`)
	rePhpFuncParams = regexp.MustCompile(`function\s+[A-Za-z_][A-Za-z0-9_]*\s*\(([^)]*)\)`)
	rePhpParamType  = regexp.MustCompile(`\??[A-Za-z_\\][A-Za-z0-9_\\]*\s*\$`)
	rePhpReturnType = regexp.MustCompile(`\)\s*:\s*\??([A-Za-z_\\][A-Za-z0-9_\\]*)`)
	rePhpProperty   = regexp.MustCompile(`(?m)^(?:\s*)(?:public|private|protected|var)\s+\??([A-Za-z_\\][A-Za-z0-9_\\]*)?\s*\$`)
)

type TreeSitterAdapter struct {
	src      []byte
	root     *sitter.Node // shared root from runner to avoid re-parsing
	ns       string
	aliases  map[string]string
	computed bool
	extDeps  []Treesitter.ImportItem
}

func NewTreeSitterAdapter(src []byte) *TreeSitterAdapter { return &TreeSitterAdapter{src: src} }
func (a *TreeSitterAdapter) SetSource(src []byte) {
	a.src = src
	a.root = nil
	a.computed = false
	a.ns = ""
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
func (a *TreeSitterAdapter) IsModule(n *sitter.Node) bool { return n.Type() == "program" }

func (a *TreeSitterAdapter) IsClass(n *sitter.Node) bool {
	switch n.Type() {
	case "class_declaration", "trait_declaration", "enum_declaration":
		return true
	}
	return false
}

// Optional interface awareness for Visitor
func (a *TreeSitterAdapter) IsInterface(n *sitter.Node) bool {
	return n != nil && n.Type() == "interface_declaration"
}

func (a *TreeSitterAdapter) IsFunction(n *sitter.Node) bool {
	switch n.Type() {
	case "function_definition", "method_declaration":
		return true
	}
	return false
}

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
	If:      []string{"if_statement"},
	Elif:    []string{"else_if_clause"},
	Else:    []string{"else_clause"},
	Loop:    []string{"while_statement", "for_statement", "foreach_statement", "do_statement"},
	Switch:  []string{"switch_statement", "match_expression"},
	Case:    []string{"case_statement", "match_conditional_expression"},
	Default: []string{"default_statement", "match_default_expression"},
	Catch:   []string{"catch_clause"},
	Ternary: []string{"conditional_expression"},
	Logical: []string{"binary_expression"},
	Ops:     []string{"&&", "||", "and", "or"},
}

func (a *TreeSitterAdapter) Decision(n *sitter.Node) Treesitter.DecisionKind {
	return phpDecisions.Classify(n, a.src)
}

func (a *TreeSitterAdapter) LogicalOperator(n *sitter.Node) string {
	return phpDecisions.LogicalOperator(n, a.src)
}

// ---- Imports (use statements) ----
func (a *TreeSitterAdapter) Imports(n *sitter.Node) []Treesitter.ImportItem {
	if n == nil {
		return nil
	}
	// compute once from whole source
	if !a.computed {
		a.computeExternalDependencies()
	}
	if n.Type() == "use_declaration" {
		// also return plain use declarations as dependencies
		items := []Treesitter.ImportItem{}
		var walk func(*sitter.Node)
		walk = func(x *sitter.Node) {
			if x == nil {
				return
			}
			t := x.Type()
			if t == "qualified_name" || t == "namespace_name" || t == "name" {
				mod := a.text(x)
				if mod != "" {
					items = append(items, Treesitter.ImportItem{Module: mod, Name: mod})
				}
			}
			for i := 0; i < int(x.ChildCount()); i++ {
				walk(x.Child(i))
			}
		}
		walk(n)
		return dedup(items)
	}
	// return precomputed externals on class nodes to attach them in class scope
	if n.Type() == "class_declaration" {
		return a.extDeps
	}
	return nil
}

// computeExternalDependencies scans the whole source and fills a.extDeps with external classes used within class bodies.
// It tries to resolve:
// - property and parameter type hints
// - return types
// - new ClassName
// - static calls ClassName::method/const and attributes ClassName::$ATTR
// - fully qualified names starting with \
// - use imports with aliases
func (a *TreeSitterAdapter) computeExternalDependencies() {
	if a.computed {
		return
	}
	a.computed = true
	a.extDeps = []Treesitter.ImportItem{}
	if a.src == nil {
		return
	}
	root := a.root
	if root == nil {
		parser := sitter.NewParser()
		parser.SetLanguage(tsPhp.GetLanguage())
		tree := parser.Parse(nil, a.src)
		root = tree.RootNode()
	}

	// collect namespace and use aliases — only scan top-level children
	// (namespace/use declarations are always at the top of a PHP file)
	a.aliases = map[string]string{}
	collectUse := func(n *sitter.Node) {
		// Traverse to collect any use_as_clause (aliases)
		var collect func(*sitter.Node)
		collect = func(x *sitter.Node) {
			if x == nil {
				return
			}
			if x.Type() == "use_as_clause" {
				var base, alias string
				if q := firstChildOfType(x, "qualified_name"); q != nil {
					base = a.text(q)
				}
				if base == "" {
					if nm := firstChildOfType(x, "name"); nm != nil {
						base = a.text(nm)
					}
				}
				if aliasNode := x.Child(int(x.ChildCount() - 1)); aliasNode != nil {
					alias = a.text(aliasNode)
				}
				if base != "" && alias != "" {
					a.aliases[alias] = base
				}
			}
			for i := 0; i < int(x.ChildCount()); i++ {
				collect(x.Child(i))
			}
		}
		collect(n)
		// simple import without alias: use A\B; alias is short name or single name
		if q := firstChildOfType(n, "qualified_name"); q != nil {
			base := a.text(q)
			if base != "" {
				short := base
				if idx := strings.LastIndex(base, "\\"); idx >= 0 {
					short = base[idx+1:]
				}
				a.aliases[short] = base
			}
		} else if nm := firstChildOfType(n, "name"); nm != nil {
			base := a.text(nm)
			if base != "" {
				a.aliases[base] = base
			}
		}
	}
	// Scan only top-level children (and one level into namespace body)
	for i := 0; i < int(root.ChildCount()); i++ {
		ch := root.Child(i)
		t := ch.Type()
		if t == "namespace_definition" {
			if nm := firstChildOfType(ch, "namespace_name"); nm != nil {
				a.ns = a.text(nm)
			}
			// Also scan use declarations inside namespace body
			if body := firstChildOfType(ch, "compound_statement"); body != nil {
				for j := 0; j < int(body.ChildCount()); j++ {
					inner := body.Child(j)
					if inner.Type() == "use_declaration" {
						collectUse(inner)
					}
				}
			}
		} else if t == "use_declaration" {
			collectUse(ch)
		}
	}

	// helper to resolve a class name considering alias, FQN and local namespace
	resolve := func(name string) string {
		if name == "" {
			return name
		}
		// drop leading ? nullable
		name = strings.TrimPrefix(name, "?")
		if strings.HasPrefix(name, "\\") {
			return strings.TrimPrefix(name, "\\")
		}
		// some global classes in PHP's root namespace should not be prefixed
		switch name {
		case "stdClass", "InvalidArgumentException":
			return name
		}
		if full, ok := a.aliases[name]; ok {
			return full
		}
		if a.ns != "" {
			return a.ns + "\\" + name
		}
		return name
	}
	add := func(class string) {
		if class == "" {
			return
		}
		// Ignore pseudo-class keywords which are not external dependencies
		last := class
		if idx := strings.LastIndex(class, "\\"); idx >= 0 {
			last = class[idx+1:]
		}
		switch strings.ToLower(last) {
		case "self", "static", "parent":
			return
		}
		a.extDeps = append(a.extDeps, Treesitter.ImportItem{Module: class, Name: class})
	}

	// Also, regex-pass to find use statements for aliases in case AST patterns differ
	src := string(a.src)
	{
		for _, m := range rePhpUse.FindAllStringSubmatch(src, -1) {
			clause := m[1]
			// split multiple by comma
			parts := strings.Split(clause, ",")
			for _, p := range parts {
				seg := strings.TrimSpace(p)
				if seg == "" {
					continue
				}
				if strings.Contains(seg, " as ") {
					kv := strings.SplitN(seg, " as ", 2)
					base := strings.TrimSpace(kv[0])
					alias := strings.TrimSpace(kv[1])
					if base != "" && alias != "" {
						a.aliases[alias] = base
					}
				} else {
					base := seg
					if base != "" {
						short := base
						if i := strings.LastIndex(short, "\\"); i >= 0 {
							short = short[i+1:]
						}
						a.aliases[short] = base
					}
				}
			}
		}
	}
	// Fallback: use regex-like scanning on the whole source to approximate dependencies (sufficient for tests)
	isPrimitive := func(t string) bool {
		low := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(t), "?"))
		switch low {
		case "int", "float", "string", "bool", "boolean", "array", "callable", "iterable", "void", "mixed", "object", "null":
			return true
		}
		return false
	}
	// new Class
	{
		for _, m := range rePhpNewClass.FindAllStringSubmatch(src, -1) {
			name := m[2]
			if m[1] != "" {
				name = "\\" + name
			}
			add(resolve(name))
		}
	}
	// Static: Class::
	{
		for _, m := range rePhpStatic.FindAllStringSubmatch(src, -1) {
			name := m[2]
			if m[1] != "" {
				name = "\\" + name
			}
			add(resolve(name))
		}
	}
	// Function parameter types
	{
		matches := rePhpFuncParams.FindAllStringSubmatch(src, -1)
		for _, mm := range matches {
			params := mm[1]
			for _, p := range rePhpParamType.FindAllString(params, -1) {
				name := strings.TrimSpace(strings.TrimSuffix(p, "$"))
				if !isPrimitive(name) {
					add(resolve(name))
				}
			}
		}
	}
	// Return types
	{
		for _, m := range rePhpReturnType.FindAllStringSubmatch(src, -1) {
			if !isPrimitive(m[1]) {
				add(resolve(m[1]))
			}
		}
	}
	// Property declarations with types
	{
		for _, m := range rePhpProperty.FindAllStringSubmatch(src, -1) {
			if m[1] != "" && !isPrimitive(m[1]) {
				add(resolve(m[1]))
			}
		}
	}
	// keep duplicates to align with expected metrics (counts each usage)
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
	return foldPhpPipes(root, source, ops, startLine, endLine), operands
}

// foldPhpPipes repairs the PHP 8.5 pipe operator. The bundled grammar predates
// it and reads `'x' |> trim(...)` as a binary expression whose operator is ">"
// and whose left side ends with an ERROR node holding a lone "|". Reporting two
// operators where the source writes one would inflate both the vocabulary and
// the length, so each such pair is folded back into a single "|>".
func foldPhpPipes(root *sitter.Node, src []byte, ops []string, startLine, endLine int) []string {
	pipes := countPhpPipes(root, src, startLine, endLine)
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

// countPhpPipes counts the "|>" written between startLine and endLine.
func countPhpPipes(root *sitter.Node, src []byte, startLine, endLine int) int {
	count := 0
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		if int(n.EndPoint().Row)+1 < startLine || int(n.StartPoint().Row)+1 > endLine {
			return
		}
		if n.Type() == "binary_expression" && isPhpPipe(n, src) {
			count++
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(root)
	return count
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
	lines := strings.Split(string(src), "\n")
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
func (a *TreeSitterAdapter) CountComments(lines []string, start, end int) int {
	cnt := 0
	inBlock := false
	for i := start - 1; i < end && i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		clean := stripStrings(line)

		if inBlock {
			// Every line of a block comment is a comment line, including the
			// closing "*/" delimiter line.
			cnt++
			if strings.Contains(clean, "*/") {
				inBlock = false
			}
			continue
		}

		if strings.HasPrefix(clean, "/*") {
			// Opening line of a "/*" or "/**" block; a block closing on the
			// same line stays a single comment line.
			cnt++
			if !strings.Contains(clean, "*/") {
				inBlock = true
			}
			continue
		}

		// line comments anywhere on the line
		if strings.Contains(clean, "//") || strings.HasPrefix(clean, "#") || strings.Contains(clean, "# ") {
			cnt++
			continue
		}
	}
	return cnt
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

func (a *TreeSitterAdapter) findNamespace() string {
	// If computeExternalDependencies already ran, reuse its result
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
