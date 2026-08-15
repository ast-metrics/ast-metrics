package deps

import (
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/engine/dependency"
	"github.com/ast-metrics/ast-metrics/internal/engine/rust/module"
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

	crates := module.NewCache()
	for _, file := range files {
		path := file.GetPath()
		if file == nil || path == "" || file.GetProgrammingLanguage() != Language {
			continue
		}
		located, found := crates.Locate(path)
		if !found {
			continue
		}
		location := crateModule{crate: located.Crate, module: located.Module}
		locations[path] = location
		modules.Add(moduleKey(location.crate, location.module), path)
		if located.Name != "" {
			crateNames[located.Name] = location.crate
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

	// A crate of the workspace, named as its Cargo.toml declares it: a sibling
	// crate, or the importing crate itself, which is how the engine spells a
	// `crate::` path once it has read it.
	if crate, found := r.crateNames[segments[0]]; found {
		return crate, segments[1:], true
	}
	// Otherwise the path is read from the crate root, which is how the 2015
	// edition spells `use module::Item`. An external crate simply finds no
	// module of that name and resolves to nothing.
	return from.crate, segments, true
}

func splitPath(path string) []string { return module.SplitPath(path) }
