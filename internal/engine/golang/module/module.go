// Package module resolves the import path of a Go package from where it sits on
// disk.
//
// A Go package is a directory, and its import path is the module path declared
// in go.mod followed by the path of the directory relative to that go.mod. That
// is the rule the toolchain applies, and the only way to name a package the
// same way an import statement names it.
package module

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Cache resolves directories to import paths, remembering the go.mod files
// found on the way up. One repository can hold several modules, so the lookup
// stops at the nearest go.mod rather than at a single project-wide root.
//
// It is safe for concurrent use: the files of a project are parsed in parallel.
type Cache struct {
	mutex sync.RWMutex
	// modulePathByRoot maps the directory holding a go.mod to its module path,
	// with an empty value marking a directory known to hold no readable go.mod.
	modulePathByRoot map[string]string
}

func NewCache() *Cache {
	return &Cache{modulePathByRoot: make(map[string]string)}
}

// ImportPathOf returns the import path of the package held by a directory, and
// an empty string when no go.mod stands above it. The nil cache answers the
// same, remembering nothing: a caller parsing a single file has nothing to
// share the answer with.
func (c *Cache) ImportPathOf(directory string) string {
	absolute, err := filepath.Abs(directory)
	if err != nil {
		absolute = filepath.Clean(directory)
	}

	for current := absolute; ; {
		if modulePath := c.modulePathAt(current); modulePath != "" {
			relative, err := filepath.Rel(current, absolute)
			if err != nil {
				return ""
			}
			relative = filepath.ToSlash(relative)
			if relative == "." {
				return modulePath
			}
			return modulePath + "/" + relative
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

func (c *Cache) modulePathAt(directory string) string {
	if c == nil {
		return readModulePath(directory)
	}

	c.mutex.RLock()
	modulePath, known := c.modulePathByRoot[directory]
	c.mutex.RUnlock()
	if known {
		return modulePath
	}

	// Two parsers reaching the same unknown directory both read the file. That
	// costs one read, where holding the lock across the read would serialize
	// every directory of the project behind the slowest disk answer.
	modulePath = readModulePath(directory)

	c.mutex.Lock()
	c.modulePathByRoot[directory] = modulePath
	c.mutex.Unlock()
	return modulePath
}

// readModulePath returns the module path declared by the go.mod of a directory,
// and an empty string when it holds none.
func readModulePath(directory string) string {
	content, err := os.ReadFile(filepath.Join(directory, "go.mod"))
	if err != nil {
		return ""
	}
	return modulePathIn(string(content))
}

// modulePathIn reads the module path out of a go.mod. Only the module directive
// is needed, so the file is scanned rather than parsed.
func modulePathIn(content string) string {
	for _, line := range strings.Split(content, "\n") {
		// Splitting on spaces keeps "modules" from passing for "module" and
		// drops any trailing comment along the way.
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "module" {
			continue
		}
		return strings.Trim(fields[1], `"`)
	}
	return ""
}
