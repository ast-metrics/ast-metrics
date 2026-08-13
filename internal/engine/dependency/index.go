package dependency

import (
	"sort"

	pb "github.com/ast-metrics/ast-metrics/pb"
)

// QualifiedOrShort returns the name a declaration is looked up by. The
// qualified form is the one an import spells out, and the short form is all
// there is when a file declares no namespace.
func QualifiedOrShort(name *pb.Name) string {
	if qualified := name.GetQualified(); qualified != "" {
		return qualified
	}
	return name.GetShort()
}

// Index maps a resolution key to the files that answer it.
//
// The key is whatever unit the language's import syntax names: a package
// import path in Go, a fully qualified type in Java, a namespace in C#, a
// module path in Python and Rust. Several files can answer one key, so the
// index keeps them all instead of letting the last writer win: the files of a
// Go package are equally the target of an import of that package.
//
// Results are sorted, which is what makes the graph reproducible. Files reach
// the index in the order the parsers happen to finish, and that order changes
// between two runs on the same sources.
type Index struct {
	entries map[string]map[string]struct{}
}

func NewIndex() *Index {
	return &Index{entries: make(map[string]map[string]struct{})}
}

func (index *Index) Add(key, path string) {
	if key == "" || path == "" {
		return
	}
	if index.entries[key] == nil {
		index.entries[key] = make(map[string]struct{})
	}
	index.entries[key][path] = struct{}{}
}

// Get returns the files registered under key, sorted.
func (index *Index) Get(key string) []string {
	if key == "" {
		return nil
	}
	paths := index.entries[key]
	if len(paths) == 0 {
		return nil
	}
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

// GetUnambiguous returns the files registered under key only when the key
// designates a single file. It backs the suffix lookups used when a project
// layout hides the real root: guessing is only acceptable when there is
// nothing to guess between.
func (index *Index) GetUnambiguous(key string) []string {
	if paths := index.Get(key); len(paths) == 1 {
		return paths
	}
	return nil
}

func (index *Index) Has(key string) bool {
	return len(index.entries[key]) > 0
}
