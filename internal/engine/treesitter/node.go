package treesitter

import (
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// SameNode reports whether two values stand for the same node of the same tree.
//
// The bindings hand out a fresh Go value for every lookup, so two reads of one
// node are never equal as pointers: a walk that has to recognize the node it
// started from compares them here instead of with ==.
func SameNode(a, b *sitter.Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Id() == b.Id()
}
