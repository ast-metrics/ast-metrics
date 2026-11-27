package rust

import (
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tsRust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
)

// grammar loads the Rust grammar once. Both the parsers and the node type tables
// ask for it, and each load crosses into C.
var grammar = sync.OnceValue(func() *sitter.Language {
	return sitter.NewLanguage(tsRust.Language())
})
