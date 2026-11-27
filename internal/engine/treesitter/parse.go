package treesitter

import (
	"errors"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// rootSetter is implemented by the adapters that reuse the tree the runner
// parsed instead of parsing the file a second time for themselves.
type rootSetter interface {
	SetRootNode(*sitter.Node)
}

// Parsed is a parse tree and its root.
//
// The tree lives outside the Go heap and the garbage collector knows nothing
// about it: Close releases it, and every node read from it dies with it. One
// file is one Parsed, closed before the next is read.
type Parsed struct {
	tree *sitter.Tree
	Root *sitter.Node
}

// Parse reads src with the grammar of the adapter, and hands the tree over to
// the adapter so that it answers from it instead of parsing the file again.
func Parse(adapter LangAdapter, src []byte) (*Parsed, error) {
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(adapter.Language()); err != nil {
		return nil, err
	}
	tree := parser.Parse(src, nil)
	if tree == nil {
		return nil, errors.New("tree-sitter parsed no tree")
	}
	root := tree.RootNode()
	if rs, ok := adapter.(rootSetter); ok {
		rs.SetRootNode(root)
	}
	return &Parsed{tree: tree, Root: root}, nil
}

// Close releases the tree.
func (p *Parsed) Close() {
	if p == nil || p.tree == nil {
		return
	}
	p.tree.Close()
	p.tree = nil
	p.Root = nil
}

// OwnedTree is the tree an adapter parsed for itself, when it was used outside
// an analysis and no runner shared one with it.
//
// Something has to release a tree, and a node read from one dies with it: held
// by the adapter, the tree lives exactly as long as the source the adapter was
// given, which is as long as the nodes it hands out are read.
type OwnedTree struct {
	parsed *Parsed
}

// Root parses src with the grammar of the adapter and returns the root, or nil
// when the grammar refuses the source. The previous tree, if any, is released:
// its nodes answered for a source the adapter no longer holds.
func (o *OwnedTree) Root(adapter LangAdapter, src []byte) *sitter.Node {
	o.Release()
	parsed, err := Parse(adapter, src)
	if err != nil {
		return nil
	}
	o.parsed = parsed
	return parsed.Root
}

// Release closes the tree the adapter owns, if it parsed one.
func (o *OwnedTree) Release() {
	o.parsed.Close()
	o.parsed = nil
}
