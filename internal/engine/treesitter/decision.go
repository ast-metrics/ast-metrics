package treesitter

import (
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
)

// Cyclomatic complexity model shared by every language.
//
// AST Metrics implements the extended McCabe measure, the one used by lizard,
// gocyclo, phploc, PMD and SonarQube: a scope starts at 1 and every construct
// that adds a branch to the control flow graph adds 1.
//
//	construct                                weight
//	if, elseif / elif                          1
//	else                                       0  (the if already paid for it)
//	loop (for, foreach, while, do, loop)       1
//	switch / match statement itself            0  (it only holds its branches)
//	case label, match arm                      1
//	default, catch-all arm (_)                 0  (the fallback path)
//	catch / except clause                      1
//	ternary (a ? b : c, a if c else b)         1
//	short-circuit boolean operator (&&, ||)    1
//
// A language adapter never decides how much a construct weighs: it only says
// which of these kinds a grammar node is. Two equivalent programs written in
// two languages therefore get the same number.
type DecisionKind int

const (
	DecNone DecisionKind = iota
	DecIf
	DecElif
	DecElse
	DecLoop
	DecSwitch
	DecCase
	// DecDefault is the fallback branch of a switch or match (default, _). Its
	// body is analyzed like any other, but it opens no new path.
	DecDefault
	// DecCatch is one exception handler (catch, except, rescue).
	DecCatch
	// DecTernary is a conditional expression.
	DecTernary
	// DecLogical is a short-circuit boolean operator.
	DecLogical
)

// DecisionSpec maps the node types of one grammar onto the shared vocabulary
// above. An adapter declares it once instead of hand-writing a traversal, so
// supporting a language means listing node names rather than reimplementing
// the model.
//
// A node type must appear in at most one list. Node types that need more than
// their name to be classified (a case label that turns out to be the catch-all
// one, for instance) are refined by the adapter after Classify has run.
type DecisionSpec struct {
	If      []string
	Elif    []string
	Else    []string
	Loop    []string
	Switch  []string
	Case    []string
	Default []string
	Catch   []string
	Ternary []string

	// Logical lists the node types that may carry a short-circuit boolean
	// operator, and Ops the operators themselves. Such a node counts only when
	// its `operator` field holds one of Ops, so that a bitwise `&` or a
	// comparison is never mistaken for a branch.
	Logical []string
	Ops     []string

	// Language is the grammar these node types belong to, which lets the
	// classification work on symbol ids instead of node type names. See
	// symbols.go.
	Language func() *sitter.Language

	once     sync.Once
	byType   map[string]DecisionKind
	logical  map[string]bool
	ops      map[string]bool
	bySymbol symbolTable[DecisionKind]
}

// decLogicalCandidate marks, inside the symbol table only, a node type that may
// carry a short-circuit operator. It never leaves Classify: what the node holds
// decides between DecLogical and DecNone.
const decLogicalCandidate DecisionKind = -1

func (s *DecisionSpec) index() {
	s.once.Do(func() {
		s.byType = make(map[string]DecisionKind)
		for kind, types := range map[DecisionKind][]string{
			DecIf:      s.If,
			DecElif:    s.Elif,
			DecElse:    s.Else,
			DecLoop:    s.Loop,
			DecSwitch:  s.Switch,
			DecCase:    s.Case,
			DecDefault: s.Default,
			DecCatch:   s.Catch,
			DecTernary: s.Ternary,
		} {
			for _, t := range types {
				s.byType[t] = kind
			}
		}
		s.logical = make(map[string]bool, len(s.Logical))
		for _, t := range s.Logical {
			s.logical[t] = true
		}
		s.ops = make(map[string]bool, len(s.Ops))
		for _, o := range s.Ops {
			s.ops[o] = true
		}
		if s.Language != nil {
			// a candidate for a boolean operator wins over the kind its name
			// would give it, as in Classify below
			byName := make(map[string]DecisionKind, len(s.byType)+len(s.logical))
			for t, kind := range s.byType {
				byName[t] = kind
			}
			for t := range s.logical {
				byName[t] = decLogicalCandidate
			}
			s.bySymbol = newSymbolTable(s.Language(), byName)
		}
	})
}

// Classify returns the decision kind of a node. src is the file content, read
// to get the operator of a candidate boolean expression.
func (s *DecisionSpec) Classify(n *sitter.Node, src []byte) DecisionKind {
	if n == nil {
		return DecNone
	}
	s.index()

	if s.bySymbol.ready() {
		kind := s.bySymbol.at(n)
		if kind == decLogicalCandidate {
			return s.logicalKind(n, src)
		}
		return kind
	}

	t := n.Type()
	if s.logical[t] {
		return s.logicalKind(n, src)
	}
	if kind, ok := s.byType[t]; ok {
		return kind
	}
	return DecNone
}

// logicalKind decides what a candidate for a short-circuit operator is worth:
// only the operator it carries tells a branch from a bitwise or comparison
// expression.
func (s *DecisionSpec) logicalKind(n *sitter.Node, src []byte) DecisionKind {
	op := n.ChildByFieldName("operator")
	if op == nil || !s.ops[nodeText(src, op)] {
		return DecNone
	}
	return DecLogical
}

// LogicalOperator returns the operator carried by a DecLogical node.
func (s *DecisionSpec) LogicalOperator(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	if op := n.ChildByFieldName("operator"); op != nil {
		return nodeText(src, op)
	}
	return ""
}

// IsElseBranch reports whether n is the else branch of the if statement that
// contains it, in a grammar that has no else_clause node of its own (Go, Java,
// C#): there the branch is the `alternative` field of the if statement, holding
// either a block (a plain else) or another if statement (an else-if).
//
// It answers false for the else-if form, which is counted as the if it is.
func IsElseBranch(n *sitter.Node, ifType string) bool {
	if n == nil || n.Type() == ifType {
		return false
	}
	parent := n.Parent()
	if parent == nil || parent.Type() != ifType {
		return false
	}
	alt := parent.ChildByFieldName("alternative")
	return alt != nil && alt.StartByte() == n.StartByte() && alt.EndByte() == n.EndByte()
}

// HasChildOfType reports whether one of the direct children of n is of the
// given type. Adapters use it to tell a `case` label from a `default` one when
// the grammar gives them the same node type.
func HasChildOfType(n *sitter.Node, types ...string) bool {
	if n == nil {
		return false
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		for _, t := range types {
			if n.Child(i).Type() == t {
				return true
			}
		}
	}
	return false
}
