package php

import (
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tsPhp "github.com/tree-sitter/tree-sitter-php/bindings/go"
)

// grammar loads the PHP grammar once. Both the parsers and the node type tables
// ask for it, and each load crosses into C.
var grammar = sync.OnceValue(func() *sitter.Language {
	return sitter.NewLanguage(tsPhp.LanguagePHP())
})
