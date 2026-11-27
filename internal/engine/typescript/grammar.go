package typescript

import (
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"
	tsTypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// grammar loads the TSX grammar once. TSX is a superset of TypeScript, so one
// grammar covers both .ts and .tsx files. Both the parsers and the node type
// tables ask for it, and each load crosses into C.
var grammar = sync.OnceValue(func() *sitter.Language {
	return sitter.NewLanguage(tsTypescript.LanguageTSX())
})
