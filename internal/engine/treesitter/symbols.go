package treesitter

import (
	"sync"

	sitter "github.com/smacker/go-tree-sitter"
)

// Classifying a node by the name of its type is what every walk does on every
// node, several times: is it a statement, a decision, a class, a function, a
// scope boundary. Asking `Node.Type()` is the most expensive way to ask: it
// crosses into C and copies the name into a fresh Go string, every time, so the
// walks spent a fifth of the analysis there and fed the garbage collector with
// strings nobody keeps.
//
// The grammar answers the same question with an integer. Every node carries a
// symbol id, the ids of one language form a small dense range, and
// `Node.Type()` is defined as the name of the node's symbol. Turning the node
// names an adapter declares into a table indexed by that id therefore keeps the
// exact same answers, while making a classification an array lookup with no
// allocation and no string comparison.
//
// A grammar an adapter does not declare a language for still works: the tables
// stay empty and the classification falls back to the node type name.

// symbolTable maps the symbol ids of one grammar onto a value. The zero value of
// T is the answer for any id the adapter said nothing about, which is what a
// comparison against a list of node type names would have returned.
type symbolTable[T any] struct {
	byID []T
}

// newSymbolTable turns a mapping keyed by node type name into one keyed by
// symbol id. A name the grammar does not know simply never matches.
func newSymbolTable[T any](lang *sitter.Language, byName map[string]T) symbolTable[T] {
	if lang == nil || len(byName) == 0 {
		return symbolTable[T]{}
	}

	count := int(lang.SymbolCount())
	table := symbolTable[T]{byID: make([]T, count)}
	for id := 0; id < count; id++ {
		if value, ok := byName[lang.SymbolName(sitter.Symbol(id))]; ok {
			table.byID[id] = value
		}
	}
	return table
}

func (t symbolTable[T]) ready() bool {
	return len(t.byID) > 0
}

// at returns the value declared for the node's type. Ids outside the table are
// the built-in symbols a grammar file does not describe (ERROR carries 65535),
// and no adapter declares those.
func (t symbolTable[T]) at(n *sitter.Node) T {
	var none T
	if n == nil {
		return none
	}
	id := int(n.Symbol())
	if id < 0 || id >= len(t.byID) {
		return none
	}
	return t.byID[id]
}

// TypeSet answers whether a node is one of the node types of a grammar, by
// symbol id rather than by name. Adapters use it for the structural questions
// asked on every node of every walk.
type TypeSet struct {
	// Language is the grammar the node types belong to. It is a function
	// because a grammar is loaded by a call, and a package-level set must not
	// depend on the initialization order of two packages.
	Language func() *sitter.Language
	// Types lists the node types of the set.
	Types []string

	once   sync.Once
	table  symbolTable[bool]
	byName map[string]bool
}

func (s *TypeSet) index() {
	s.once.Do(func() {
		s.byName = make(map[string]bool, len(s.Types))
		for _, t := range s.Types {
			s.byName[t] = true
		}
		if s.Language != nil {
			s.table = newSymbolTable(s.Language(), s.byName)
		}
	})
}

// Has reports whether the node is one of the declared types.
func (s *TypeSet) Has(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	s.index()
	if s.table.ready() {
		return s.table.at(n)
	}
	return s.byName[n.Type()]
}
