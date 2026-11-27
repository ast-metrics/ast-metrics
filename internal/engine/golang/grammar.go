package golang

import (
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tsGo "github.com/tree-sitter/tree-sitter-go/bindings/go"
)

// grammar loads the Go grammar once. Both the parsers and the node type tables
// ask for it, and each load crosses into C.
var grammar = sync.OnceValue(func() *sitter.Language {
	return sitter.NewLanguage(tsGo.Language())
})
