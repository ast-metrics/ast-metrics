// Package module locates a Rust file in the module tree of its crate: which
// crate it belongs to, how that crate is named, and the path of the module the
// file implements.
//
// Rust names modules, not files, and the mapping between the two is a
// convention of the compiler: src/a.rs and src/a/mod.rs are both the module
// `a`, and src/lib.rs or src/main.rs is the crate root.
package module

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Location is the place of a file in the module tree.
type Location struct {
	// Crate is the directory holding the Cargo.toml of the crate.
	Crate string
	// Name is the name the Cargo.toml gives the crate, spelled with underscores
	// as a use path spells it, and empty when the crate has no readable manifest.
	Name string
	// Module is the path of the module the file implements, "a::b" for
	// src/a/b.rs and empty for the crate root.
	Module string
}

// Path returns the module path a use statement names the module by, from the
// name of the crate: "demo::a::b" for src/a/b.rs of the crate demo, and "demo"
// for its root. A crate without a name is called "crate", the way it names
// itself.
func (l Location) Path() string {
	name := l.Name
	if name == "" {
		name = "crate"
	}
	if l.Module == "" {
		return name
	}
	return name + "::" + l.Module
}

// Cache locates files, remembering the Cargo.toml files found while walking up.
// A workspace holds several crates, so the search stops at the nearest manifest
// rather than at a single root.
//
// It is safe for concurrent use: the files of a project are parsed in parallel.
type Cache struct {
	mutex sync.RWMutex
	// nameByRoot maps a crate root directory to the package name declared in
	// its Cargo.toml, with an empty value marking a directory known to hold no
	// readable manifest.
	nameByRoot map[string]string
}

func NewCache() *Cache {
	return &Cache{nameByRoot: make(map[string]string)}
}

// Locate places a file in the module tree. Anything outside a src/ directory
// is not part of a crate laid out the way cargo expects, and is not located.
func (c *Cache) Locate(path string) (Location, bool) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = filepath.Clean(path)
	}
	sourceRoot := SourceRootOf(absolute)
	if sourceRoot == "" {
		return Location{}, false
	}
	crate := filepath.Dir(sourceRoot)
	return Location{Crate: crate, Name: c.NameOf(crate), Module: ModuleOf(sourceRoot, absolute)}, true
}

// NameOf returns the package name declared by the Cargo.toml of a crate root,
// and an empty string when it holds none. The nil cache reads it every time.
func (c *Cache) NameOf(crate string) string {
	if c == nil {
		return readPackageName(crate)
	}
	c.mutex.RLock()
	name, known := c.nameByRoot[crate]
	c.mutex.RUnlock()
	if known {
		return name
	}
	name = readPackageName(crate)
	c.mutex.Lock()
	c.nameByRoot[crate] = name
	c.mutex.Unlock()
	return name
}

func readPackageName(crate string) string {
	content, err := os.ReadFile(filepath.Join(crate, "Cargo.toml"))
	if err != nil {
		return ""
	}
	return PackageNameIn(string(content))
}

// SourceRootOf returns the innermost "src" directory above a file, which is
// where a crate's module tree starts, and an empty string when there is none.
func SourceRootOf(path string) string {
	for current := filepath.Dir(path); ; {
		if filepath.Base(current) == "src" {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

// ModuleOf reads the module path a file implements from its place under the
// source root: lib.rs and main.rs are the crate root, a/mod.rs and a.rs are
// both the module `a`.
func ModuleOf(sourceRoot, path string) string {
	relative, err := filepath.Rel(sourceRoot, path)
	if err != nil {
		return ""
	}
	segments := strings.Split(filepath.ToSlash(relative), "/")
	last := len(segments) - 1
	switch strings.TrimSuffix(segments[last], ".rs") {
	case "lib", "main", "mod":
		segments = segments[:last]
	default:
		segments[last] = strings.TrimSuffix(segments[last], ".rs")
	}
	return strings.Join(segments, "::")
}

// PackageNameIn reads the package name out of a Cargo.toml. Only the name of
// the [package] section is needed, so the file is scanned rather than parsed.
// Cargo lets a crate be named with dashes and referred to with underscores.
func PackageNameIn(content string) string {
	inPackage := false
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			inPackage = line == "[package]"
			continue
		}
		if !inPackage {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		// The key is compared whole, so that "names" does not pass for "name".
		if !found || strings.TrimSpace(key) != "name" {
			continue
		}
		return strings.ReplaceAll(strings.Trim(strings.TrimSpace(value), `"'`), "-", "_")
	}
	return ""
}

// Anchor spells a use path written in the given module the way it reads from
// outside: `crate::a` becomes "<crate>::a", `self::a` and `super::a` are
// resolved from the module, and any other path, an external crate or a sibling
// module named the 2015 way, is returned as written. A `super` climbing past
// the root is not understood, and returned as written as well.
func Anchor(from Location, path string) string {
	segments := SplitPath(path)
	if len(segments) == 0 {
		return path
	}
	current := SplitPath(from.Module)
	var resolved []string
	switch segments[0] {
	case "crate":
		resolved = segments[1:]
	case "self":
		resolved = append(append([]string{}, current...), segments[1:]...)
	case "super":
		rest := segments
		for len(rest) > 0 && rest[0] == "super" {
			if len(current) == 0 {
				return path
			}
			current = current[:len(current)-1]
			rest = rest[1:]
		}
		resolved = append(append([]string{}, current...), rest...)
	default:
		return path
	}
	return Location{Name: from.Name, Module: strings.Join(resolved, "::")}.Path()
}

// SplitPath cuts a use path on its "::", leaving out the empty segments.
func SplitPath(path string) []string {
	segments := make([]string, 0, 4)
	for _, segment := range strings.Split(path, "::") {
		if segment = strings.TrimSpace(segment); segment != "" {
			segments = append(segments, segment)
		}
	}
	return segments
}
