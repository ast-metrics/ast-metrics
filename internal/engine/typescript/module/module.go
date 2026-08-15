// Package module names a TypeScript module the way an import names it: by its
// path from the root of the package, without extension.
//
// TypeScript has no namespaces: a module is a file, and an import names it by
// a path relative to the importing file, "../clearing/clearing". Two files
// naming the same module spell it two different ways, and the module names
// itself by its bare file name. The path from the root of the package is the
// one spelling everyone agrees on: "src/clearing/clearing".
package module

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// manifests are the files marking the root of a package. The nearest one wins,
// so that a workspace of several packages names the modules of each from its
// own root.
var manifests = []string{"package.json", "tsconfig.json", "jsconfig.json"}

// extensions are the ones an import leaves out, or substitutes, longest first
// so that ".d.ts" is stripped before ".ts".
var extensions = []string{".d.ts", ".d.mts", ".d.cts", ".ts", ".tsx", ".mts", ".cts", ".js", ".jsx", ".mjs", ".cjs", ".vue"}

// Cache resolves files to module paths, remembering where the packages are.
// It is safe for concurrent use: the files of a project are parsed in parallel.
type Cache struct {
	mutex sync.RWMutex
	// isRoot tells whether a directory holds a manifest.
	isRoot map[string]bool
}

func NewCache() *Cache {
	return &Cache{isRoot: make(map[string]bool)}
}

// Location is the place of a file in its package.
type Location struct {
	// Root is the directory holding the manifest of the package.
	Root string
	// Module is the path of the module from the root, without extension, and
	// with a trailing index left out: src/shared/index.ts is the module
	// src/shared, which is how `import "../shared"` names it.
	Module string
}

// Locate places a file in its package. A file under no manifest is not
// located, and keeps the bare name it has.
func (c *Cache) Locate(path string) (Location, bool) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = filepath.Clean(path)
	}
	root := c.rootAbove(filepath.Dir(absolute))
	if root == "" {
		return Location{}, false
	}
	relative, err := filepath.Rel(root, absolute)
	if err != nil {
		return Location{}, false
	}
	return Location{Root: root, Module: moduleName(filepath.ToSlash(relative))}, true
}

// Anchor spells the specifier of an import written in the located file the
// way the module it names is located: "../clearing/clearing" written in
// src/artifact/entrypoint.ts names src/clearing/clearing. A specifier that is
// not relative, a package of node_modules or an alias of the compiler, and one
// climbing out of the package are returned as written.
func Anchor(from Location, specifier string) string {
	if !isRelative(specifier) {
		return specifier
	}
	directory := filepath.Dir(filepath.Join(from.Root, filepath.FromSlash(from.Module+".ts")))
	joined := filepath.Clean(filepath.Join(directory, filepath.FromSlash(specifier)))
	relative, err := filepath.Rel(from.Root, joined)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return specifier
	}
	return moduleName(filepath.ToSlash(relative))
}

func isRelative(specifier string) bool {
	return specifier == "." || specifier == ".." ||
		strings.HasPrefix(specifier, "./") || strings.HasPrefix(specifier, "../")
}

// moduleName strips the extension and the trailing index of a slash-separated
// path relative to the root.
func moduleName(relative string) string {
	for _, extension := range extensions {
		if strings.HasSuffix(relative, extension) {
			relative = strings.TrimSuffix(relative, extension)
			break
		}
	}
	if relative == "index" {
		return "."
	}
	relative = strings.TrimSuffix(relative, "/index")
	if relative == "." || relative == "" {
		return "."
	}
	return relative
}

// rootAbove returns the nearest directory, from the given one up, holding a
// manifest, and an empty string when there is none.
func (c *Cache) rootAbove(directory string) string {
	for current := directory; ; {
		if c.rootAt(current) {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

func (c *Cache) rootAt(directory string) bool {
	look := func() bool {
		for _, manifest := range manifests {
			if _, err := os.Stat(filepath.Join(directory, manifest)); err == nil {
				return true
			}
		}
		return false
	}
	// The nil cache looks every time, remembering nothing.
	if c == nil {
		return look()
	}
	c.mutex.RLock()
	answer, cached := c.isRoot[directory]
	c.mutex.RUnlock()
	if cached {
		return answer
	}
	answer = look()
	c.mutex.Lock()
	c.isRoot[directory] = answer
	c.mutex.Unlock()
	return answer
}
