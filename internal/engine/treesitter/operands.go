package treesitter

import (
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// CallOperator is the operator reported for a call: Halstead counts the
// invocation itself as one operator, whatever the callee is. It is the same
// symbol for every language so that the volumes stay comparable.
const CallOperator = "()"

// OperandSpec drives the AST-based extraction of Halstead operators and
// operands. On top of the plain token walk, it prunes type positions (a type
// annotation is not an operand), keeps member access chains whole ("a.b.c" is
// one operand, not three) and normalizes accesses made through the current
// object as "this.attr", the form the cohesion metrics (LCOM4) expect.
//
// What counts as an operator is deliberately the same for every language:
// the symbolic operators, the keywords that drive the control flow
// ("return", "if", "for", "throw"...), the argument separator, the
// subscript and the call itself. Leaving any of them out shrinks the
// operator count to almost nothing on plain code ("return keys($this);"),
// which collapses the Halstead volume and, in turn, inflates the
// maintainability index.
type OperandSpec struct {
	// OperatorTokens lists the anonymous leaf token types counted as operators.
	// Keywords are anonymous leaves in every tree-sitter grammar, so a keyword
	// operator ("return", "new", "instanceof") is declared here like a symbol.
	OperatorTokens map[string]bool
	// OperandTypes lists the named node types counted as operands.
	OperandTypes map[string]bool
	// CallTypes lists the node types counted as one CallOperator: a call is an
	// operator, and its callee keeps whatever role the language gives it
	// (an operand where identifiers are operands, nothing otherwise).
	CallTypes map[string]bool
	// PruneTypes lists the node types whose whole subtree yields nothing:
	// type annotations, type arguments, type declarations.
	PruneTypes map[string]bool
	// PruneFields lists the field names whose child is skipped, for children
	// that hold no operand but have no dedicated node type (the "receiver" and
	// "result" of a Go method).
	PruneFields map[string]bool
	// ChainTypes lists the member access node types ("selector_expression" in
	// Go, "member_expression" in TypeScript).
	ChainTypes map[string]bool
	// LeafTypes lists the node types read as a single name inside a member
	// access chain, even though the grammar gives them children (the PHP
	// "variable_name", made of a "$" token and a name).
	LeafTypes map[string]bool
	// Normalize rewrites an operand before it is reported, for languages whose
	// syntax decorates names (the "$" of a PHP variable). It is optional.
	Normalize func(string) string
	// Receiver names the variable standing for the current object (the
	// receiver of a Go method, "$this" in PHP). Languages where the object is
	// a keyword ("this", "super") leave it empty.
	Receiver string
}

// Extract walks the AST between startLine and endLine (1-based, inclusive) and
// returns the Halstead operators and operands found there. Strings and
// comments never produce tokens, so they are excluded by construction.
func (s OperandSpec) Extract(root *sitter.Node, src []byte, startLine, endLine int) ([]string, []string) {
	if root == nil || src == nil || startLine <= 0 || endLine < startLine {
		return nil, nil
	}
	// start from the smallest node holding the range rather than from the root:
	// walking the whole file for each of its methods costs a pass over every
	// sibling, and an enclosing call would lend its "()" to a nested function
	if narrowed := smallestNodeCovering(root, startLine, endLine); narrowed != nil {
		root = narrowed
	}
	return s.ExtractFrom(root, src, startLine, endLine)
}

// ExtractFrom is Extract walking from a node the adapter knows to hold the
// range, the declaration being measured, rather than from the node found by
// position. The position of a function written on one line from the first
// column, `int g() { return 1; }`, falls on its first token, the return type,
// which holds nothing to count.
func (s OperandSpec) ExtractFrom(root *sitter.Node, src []byte, startLine, endLine int) ([]string, []string) {
	if root == nil || src == nil || startLine <= 0 || endLine < startLine {
		return nil, nil
	}
	ops := []string{}
	operands := []string{}

	addOperand := func(name string) {
		if s.Normalize != nil {
			name = s.Normalize(name)
		}
		if name != "" {
			operands = append(operands, name)
		}
	}

	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		if outsideLines(n, startLine, endLine) {
			return
		}
		t := n.Type()
		if s.PruneTypes[t] {
			return
		}
		if s.CallTypes[t] {
			ops = append(ops, CallOperator)
			// the callee and the arguments still hold operators and operands
		}
		if s.ChainTypes[t] {
			if operand, separators, ok := s.chainOperand(n, src); ok {
				if operand != "" {
					addOperand(operand)
				}
				ops = append(ops, s.keptOperators(separators)...)
				return
			}
			// not a plain chain ("foo().bar"): its property names nothing on
			// its own, only the object is worth walking
			ops = append(ops, s.keptOperators(chainSeparator(n))...)
			if object := n.NamedChild(0); object != nil {
				walk(object)
			}
			return
		}
		if n.IsNamed() && s.OperandTypes[t] {
			// the object alone ("return e") names no attribute
			if txt := nodeText(src, n); s.Receiver == "" || txt != s.Receiver {
				addOperand(txt)
			}
			return
		}
		if !n.IsNamed() && n.ChildCount() == 0 && s.OperatorTokens[t] {
			ops = append(ops, t)
			return
		}
		// asking the grammar for a field name costs a call into tree-sitter for
		// every child: only the languages that prune by field pay for it
		prunesByField := len(s.PruneFields) > 0
		for i := 0; i < int(n.ChildCount()); i++ {
			if prunesByField && s.PruneFields[n.FieldNameForChild(i)] {
				continue
			}
			walk(n.Child(i))
		}
	}
	walk(root)
	return ops, operands
}

// keptOperators filters the tokens the language declares as operators. A
// language opts out of the access operator ("." in Python, where the attribute
// is read as two operands) by leaving it out of OperatorTokens.
func (s OperandSpec) keptOperators(tokens []string) []string {
	kept := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		if s.OperatorTokens[tok] {
			kept = append(kept, tok)
		}
	}
	return kept
}

// MethodCalls returns the methods called on the current object between
// startLine and endLine, normalized as "this.name" ("super.name" for a call on
// the parent class). Calls made on another variable or on a package say
// nothing about the cohesion of the class and are not reported.
func (s OperandSpec) MethodCalls(root *sitter.Node, src []byte, startLine, endLine int) []string {
	if root == nil || src == nil || startLine <= 0 || endLine < startLine {
		return nil
	}
	var calls []string
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		if outsideLines(n, startLine, endLine) {
			return
		}
		if n.Type() == "call_expression" {
			if fn := n.ChildByFieldName("function"); fn != nil && s.ChainTypes[fn.Type()] {
				if segments, _, ok := s.flatten(fn, src); ok && len(segments) == 2 {
					if object := s.normalizeObject(segments[0]); object != "" {
						calls = append(calls, object+"."+segments[1])
					}
				}
			}
		}
		for i := 0; i < int(n.ChildCount()); i++ {
			walk(n.Child(i))
		}
	}
	walk(root)
	return calls
}

// chainOperand renders a member access chain as a single operand, and reports
// the access operators it went through ("." for "a.b.c", "?->" for a PHP
// nullsafe access). An access through the current object reads an attribute,
// whatever is done with it afterwards: "this.items", "this.items.length" and
// "this.items.push()" all read "this.items" (Go receivers are normalized the
// same way: "e.counter" gives "this.counter"). A direct call on the object
// ("this.load()") reads no attribute at all: it is a method call, reported by
// MethodCalls. ok is false when the chain is not a plain chain of identifiers
// ("foo().bar").
func (s OperandSpec) chainOperand(n *sitter.Node, src []byte) (operand string, separators []string, ok bool) {
	segments, separators, ok := s.flatten(n, src)
	if !ok {
		return "", nil, false
	}
	if object := s.normalizeObject(segments[0]); object != "" {
		if len(segments) == 2 && isCallee(n) {
			return "", separators, true
		}
		return object + "." + segments[1], separators, true
	}
	return strings.Join(segments, "."), separators, true
}

// normalizeObject returns "this" for the current object ("this", the Go
// receiver, the PHP "$this"), "super" for the parent class, and "" for
// anything else.
func (s OperandSpec) normalizeObject(root string) string {
	if s.Receiver != "" && root == s.Receiver {
		return "this"
	}
	if root == "this" || root == "super" {
		return root
	}
	return ""
}

// flatten splits a member access chain into its segments and the access
// operators between them: `this.items.length` gives ["this", "items",
// "length"] and [".", "."]. ok is false when a link is not a plain name (a
// call, an index, a literal).
func (s OperandSpec) flatten(n *sitter.Node, src []byte) ([]string, []string, bool) {
	if s.ChainTypes[n.Type()] {
		// every grammar shapes a chain the same way: object, separator, name
		if n.ChildCount() != 3 {
			return nil, nil, false
		}
		name := n.Child(2)
		if !name.IsNamed() || name.NamedChildCount() != 0 {
			return nil, nil, false
		}
		segments, separators, ok := s.flatten(n.Child(0), src)
		if !ok {
			return nil, nil, false
		}
		return append(segments, nodeText(src, name)), append(separators, n.Child(1).Type()), true
	}
	if s.isChainLeaf(n) {
		return []string{nodeText(src, n)}, nil, true
	}
	return nil, nil, false
}

// isChainLeaf reports whether the node names one link of a chain: a plain
// identifier, a keyword standing for the current object ("this" is an
// anonymous token in C#), or a node the language declares as a leaf (the PHP
// "variable_name"). A literal is not a name: "1.foo" is not an attribute.
func (s OperandSpec) isChainLeaf(n *sitter.Node) bool {
	if s.LeafTypes[n.Type()] {
		return true
	}
	return n.NamedChildCount() == 0 && !strings.HasSuffix(n.Type(), "literal")
}

// chainSeparator returns the access operator of a chain whose name says
// nothing on its own ("foo().bar"): the access itself still happened.
func chainSeparator(n *sitter.Node) []string {
	if n.ChildCount() != 3 {
		return nil
	}
	if sep := n.Child(1); !sep.IsNamed() {
		return []string{sep.Type()}
	}
	return nil
}

// isCallee reports whether the node is the callee of a call. Every grammar
// names that child "function", whatever it calls the call node itself
// ("call_expression" in Go and TypeScript, "invocation_expression" in C#).
func isCallee(n *sitter.Node) bool {
	parent := n.Parent()
	if parent == nil {
		return false
	}
	fn := parent.ChildByFieldName("function")
	return fn != nil && fn.StartByte() == n.StartByte() && fn.EndByte() == n.EndByte()
}

func outsideLines(n *sitter.Node, startLine, endLine int) bool {
	return int(n.EndPoint().Row)+1 < startLine || int(n.StartPoint().Row)+1 > endLine
}

// smallestNodeCovering returns the deepest named node holding the whole line
// range, or nil when the range reaches past it. Anchoring the walk there keeps
// it proportional to the code being measured instead of to the file.
func smallestNodeCovering(root *sitter.Node, startLine, endLine int) *sitter.Node {
	start := sitter.Point{Row: uint32(startLine - 1), Column: 0}
	end := sitter.Point{Row: uint32(endLine - 1), Column: 0}
	node := root.NamedDescendantForPointRange(start, end)
	if node == nil {
		return nil
	}
	// the last line of the range may hold more than the node found (a closing
	// brace shared with an enclosing block): only keep a node that covers it
	if int(node.EndPoint().Row)+1 < endLine || int(node.StartPoint().Row)+1 > startLine {
		return nil
	}
	return node
}

func nodeText(src []byte, n *sitter.Node) string {
	if int(n.EndByte()) > len(src) {
		return ""
	}
	return string(src[n.StartByte():n.EndByte()])
}
