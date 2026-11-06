package deps

import (
	"path/filepath"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/dependency"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// Language is the value the C++ engine writes in pb.File.ProgrammingLanguage.
const Language = "C++"

// FileDependencyResolver owns the two kinds of dependency a C++ file has.
//
// An #include names a file. It is first read from the directory of the
// including file, the one rule every compiler shares, then against the
// directories of the project as include roots: `#include "fmt/format.h"` is
// include/fmt/format.h when the project holds a single file ending that way.
// The real search path lives in the build system (compile_commands.json, -I
// flags), which a syntax-level engine does not read.
//
// A type reference names a class the way it is written, relative to the
// scope it is used from. It is looked up from that scope outward, the
// innermost candidate first, the way the compiler does it.
type FileDependencyResolver struct{}

var _ dependency.Resolver = (*FileDependencyResolver)(nil)

func NewFileDependencyResolver() *FileDependencyResolver { return &FileDependencyResolver{} }

func (r *FileDependencyResolver) ForFiles(files []*pb.File) dependency.ScopedResolver {
	resolver := &scopedFileDependencyResolver{
		paths:    make(map[string]string),
		suffixes: dependency.NewIndex(),
		types:    dependency.NewIndex(),
		includes: make(map[string]map[string]bool),
	}
	for _, file := range files {
		if file == nil || file.GetProgrammingLanguage() != Language || file.GetPath() == "" {
			continue
		}
		path := file.GetPath()
		resolver.paths[filepath.Clean(path)] = path
		segments := strings.Split(filepath.ToSlash(filepath.Clean(path)), "/")
		for i := range segments {
			resolver.suffixes.Add(strings.Join(segments[i:], "/"), path)
		}
		if file.Stmts == nil {
			continue
		}
		for _, class := range engine.GetClassesInFile(file) {
			if class != nil && class.Name != nil {
				resolver.types.Add(class.Name.GetQualified(), path)
			}
		}
		for _, dep := range engine.GetDependenciesInFile(file) {
			if dep != nil && dep.GetClassName() == "" && dep.GetNamespace() != "" {
				if resolver.includes[path] == nil {
					resolver.includes[path] = make(map[string]bool)
				}
				resolver.includes[path][dep.GetNamespace()] = true
			}
		}
	}
	return resolver
}

type scopedFileDependencyResolver struct {
	// cleaned path -> path as analyzed
	paths map[string]string
	// every trailing run of path segments -> files ending that way
	suffixes *dependency.Index
	// qualified class name -> files declaring it
	types *dependency.Index
	// file -> what it includes
	includes map[string]map[string]bool
}

var (
	_ dependency.ScopedResolver        = (*scopedFileDependencyResolver)(nil)
	_ dependency.LibraryTeller         = (*scopedFileDependencyResolver)(nil)
	_ dependency.StandardLibraryTeller = (*scopedFileDependencyResolver)(nil)
)

// Resolve claims every dependency of a C++ file, resolved or not: a header
// of the toolchain or of a package sits outside the analyzed scope by design.
func (r *scopedFileDependencyResolver) Resolve(source *pb.File, dep *pb.StmtExternalDependency) ([]string, bool) {
	if source == nil || dep == nil || source.GetProgrammingLanguage() != Language {
		return nil, false
	}
	if dep.GetClassName() != "" {
		return r.resolveType(dep), true
	}
	return r.resolveInclude(source, dep.GetNamespace()), true
}

func (r *scopedFileDependencyResolver) resolveInclude(source *pb.File, include string) []string {
	if include == "" || filepath.IsAbs(include) {
		return nil
	}
	target := filepath.Clean(filepath.Join(filepath.Dir(source.GetPath()), filepath.FromSlash(include)))
	if path := r.paths[target]; path != "" {
		return []string{path}
	}
	// an include root of the project: guessing is only acceptable when there
	// is nothing to guess between, and a relative include leaves nothing to
	// guess
	if dependency.IsRelative(include) {
		return nil
	}
	return r.suffixes.GetUnambiguous(strings.TrimPrefix(filepath.ToSlash(filepath.Clean(include)), "/"))
}

func (r *scopedFileDependencyResolver) resolveType(dep *pb.StmtExternalDependency) []string {
	name := dep.GetNamespace()
	for scope := dep.GetFrom(); scope != ""; scope = parentScope(scope) {
		if targets := r.types.Get(scope + "::" + name); len(targets) > 0 {
			return targets
		}
	}
	return r.types.Get(name)
}

func parentScope(name string) string {
	if i := strings.LastIndex(name, "::"); i > 0 {
		return name[:i]
	}
	return ""
}

// IsLibrary tells an include from a type reference: only a header the file
// includes is a library. A type that resolves to no class of the project,
// `spdlog::logger`, is what that header declares.
func (r *scopedFileDependencyResolver) IsLibrary(source *pb.File, module string) bool {
	return r.includes[source.GetPath()][module]
}

// IsStandardLibrary tells the headers of the C++ standard library, those of
// the C library it carries (<cstdio> as well as <stdio.h>), from the headers
// of a package.
func (r *scopedFileDependencyResolver) IsStandardLibrary(module string) bool {
	return standardHeaders[module]
}

var standardHeaders = func() map[string]bool {
	headers := map[string]bool{}
	for _, name := range strings.Fields(`
		algorithm any array atomic barrier bit bitset charconv chrono codecvt
		compare complex concepts condition_variable coroutine deque exception
		execution expected filesystem flat_map flat_set format forward_list
		fstream functional future generator initializer_list iomanip ios iosfwd
		iostream istream iterator latch limits list locale map mdspan memory
		memory_resource mutex new numbers numeric optional ostream print queue
		random ranges ratio regex scoped_allocator semaphore set shared_mutex
		source_location span spanstream sstream stack stacktrace stdexcept
		stdfloat stop_token streambuf string string_view strstream syncstream
		system_error thread tuple type_traits typeindex typeinfo unordered_map
		unordered_set utility valarray variant vector version
		cassert ccomplex cctype cerrno cfenv cfloat cinttypes ciso646 climits
		clocale cmath csetjmp csignal cstdalign cstdarg cstdbool cstddef cstdint
		cstdio cstdlib cstring ctgmath ctime cuchar cwchar cwctype`) {
		headers[name] = true
	}
	for _, name := range strings.Fields(`
		assert complex ctype errno fenv float inttypes iso646 limits locale math
		setjmp signal stdalign stdarg stdatomic stdbool stddef stdint stdio
		stdlib stdnoreturn string tgmath threads time uchar wchar wctype`) {
		headers[name+".h"] = true
	}
	return headers
}()
