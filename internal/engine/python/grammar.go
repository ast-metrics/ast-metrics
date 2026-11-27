package python

import (
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tsPython "github.com/tree-sitter/tree-sitter-python/bindings/go"
)

// grammar loads the Python grammar once. Both the parsers and the node type
// tables ask for it, and each load crosses into C.
var grammar = sync.OnceValue(func() *sitter.Language {
	return sitter.NewLanguage(tsPython.Language())
})
