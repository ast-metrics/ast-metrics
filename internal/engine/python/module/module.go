// Package module resolves the dotted path of a Python module from where it sits
// on disk, the path an import statement names it by.
package module

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Cache resolves files to module paths, remembering which directories are
// packages and which hold a project, so that a chain is only walked once.
//
// It is safe for concurrent use: the files of a project are parsed in parallel.
type Cache struct {
	mutex sync.RWMutex
	// isPackage tells whether a directory holds an __init__.py.
	isPackage map[string]bool
	// isProject tells whether a directory holds the manifest of a project.
	isProject map[string]bool
}

func NewCache() *Cache {
	return &Cache{isPackage: make(map[string]bool), isProject: make(map[string]bool)}
}

// ModuleOf returns the dotted path of the module a file implements.
//
// A package is a directory holding an __init__.py, and the module path of a
// file is the chain of packages above it followed by its own name:
// company/product/artifact/entrypoint.py reads company.product.artifact.entrypoint
// as long as every directory on the way is a package. An __init__.py is the
// package holding it, not a module of its own: `import pkg` loads pkg/__init__.py.
//
// A namespace package (PEP 420) has no __init__.py, and the chain then stops
// short of the top. The directories are then read from the root of the
// project, the nearest directory holding a pyproject.toml, a setup.py or a
// setup.cfg, leaving out the src/ directory of the src layout, since the
// imports are written from there. A file under neither is named by its bare
// name, which is all it has.
func (c *Cache) ModuleOf(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = filepath.Clean(path)
	}

	segments := []string{}
	if base := strings.TrimSuffix(filepath.Base(absolute), filepath.Ext(absolute)); base != "__init__" {
		segments = append(segments, base)
	}
	current := filepath.Dir(absolute)
	for c.packageAt(current) {
		segments = append([]string{filepath.Base(current)}, segments...)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	// current is the first directory that is not a package: the top of the
	// chain, or the directory of the file when there is no chain at all.
	if root := c.projectRootAbove(current); root != "" {
		relative, err := filepath.Rel(root, current)
		if err == nil && relative != "." {
			above := strings.Split(filepath.ToSlash(relative), "/")
			if above[0] == "src" {
				above = above[1:]
			}
			segments = append(above, segments...)
		}
	}
	return strings.Join(segments, ".")
}

// Rebase turns the specifier of an import written in the given module into an
// absolute module path. A specifier with no leading dot is already absolute;
// each leading dot climbs one package from the one holding the importing
// module. isPackage tells whether the module is a package itself, an
// __init__.py, in which case the first dot names the module and not its
// parent. A specifier climbing past the top of the tree names something
// outside the sources, and is reported as not understood.
func Rebase(module string, isPackage bool, specifier string) (string, bool) {
	level := len(specifier) - len(strings.TrimLeft(specifier, "."))
	if level == 0 {
		return specifier, true
	}
	if module == "" {
		return "", false
	}
	segments := strings.Split(module, ".")
	if !isPackage {
		segments = segments[:len(segments)-1]
	}
	if level-1 > len(segments) {
		return "", false
	}
	segments = segments[:len(segments)-(level-1)]

	base := strings.Join(segments, ".")
	relative := strings.TrimLeft(specifier, ".")
	switch {
	case relative == "":
		return base, true
	case base == "":
		return relative, true
	}
	return base + "." + relative, true
}

func (c *Cache) packageAt(directory string) bool {
	return c.remember(func(c *Cache) map[string]bool { return c.isPackage }, directory, func() bool {
		_, err := os.Stat(filepath.Join(directory, "__init__.py"))
		return err == nil
	})
}

// projectRootAbove returns the nearest directory, from the given one up, that
// holds the manifest of a project, and an empty string when there is none.
func (c *Cache) projectRootAbove(directory string) string {
	for current := directory; ; {
		if c.projectAt(current) {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

func (c *Cache) projectAt(directory string) bool {
	return c.remember(func(c *Cache) map[string]bool { return c.isProject }, directory, func() bool {
		for _, manifest := range []string{"pyproject.toml", "setup.py", "setup.cfg"} {
			if _, err := os.Stat(filepath.Join(directory, manifest)); err == nil {
				return true
			}
		}
		return false
	})
}

// remember answers a question about a directory once. The nil cache looks
// every time, remembering nothing: a caller parsing a single file has nothing
// to share the answer with. Two parsers reaching the same unknown directory
// both look, which costs one stat, where holding the lock across it would
// serialize every directory behind the slowest one.
func (c *Cache) remember(table func(*Cache) map[string]bool, directory string, look func() bool) bool {
	if c == nil {
		return look()
	}
	c.mutex.RLock()
	answer, cached := table(c)[directory]
	c.mutex.RUnlock()
	if cached {
		return answer
	}
	answer = look()
	c.mutex.Lock()
	table(c)[directory] = answer
	c.mutex.Unlock()
	return answer
}
