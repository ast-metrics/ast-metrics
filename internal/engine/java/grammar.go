package java

import (
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tsJava "github.com/tree-sitter/tree-sitter-java/bindings/go"
)

// grammar loads the Java grammar once. Both the parsers and the node type tables
// ask for it, and each load crosses into C.
var grammar = sync.OnceValue(func() *sitter.Language {
	return sitter.NewLanguage(tsJava.Language())
})
