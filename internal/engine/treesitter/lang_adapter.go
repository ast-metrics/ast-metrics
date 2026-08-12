package treesitter

import (
	sitter "github.com/smacker/go-tree-sitter"
)

type ImportItem struct {
	Module string // e.g., "pkg.sub" (for `from pkg.sub import X`) or full module for `import pkg.sub`
	Name   string // imported symbol (empty for plain `import pkg.sub`)
}

type LangAdapter interface {
	Language() *sitter.Language

	// structure
	IsModule(*sitter.Node) bool
	IsClass(*sitter.Node) bool
	IsFunction(*sitter.Node) bool

	// attributes
	NodeName(*sitter.Node) string
	NodeBody(*sitter.Node) *sitter.Node
	NodeParams(*sitter.Node) *sitter.Node
	ModuleNameFromPath(path string) string
	AttachQualified(parentClass, fn string) string
	EachChildBody(n *sitter.Node, yield func(*sitter.Node))
	EachParamIdent(params *sitter.Node, yield func(string))

	// Decision classifies a grammar node against the shared cyclomatic
	// complexity model documented in decision.go. It is called on every node of
	// a scope, so it must answer on the node alone, without assuming anything
	// about the order in which nodes are seen.
	Decision(n *sitter.Node) DecisionKind

	// Statement classifies a grammar node against the shared logical-lines
	// model documented in statement.go. Like Decision, it is called on every
	// node of the tree and must answer on the node alone.
	Statement(n *sitter.Node) StatementKind

	// imports
	Imports(n *sitter.Node) []ImportItem
}
