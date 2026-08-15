package python

import (
	"path/filepath"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	Treesitter "github.com/ast-metrics/ast-metrics/internal/engine/treesitter"
	sitter "github.com/smacker/go-tree-sitter"
	tsPython "github.com/smacker/go-tree-sitter/python"
)

type TreeSitterAdapter struct {
	src  []byte
	root *sitter.Node
	// docStringLines holds the 1-based lines on which a bare string
	// expression begins, computed once from the tree on first use.
	docStringLines map[int]bool
}

func NewTreeSitterAdapter(src []byte) *TreeSitterAdapter { return &TreeSitterAdapter{src: src} }

func (a *TreeSitterAdapter) SetSource(src []byte)          { a.src = src }
func (a *TreeSitterAdapter) SetRootNode(root *sitter.Node) { a.root = root; a.docStringLines = nil }

func (a *TreeSitterAdapter) NodeName(n *sitter.Node) string {
	if a.src == nil || n == nil {
		return ""
	}
	// champ nommé "name" si dispo
	if name := n.ChildByFieldName("name"); name != nil {
		return string(a.src[name.StartByte():name.EndByte()])
	}
	// fallback: premier identifier
	if id := firstChildOfType(n, "identifier"); id != nil {
		return string(a.src[id.StartByte():id.EndByte()])
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
	// fallback: last child of type "block"
	if b := firstChildOfType(n, "block"); b != nil {
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
	// some grammars name this "parameter_list"
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
	walk = func(n *sitter.Node) {
		if n == nil {
			return
		}
		if n.Type() == "identifier" {
			yield(string(a.src[n.StartByte():n.EndByte()]))
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(params)
}

func (a *TreeSitterAdapter) Language() *sitter.Language { return tsPython.GetLanguage() }

// These questions are asked on every node of every walk, so they are answered
// by symbol id rather than by node type name. See
// internal/engine/treesitter/symbols.go.
var (
	pythonModules   = &Treesitter.TypeSet{Language: tsPython.GetLanguage, Types: []string{"module"}}
	pythonClasses   = &Treesitter.TypeSet{Language: tsPython.GetLanguage, Types: []string{"class_definition"}}
	pythonFunctions = &Treesitter.TypeSet{Language: tsPython.GetLanguage, Types: []string{"function_definition", "async_function_definition"}}
)

func (a *TreeSitterAdapter) IsModule(n *sitter.Node) bool   { return pythonModules.Has(n) }
func (a *TreeSitterAdapter) IsClass(n *sitter.Node) bool    { return pythonClasses.Has(n) }
func (a *TreeSitterAdapter) IsFunction(n *sitter.Node) bool { return pythonFunctions.Has(n) }

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
	case "block":
		for i := 0; i < int(body.ChildCount()); i++ {
			yield(body.Child(i))
		}

	case "match_statement":
		// Yield all case nodes, regardless of depth or wrapper.
		var walk func(*sitter.Node)
		walk = func(n *sitter.Node) {
			if n == nil {
				return
			}
			tt := n.Type()
			if tt == "case_clause" || tt == "case_block" {
				yield(n)
				// do not return; there can be nested blocks to visit later if needed
			}
			for i := 0; i < int(n.ChildCount()); i++ {
				walk(n.Child(i))
			}
		}
		walk(body)

	default:
		for i := 0; i < int(body.ChildCount()); i++ {
			yield(body.Child(i))
		}
	}
}
func (a *TreeSitterAdapter) text(n *sitter.Node) string {
	if n == nil || a.src == nil {
		return ""
	}
	return string(a.src[n.StartByte():n.EndByte()])
}
func findDescendantOfType(n *sitter.Node, t string) *sitter.Node {
	if n == nil {
		return nil
	}
	if n.Type() == t {
		return n
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if got := findDescendantOfType(n.Child(i), t); got != nil {
			return got
		}
	}
	return nil
}

// pythonDecisions maps the Python grammar onto the shared complexity model.
//
// A comprehension is a loop with an optional filter, so `for_in_clause` and
// `if_clause` count exactly like the statements they abbreviate. `case _:` is
// a case_clause like any other and is demoted to a Default by Decision below.
var pythonDecisions = &Treesitter.DecisionSpec{
	Language: tsPython.GetLanguage,
	If:       []string{"if_statement", "if_clause"},
	Elif:     []string{"elif_clause"},
	Else:     []string{"else_clause"},
	Loop:     []string{"for_statement", "while_statement", "for_in_clause"},
	Switch:   []string{"match_statement"},
	Case:     []string{"case_clause", "case_block"},
	Catch:    []string{"except_clause", "except_group_clause"},
	Ternary:  []string{"conditional_expression"},
	Logical:  []string{"boolean_operator"},
	Ops:      []string{"and", "or"},
}

func (a *TreeSitterAdapter) Decision(n *sitter.Node) Treesitter.DecisionKind {
	kind := pythonDecisions.Classify(n, a.src)
	if kind == Treesitter.DecCase && isWildcardCase(n, a.src) {
		return Treesitter.DecDefault
	}
	return kind
}

func (a *TreeSitterAdapter) LogicalOperator(n *sitter.Node) string {
	return pythonDecisions.LogicalOperator(n, a.src)
}

// isWildcardCase reports whether a match arm is the catch-all one (`case _:`).
// It matches anything, so it is the fallback path rather than a decision.
func isWildcardCase(n *sitter.Node, src []byte) bool {
	pattern := firstChildOfType(n, "case_pattern")
	if pattern == nil || int(pattern.EndByte()) > len(src) {
		return false
	}
	return strings.TrimSpace(string(src[pattern.StartByte():pattern.EndByte()])) == "_"
}

func (a *TreeSitterAdapter) Imports(n *sitter.Node) []Treesitter.ImportItem {
	if n == nil {
		return nil
	}
	switch n.Type() {
	case "import_statement":
		return importsFromImportStatement(a, n)
	case "import_from_statement":
		return importsFromImportFromStatement(a, n)
	default:
		return nil
	}
}

func importsFromImportStatement(a *TreeSitterAdapter, n *sitter.Node) []Treesitter.ImportItem {
	items := []Treesitter.ImportItem{}
	// Robust: walk descendants and pick modules
	var walk func(*sitter.Node)
	walk = func(x *sitter.Node) {
		if x == nil {
			return
		}
		switch x.Type() {
		case "aliased_import":
			// Original symbol is in field "name"
			if nm := x.ChildByFieldName("name"); nm != nil {
				if txt := a.text(nm); txt != "" {
					items = append(items, Treesitter.ImportItem{Module: txt})
				}
				return // do not walk into alias
			}
			// Fallbacks
			if dn := firstChildOfType(x, "dotted_name"); dn != nil {
				items = append(items, Treesitter.ImportItem{Module: a.text(dn)})
				return
			}
			if id := firstChildOfType(x, "identifier"); id != nil {
				items = append(items, Treesitter.ImportItem{Module: a.text(id)})
				return
			}
		case "dotted_name":
			items = append(items, Treesitter.ImportItem{Module: a.text(x)})
			return
		case "identifier":
			txt := a.text(x)
			if txt != "" && txt != "import" && txt != "as" {
				items = append(items, Treesitter.ImportItem{Module: txt})
				return
			}
		}
		for i := 0; i < int(x.ChildCount()); i++ {
			walk(x.Child(i))
		}
	}
	walk(n)
	return dedup(items)
}

// helper: find first child of given type
func firstChildOfType(n *sitter.Node, t string) *sitter.Node {
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		if ch.Type() == t {
			return ch
		}
	}
	return nil
}

// helper: find the byte offset of the `import` keyword
func importKeywordStart(n *sitter.Node) (uint32, bool) {
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		if ch.Type() == "import" { // token node
			return ch.StartByte(), true
		}
	}
	return 0, false
}

func importsFromImportFromStatement(a *TreeSitterAdapter, n *sitter.Node) []Treesitter.ImportItem {
	items := []Treesitter.ImportItem{}
	if n == nil {
		return items
	}

	// 1) cut at `import`
	cut, ok := importKeywordStart(n)
	if !ok {
		// fallback to previous version if grammar differs
		host := findDescendantOfType(n, "import_list")
		if host == nil {
			host = n
		}
		// reuse your non-cut logic here if needed
	}

	// 2) resolve module: rightmost module-like node BEFORE cut
	var moduleNode *sitter.Node
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		if ch.EndByte() <= cut && (ch.Type() == "dotted_name" || ch.Type() == "relative_import") {
			moduleNode = ch // keep the last one before `import`
		}
	}
	// The specifier is kept as written, leading dots included. They are not
	// decoration: `from ..model import User` and `from model import User` name
	// two different modules, and dropping the dots makes the two records
	// identical.
	module := strings.TrimSpace(a.text(moduleNode))

	// 3) collect names: nodes AFTER cut
	host := findDescendantOfType(n, "import_list")
	if host == nil {
		host = n
	}

	for i := 0; i < int(host.ChildCount()); i++ {
		ch := host.Child(i)
		if ch.StartByte() <= cut {
			continue
		}

		switch ch.Type() {
		case "aliased_import":
			// original symbol in field "name"
			if nm := ch.ChildByFieldName("name"); nm != nil {
				if name := a.text(nm); name != "" {
					items = append(items, Treesitter.ImportItem{Module: module, Name: name})
					continue
				}
			}
			// fallbacks
			if dn := firstChildOfType(ch, "dotted_name"); dn != nil {
				items = append(items, Treesitter.ImportItem{Module: module, Name: a.text(dn)})
				continue
			}
			if id := firstChildOfType(ch, "identifier"); id != nil {
				items = append(items, Treesitter.ImportItem{Module: module, Name: a.text(id)})
				continue
			}

		case "dotted_name":
			items = append(items, Treesitter.ImportItem{Module: module, Name: a.text(ch)})

		case "identifier":
			txt := a.text(ch)
			if txt != "" && txt != "import" && txt != "as" && txt != "*" {
				items = append(items, Treesitter.ImportItem{Module: module, Name: txt})
			}
		}
	}

	return dedup(items)
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

// pythonStatements maps the Python grammar onto the shared logical-lines model.
//
// `elif_clause` is listed: it carries a condition, so it is the nested `if` that
// it is. The other clauses (`else_clause`, `except_clause`, `finally_clause`,
// `case_clause`) are branch headers and count nothing of their own.
//
// `pass_statement` is absent. `pass` is not work the program does, it is how
// Python spells an empty block; counting it would make a Python stub bigger
// than the same stub written with braces.
//
// A class attribute (`x = 0` in a class body) is an `expression_statement` like
// any other, and is left out by the enclosing-scope rule of the shared model,
// not by its type.
var pythonStatements = &Treesitter.StatementSpec{
	Language: tsPython.GetLanguage,
	Statement: []string{
		"expression_statement", "delete_statement", "assert_statement",
		"if_statement", "elif_clause",
		"for_statement", "while_statement", "with_statement",
		"match_statement", "try_statement",
		"return_statement", "break_statement", "continue_statement", "raise_statement",
		"global_statement", "nonlocal_statement", "exec_statement", "print_statement",
	},
}

func (a *TreeSitterAdapter) Statement(n *sitter.Node) Treesitter.StatementKind {
	kind := pythonStatements.Classify(n)
	// a bare string expression is a docstring: documentation, not an
	// instruction. Python spells with a string what the other languages spell
	// with a comment block, and a documented function must not come out bigger
	// for it.
	if kind == Treesitter.IsStatement && n.Type() == "expression_statement" &&
		Treesitter.IsStringExpression(n, "string", "concatenated_string") {
		return Treesitter.NotAStatement
	}
	return kind
}

// CommentSyntax declares Python comment tokens: "#" starts a comment, "//" is
// the floor division operator and "/* */" does not exist. A triple-quoted
// string written as a statement of its own is the docstring of what follows
// it, so it is documentation, exactly like a docblock in the other languages.
// The same string written as a value (a fixture in a tuple, a query passed to
// a call) is code, and only the tree tells the two apart, black having taught
// everyone to write `(` on one line and `"""` alone on the next.
func (a *TreeSitterAdapter) CommentSyntax() engine.CommentSyntax {
	return engine.CommentSyntax{
		Line:        []string{"#"},
		DocString:   []string{`"""`, `'''`},
		Quote:       []rune{'"', '\''},
		IsDocString: a.isDocStringLine,
		// a docstring may be raw or formatted: r"""...""", f"""..."""
		LetterPrefixedStrings: true,
	}
}

// isDocStringLine reports whether the string opening at the start of the given
// 1-based line is a bare string expression: the docstring of a module, class or
// function, or a string dropped as a statement anywhere else, which the LLOC
// model leaves out for the same reason. Without a tree to ask, every
// triple-quoted string standing alone on its line is documentation, as before.
func (a *TreeSitterAdapter) isDocStringLine(line int) bool {
	if a.docStringLines == nil {
		root := a.root
		if root == nil {
			if a.src == nil {
				return true
			}
			parser := sitter.NewParser()
			parser.SetLanguage(a.Language())
			root = parser.Parse(nil, a.src).RootNode()
		}
		a.docStringLines = map[int]bool{}
		collectDocStringLines(root, a.docStringLines)
	}
	return a.docStringLines[line]
}

// collectDocStringLines records the starting line of every expression_statement
// made of a single string.
func collectDocStringLines(n *sitter.Node, lines map[int]bool) {
	if n == nil {
		return
	}
	if n.Type() == "expression_statement" {
		if Treesitter.IsStringExpression(n, "string", "concatenated_string") {
			lines[int(n.StartPoint().Row)+1] = true
		}
		return
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		collectDocStringLines(n.NamedChild(i), lines)
	}
}

// pythonOperatorTokens lists the anonymous token types counted as Halstead
// operators: arithmetic, comparison, assignment, bitwise, walrus, attribute
// access, the argument separator, the subscript and the keywords that drive
// the control flow. Keywords count as operators: without them, a body made of
// plain statements ("return list(self.items)") would hold none at all, and its
// Halstead volume would collapse to zero.
var pythonOperatorTokens = map[string]bool{
	"+": true, "-": true, "*": true, "/": true, "//": true, "%": true, "**": true, "@": true,
	"==": true, "!=": true, "<": true, ">": true, "<=": true, ">=": true, "<>": true,
	"=": true, "+=": true, "-=": true, "*=": true, "/=": true, "//=": true, "%=": true,
	"**=": true, "@=": true, "&=": true, "|=": true, "^=": true, "<<=": true, ">>=": true,
	"&": true, "|": true, "^": true, "<<": true, ">>": true, "~": true, ":=": true,
	".": true, "and": true, "or": true, "not": true, "in": true, "is": true,
	",": true, "[": true,
	"return": true, "if": true, "elif": true, "else": true, "for": true, "while": true,
	"break": true, "continue": true, "pass": true, "try": true, "except": true,
	"finally": true, "raise": true, "assert": true, "del": true, "with": true,
	"as": true, "lambda": true, "yield": true, "await": true, "global": true,
	"nonlocal": true,
}

// pythonOperandTypes lists the named node types counted as Halstead operands:
// identifiers and literals. String content is counted rather than the whole
// string node, so quotes and interpolation braces are ignored.
var pythonOperandTypes = map[string]bool{
	"identifier": true, "integer": true, "float": true, "string_content": true,
	"true": true, "false": true, "none": true,
}

// pythonCallTypes lists the node types counted as one call operator.
var pythonCallTypes = map[string]bool{"call": true}

// pythonOperandSpec reads attributes as two operands joined by the "."
// operator ("self.items" gives "self" and "items"): the cohesion metrics rely
// on the bare attribute name, since Python names the current object with a
// plain parameter.
var pythonOperandSpec = Treesitter.OperandSpec{
	OperatorTokens: pythonOperatorTokens,
	OperandTypes:   pythonOperandTypes,
	CallTypes:      pythonCallTypes,
}

// ExtractOperatorsOperands collects Halstead operators and operands from the
// AST within the given 1-based inclusive line range.
func (a *TreeSitterAdapter) ExtractOperatorsOperands(src []byte, startLine, endLine int) ([]string, []string) {
	root := a.root
	source := a.src
	if root == nil {
		if source == nil {
			source = src
		}
		if source == nil {
			return nil, nil
		}
		parser := sitter.NewParser()
		parser.SetLanguage(a.Language())
		tree := parser.Parse(nil, source)
		root = tree.RootNode()
	}
	return pythonOperandSpec.Extract(root, source, startLine, endLine)
}
