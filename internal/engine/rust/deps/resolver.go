package deps

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/engine/dependency"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// Language is the value the Rust engine writes in pb.File.ProgrammingLanguage.
const Language = "Rust"

// FileDependencyResolver owns Rust path resolution.
//
// Rust names modules, not files, and the mapping between the two is a
// convention of the compiler: src/a.rs and src/a/mod.rs are both the module
// `a`, and src/lib.rs or src/main.rs is the crate root. Each file is therefore
// indexed under the module path it implements, and a `use` path is read
// against the crate it starts from: `crate::` from the root, `self::` and
// `super::` from the module doing the importing, and a leading crate name from
// the sibling crate of the same name in a workspace.
type FileDependencyResolver struct{}

var _ dependency.Resolver = (*FileDependencyResolver)(nil)

func NewFileDependencyResolver() *FileDependencyResolver {
	return &FileDependencyResolver{}
}

// crateModule locates a file in the module tree: which crate it belongs to,
// and the path of the module it implements inside that crate.
type crateModule struct {
	crate  string
	module string
}

func (r *FileDependencyResolver) ForFiles(files []*pb.File) dependency.ScopedResolver {
	modules := dependency.NewIndex()
	locations := make(map[string]crateModule)
	crateNames := make(map[string]string)

	crates := newCrateCache()
	for _, file := range files {
		path := file.GetPath()
		if file == nil || path == "" || file.GetProgrammingLanguage() != Language {
			continue
		}
		location, found := crates.locate(path)
		if !found {
			continue
		}
		locations[path] = location
		modules.Add(moduleKey(location.crate, location.module), path)
		if name := crates.nameOf(location.crate); name != "" {
			crateNames[name] = location.crate
		}
	}
	return &scopedFileDependencyResolver{
		modules:    modules,
		locations:  locations,
		crateNames: crateNames,
	}
}

func moduleKey(crate, module string) string {
	return crate + "\x00" + module
}

type scopedFileDependencyResolver struct {
	modules    *dependency.Index
	locations  map[string]crateModule
	crateNames map[string]string
}

var _ dependency.ScopedResolver = (*scopedFileDependencyResolver)(nil)

func (r *scopedFileDependencyResolver) Resolve(source *pb.File, dep *pb.StmtExternalDependency) ([]string, bool) {
	if source == nil || dep == nil || source.GetProgrammingLanguage() != Language {
		return nil, false
	}

	// Every dependency of a Rust file is claimed, resolved or not: `use
	// std::fmt::Display` and `use serde::Serialize` name crates that are not
	// part of the analyzed sources, and letting them fall through to name
	// matching would bind them to whatever struct carries that name.
	from, located := r.locations[source.GetPath()]
	if !located {
		return nil, true
	}

	// The dependency was split into a module part and a leaf name; the path as
	// written is the two joined back together, since the split point is a
	// guess that the module tree is about to correct.
	path := dep.GetNamespace()
	if leaf := dep.GetClassName(); leaf != "" {
		path = strings.TrimSuffix(path, "::") + "::" + leaf
		path = strings.TrimPrefix(path, "::")
	}

	crate, segments, understood := r.rebase(from, path)
	if !understood {
		return nil, true
	}

	// A use path ends on an item, not on a module: `crate::model::User` names
	// the struct User inside the module `model`. Which trailing segments are
	// items is only known to the compiler, so the longest prefix that is a
	// module wins.
	for end := len(segments); end >= 0; end-- {
		module := strings.Join(segments[:end], "::")
		if targets := r.modules.Get(moduleKey(crate, module)); len(targets) > 0 {
			return targets, true
		}
	}
	return nil, true
}

// rebase turns a use path into a crate and the module segments to walk from
// its root, resolving the three ways Rust can anchor a path.
func (r *scopedFileDependencyResolver) rebase(from crateModule, path string) (string, []string, bool) {
	segments := splitPath(path)
	if len(segments) == 0 {
		return "", nil, false
	}

	current := splitPath(from.module)
	switch segments[0] {
	case "crate":
		return from.crate, segments[1:], true
	case "self":
		return from.crate, append(append([]string{}, current...), segments[1:]...), true
	case "super":
		// Each `super` climbs one module, and they can be chained.
		rest := segments
		for len(rest) > 0 && rest[0] == "super" {
			if len(current) == 0 {
				return "", nil, false
			}
			current = current[:len(current)-1]
			rest = rest[1:]
		}
		return from.crate, append(append([]string{}, current...), rest...), true
	}

	// A sibling crate of the workspace, named as its Cargo.toml declares it.
	if crate, found := r.crateNames[segments[0]]; found && crate != from.crate {
		return crate, segments[1:], true
	}
	// Otherwise the path is read from the crate root, which is how the 2015
	// edition spells `use module::Item`. An external crate simply finds no
	// module of that name and resolves to nothing.
	return from.crate, segments, true
}

func splitPath(path string) []string {
	segments := make([]string, 0, 4)
	for _, segment := range strings.Split(path, "::") {
		if segment = strings.TrimSpace(segment); segment != "" {
			segments = append(segments, segment)
		}
	}
	return segments
}

// crateCache maps a file to its place in the module tree, remembering the
// Cargo.toml files found while walking up. A workspace holds several crates,
// so the search stops at the nearest manifest rather than at a single root.
type crateCache struct {
	// nameByRoot maps a crate root directory to the package name declared in
	// its Cargo.toml, with an empty value marking a directory known to hold no
	// readable manifest.
	nameByRoot map[string]string
}

func newCrateCache() *crateCache {
	return &crateCache{nameByRoot: make(map[string]string)}
}

func (c *crateCache) locate(path string) (crateModule, bool) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = filepath.Clean(path)
	}
	// The source directory of the crate, which the module paths are relative
	// to. Anything outside a src/ directory is not part of a crate laid out
	// the way cargo expects.
	sourceRoot := sourceRootOf(absolute)
	if sourceRoot == "" {
		return crateModule{}, false
	}
	crate := filepath.Dir(sourceRoot)
	c.nameOf(crate)
	return crateModule{crate: crate, module: moduleOf(sourceRoot, absolute)}, true
}

func (c *crateCache) nameOf(crate string) string {
	if name, known := c.nameByRoot[crate]; known {
		return name
	}
	name := ""
	if content, err := os.ReadFile(filepath.Join(crate, "Cargo.toml")); err == nil {
		name = packageNameIn(string(content))
	}
	c.nameByRoot[crate] = name
	return name
}

// sourceRootOf returns the innermost "src" directory above a file, which is
// where a crate's module tree starts.
func sourceRootOf(path string) string {
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

// moduleOf reads the module path a file implements from its place under the
// source root: lib.rs and main.rs are the crate root, a/mod.rs and a.rs are
// both the module `a`.
func moduleOf(sourceRoot, path string) string {
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

// packageNameIn reads the package name out of a Cargo.toml. Only the name of
// the [package] section is needed, so the file is scanned rather than parsed.
// Cargo lets a crate be named with dashes and referred to with underscores.
func packageNameIn(content string) string {
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
