// Package grammar provides the tree-sitter PHP grammar (v0.23.3) that supports
// PHP 8.4+ syntax including property hooks, asymmetric visibility, and other
// modern PHP features.
package grammar

// #cgo CFLAGS: -I. -Itree_sitter
// #include "parser.h"
// TSLanguage *tree_sitter_php();
import "C"
import (
	"unsafe"

	sitter "github.com/smacker/go-tree-sitter"
)

// GetLanguage returns the tree-sitter language for PHP (grammar v0.23.3).
func GetLanguage() *sitter.Language {
	ptr := unsafe.Pointer(C.tree_sitter_php())
	return sitter.NewLanguage(ptr)
}
