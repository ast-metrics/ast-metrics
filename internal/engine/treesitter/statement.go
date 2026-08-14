package treesitter

import (
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
)

// Logical lines of code model shared by every language.
//
// LLOC is the number of distinct source lines on which a statement begins,
// inside the range of the scope being measured. Counting lines rather than
// statements is what makes the number comparable to the physical LOC next to
// it: two statements written on one line count once, and one statement spread
// over five lines counts once too.
//
// A statement is an instruction, something the program does at run time:
//
//	counted                                        not counted
//	assignment, call, increment                    the block that holds them
//	local variable declaration                     a field or property
//	if, else-if                                    the else clause
//	loop (for, foreach, while, do, loop)           the loop body
//	switch / match                                 a case label, a match arm
//	try                                            a catch or finally clause
//	return, break, continue, goto, throw           a class or function header
//	with / using / lock / synchronized             an import or a namespace
//
// Two rules resolve the constructs a node type alone cannot classify:
//
//   - a branch header is not a statement, but an else-if is: it carries a
//     condition of its own, so it is the nested `if` that it is. Grammars spell
//     it either as a nested if (Go, Java, C#, TypeScript) or as a dedicated
//     clause (PHP `else_if_clause`, Python `elif_clause`, Rust `else_clause`
//     holding an `if_expression`); both count once.
//
//   - the same grammar node can be an instruction or a member declaration
//     depending on where it sits. Go spells a package-level constant and a
//     local one with the same `const_declaration`, and Python spells a class
//     attribute with the same `expression_statement` as a real statement. What
//     settles it is the nearest enclosing scope: inside a function it is an
//     instruction, inside a class body it is a member, and at file scope a
//     declaration is a member while a statement is script code.
//
// A language adapter never decides whether a construct is worth a logical
// line: it only says which of these kinds a grammar node is. Two equivalent
// programs written in two languages therefore get the same number.
type StatementKind int

const (
	// NotAStatement is structure, a member declaration or a branch label.
	NotAStatement StatementKind = iota
	// IsStatement is an instruction, wherever it is written.
	IsStatement
	// IsLocalDeclaration is a declaration that is an instruction only inside a
	// function body. At file or class scope the same node declares a member.
	IsLocalDeclaration
)

// StatementSpec maps the node types of one grammar onto the vocabulary above.
//
// The lists are exhaustive on purpose: there is no "everything ending in
// _statement" fallback. That convention held for most nodes and silently broke
// on the rest, counting PHP `case_statement` labels as instructions while
// missing Java's `switch_expression` altogether.
//
// A node type must appear in at most one list.
type StatementSpec struct {
	// Statement lists the node types that are an instruction.
	Statement []string

	// LocalDeclaration lists the node types that declare something, and are an
	// instruction only inside a function body.
	LocalDeclaration []string

	// Language is the grammar these node types belong to, which lets the
	// classification work on symbol ids instead of node type names. See
	// symbols.go.
	Language func() *sitter.Language

	once     sync.Once
	byType   map[string]StatementKind
	bySymbol symbolTable[StatementKind]
}

func (s *StatementSpec) index() {
	s.once.Do(func() {
		s.byType = make(map[string]StatementKind, len(s.Statement)+len(s.LocalDeclaration))
		for _, t := range s.Statement {
			s.byType[t] = IsStatement
		}
		for _, t := range s.LocalDeclaration {
			s.byType[t] = IsLocalDeclaration
		}
		if s.Language != nil {
			s.bySymbol = newSymbolTable(s.Language(), s.byType)
		}
	})
}

// Classify returns the statement kind of a node, out of context.
func (s *StatementSpec) Classify(n *sitter.Node) StatementKind {
	if n == nil {
		return NotAStatement
	}
	s.index()
	if s.bySymbol.ready() {
		return s.bySymbol.at(n)
	}
	return s.byType[n.Type()]
}

// IsStringExpression reports whether n is an expression statement whose whole
// content is a string literal.
//
// Such a statement computes nothing and stores nothing: it is documentation. It
// is how Python writes a docstring, and a docstring is the documentation of the
// function it opens, exactly like the docblock the other languages put above
// theirs. Counting it as an instruction would make a documented function look
// bigger than the same function documented with `#` comments.
func IsStringExpression(n *sitter.Node, stringTypes ...string) bool {
	if n == nil {
		return false
	}
	named := 0
	isString := false
	for i := 0; i < int(n.ChildCount()); i++ {
		ch := n.Child(i)
		if !ch.IsNamed() {
			continue
		}
		named++
		for _, t := range stringTypes {
			if ch.Type() == t {
				isString = true
			}
		}
	}
	return named == 1 && isString
}
