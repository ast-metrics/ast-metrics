package csharp

import (
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tsCSharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
)

// grammar loads the C# grammar once. Both the parsers and the node type tables
// ask for it, and each load crosses into C.
var grammar = sync.OnceValue(func() *sitter.Language {
	return sitter.NewLanguage(tsCSharp.Language())
})
