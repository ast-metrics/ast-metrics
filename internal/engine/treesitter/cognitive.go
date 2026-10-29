package treesitter

import (
	sitter "github.com/smacker/go-tree-sitter"
)

// Cognitive complexity model shared by every language.
//
// AST Metrics implements SonarSource's measure (G. Ann Campbell, "Cognitive
// Complexity", 2017), read off the same decision kinds as the cyclomatic model
// in decision.go. The only thing a language adapter adds for it is its
// shorthand (DecisionSpec.Shorthand):
//
//	construct                                  increment
//	if, loop, switch / match, catch, ternary   1 + nesting, and nests its body
//	elseif / elif / else if, else              1, and nests its body
//	case label, match arm, default             0  (the switch already paid)
//	run of like boolean operators              1  (a && b && c: 1; a && b || c: 2)
//	shorthand (Rust ?, a comprehension clause) 0
//
// Nesting is the number of nesting constructs around a node within its
// function. An else-if continues its chain: it sits at the nesting of the if
// that starts it, not one level deeper.
//
// Shorthand follows the specification's rule that a construct collapsing
// several statements into one readable expression adds nothing, as its
// null-coalescing example does. It still counts for cyclomatic complexity: it
// is a branch, only an easy one to read.
//
// A function declared inside another is scored on its own, as for cyclomatic
// complexity, so that no aggregate counts it twice. A closure the adapter does
// not treat as a function (a Go function literal, a Java or C# lambda, a PHP or
// Rust closure) is part of the function around it: its branches count there,
// and it does not raise the nesting. Recursion and jumps to a label, which the
// specification also counts, are not classified by the shared model and add
// nothing.
func (v *Visitor) cognitiveComplexity(fn *sitter.Node) int32 {
	var score int32
	var visit func(n *sitter.Node, nesting int32, elseIf bool)

	children := func(n *sitter.Node, each func(ch *sitter.Node)) {
		for i := 0; i < int(n.ChildCount()); i++ {
			if ch := n.Child(i); ch != nil && !v.isScopeBoundary(ch) {
				each(ch)
			}
		}
	}

	visit = func(n *sitter.Node, nesting int32, elseIf bool) {
		switch v.cognitiveKind(n) {
		case DecIf:
			score += ifIncrement(nesting, elseIf)
			children(n, func(ch *sitter.Node) {
				if v.continuesChain(n, ch) {
					visit(ch, nesting, v.cognitiveKind(ch) == DecIf)
					return
				}
				visit(ch, nesting+1, false)
			})
		case DecElse:
			if inner := v.elseIfIn(n); inner != nil {
				children(n, func(ch *sitter.Node) { visit(ch, nesting, ch == inner) })
				return
			}
			score++
			children(n, func(ch *sitter.Node) { visit(ch, nesting+1, false) })
		case DecElif:
			score++
			children(n, func(ch *sitter.Node) { visit(ch, nesting+1, false) })
		case DecLoop, DecSwitch, DecCatch, DecTernary:
			score += 1 + nesting
			children(n, func(ch *sitter.Node) { visit(ch, nesting+1, false) })
		case DecLogical:
			if !v.continuesRun(n) {
				score++
			}
			children(n, func(ch *sitter.Node) { visit(ch, nesting, false) })
		default:
			children(n, func(ch *sitter.Node) { visit(ch, nesting, false) })
		}
	}

	children(fn, func(ch *sitter.Node) { visit(ch, 0, false) })
	return score
}

// cognitiveKind is the decision kind of a node, with shorthand worth nothing.
func (v *Visitor) cognitiveKind(n *sitter.Node) DecisionKind {
	kind := v.ad.Decision(n)
	if kind == DecNone {
		return kind
	}
	if sa, ok := v.ad.(ShorthandAware); ok && sa.IsShorthand(n) {
		return DecNone
	}
	return kind
}

func ifIncrement(nesting int32, elseIf bool) int32 {
	if elseIf {
		return 1
	}
	return 1 + nesting
}

// continuesChain reports whether ch, a child of the if statement n, is the
// rest of its chain: an else or elif clause, or the else branch held in the
// `alternative` field by grammars without an else node (Go, Java, C#), where
// an else-if is an if statement in that field.
func (v *Visitor) continuesChain(n, ch *sitter.Node) bool {
	switch v.cognitiveKind(ch) {
	case DecElse, DecElif:
		return true
	}
	alt := n.ChildByFieldName("alternative")
	return alt != nil && alt.StartByte() == ch.StartByte() && alt.EndByte() == ch.EndByte()
}

// elseIfIn returns the if statement an else clause holds directly, the shape
// of `else if` in grammars with an else node (TypeScript, Rust), or nil for a
// plain else.
func (v *Visitor) elseIfIn(n *sitter.Node) *sitter.Node {
	for i := 0; i < int(n.NamedChildCount()); i++ {
		if ch := n.NamedChild(i); v.cognitiveKind(ch) == DecIf {
			return ch
		}
	}
	return nil
}

// continuesRun reports whether a boolean operator extends the run of its
// parent expression: `a && b && c` parses as `(a && b) && c`, one run.
func (v *Visitor) continuesRun(n *sitter.Node) bool {
	parent := n.Parent()
	if parent == nil || v.ad.Decision(parent) != DecLogical {
		return false
	}
	return v.logicalOperator(parent) == v.logicalOperator(n)
}

func (v *Visitor) logicalOperator(n *sitter.Node) string {
	if lo, ok := v.ad.(interface {
		LogicalOperator(*sitter.Node) string
	}); ok {
		return lo.LogicalOperator(n)
	}
	return ""
}
