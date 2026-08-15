package file

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/yargevad/filepathx"
)

type FileList struct {
	Files            []string
	FilesByDirectory map[string][]string
}

// FileDiscovery caches multi-extension search results so that
// multiple calls to Search() with different extensions reuse a single walk.
type FileDiscovery struct {
	results map[string]FileList
}

// Precompute walks all source paths once for the given extensions
// and caches the results.
func (fd *FileDiscovery) Precompute(finder Finder, extensions []string) {
	fd.results = finder.SearchMultiple(extensions)
}

// Get returns the cached FileList for the given extension, or nil if not cached.
func (fd *FileDiscovery) Get(ext string) *FileList {
	if fd.results == nil {
		return nil
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	if fl, ok := fd.results[ext]; ok {
		return &fl
	}
	return nil
}

type Finder struct {
	Configuration configuration.Configuration
	// Discovery is an optional shared cache for multi-extension search.
	// When set, Search() will check it before doing a full glob.
	Discovery *FileDiscovery
	// projectRoot overrides the directory used as the reference for exclude
	// pattern matching. Empty means "use the working directory"; only tests
	// set it.
	projectRoot string
}

// resolveProjectRoot returns the directory exclude patterns are matched
// against: the working directory (where the configuration file lives),
// unless overridden for tests.
func (r Finder) resolveProjectRoot() string {
	if r.projectRoot != "" {
		return r.projectRoot
	}
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return wd
}

// scopes returns the analyzed sources paired with the configuration that
// governs them, without the duplicates a configuration is free to declare. A
// Finder whose configuration has no resolved scope falls back to the root
// configuration governing every source, which is what a single project needs.
func (r Finder) scopes() []configuration.Scope {
	declared := r.Configuration.Scopes
	if len(declared) == 0 {
		declared = make([]configuration.Scope, 0, len(r.Configuration.SourcesToAnalyzePath))
		for _, path := range r.Configuration.SourcesToAnalyzePath {
			declared = append(declared, configuration.Scope{Path: path, Configuration: &r.Configuration})
		}
	}

	seen := make(map[string]bool, len(declared))
	scopes := make([]configuration.Scope, 0, len(declared))
	for _, scope := range declared {
		key, err := filepath.Abs(strings.TrimRight(scope.Path, "/"))
		if err != nil {
			continue
		}
		key = filepath.Clean(key)
		if seen[key] {
			continue
		}
		seen[key] = true
		scopes = append(scopes, scope)
	}

	return scopes
}

// excludesOf returns the compiled exclude patterns of a scope, and the
// directory they are written against: the scope itself when it holds its own
// configuration file, the working directory otherwise.
func (r Finder) excludesOf(scope configuration.Scope) (excludeMatcher, string) {
	root := scope.Root
	if root == "" {
		root = r.resolveProjectRoot()
	}

	return compileExcludes(scope.Configuration.ExcludePatterns), root
}

func (r Finder) Search(fileExtension string) FileList {

	// Ensur extension starts with a dot
	if !strings.HasPrefix(fileExtension, ".") {
		fileExtension = "." + fileExtension
	}

	// Check shared discovery cache first
	if r.Discovery != nil {
		if cached := r.Discovery.Get(fileExtension); cached != nil {
			return *cached
		}
	}

	var result FileList
	result.FilesByDirectory = make(map[string][]string)
	result.Files = []string{}

	scopes := r.scopes()

	// Search for files in each directory
	for _, scope := range scopes {

		// The extension may belong to another scope, which declared it for its
		// own files.
		if foreignExtensions(scopes, scope)[fileExtension] {
			continue
		}

		path := strings.TrimRight(scope.Path, "/")
		excludes, projectRoot := r.excludesOf(scope)
		nested := nestedScopes(scopes, path)

		var matches []string
		if strings.HasSuffix(path, fileExtension) {
			matches = append(matches, path)
		} else {
			matches, _ = filepathx.Glob(path + "/**/*" + fileExtension)
		}

		// deal with excluded files
		for _, file := range matches {
			if isExcluded(file, projectRoot, path, excludes) {
				continue
			}
			if inNestedScope(nested, file) {
				continue
			}

			result.Files = append(result.Files, file)

			// add file to filesByDirectory
			directory := path
			if _, ok := result.FilesByDirectory[directory]; !ok {
				result.FilesByDirectory[directory] = []string{}
			}

			result.FilesByDirectory[directory] = append(result.FilesByDirectory[directory], file)
		}
	}

	return result
}

// SearchMultiple performs a single directory walk and dispatches files by extension.
// Extensions should include the leading dot (e.g. ".go", ".php").
// Returns a map from extension to FileList.
func (r Finder) SearchMultiple(extensions []string) map[string]FileList {
	results := make(map[string]FileList, len(extensions))
	extSet := make(map[string]bool, len(extensions))
	for _, ext := range extensions {
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		extSet[ext] = true
		results[ext] = FileList{
			Files:            []string{},
			FilesByDirectory: make(map[string][]string),
		}
	}

	scopes := r.scopes()

	for _, scope := range scopes {
		srcPath := strings.TrimRight(scope.Path, "/")
		excludes, projectRoot := r.excludesOf(scope)
		nested := nestedScopes(scopes, srcPath)

		// The walk looks for the union of the extensions declared across
		// scopes, so that a scope adding one does not have the others pick it
		// up too.
		foreign := foreignExtensions(scopes, scope)

		// If the source path itself is a file, check its extension
		info, err := os.Stat(srcPath)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			ext := filepath.Ext(srcPath)
			if extSet[ext] && !foreign[ext] {
				if !isExcluded(srcPath, projectRoot, srcPath, excludes) {
					fl := results[ext]
					fl.Files = append(fl.Files, srcPath)
					fl.FilesByDirectory[srcPath] = append(fl.FilesByDirectory[srcPath], srcPath)
					results[ext] = fl
				}
			}
			continue
		}

		// Single walk for all extensions
		filepath.WalkDir(srcPath, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				// A directory that is itself an analyzed source is walked by
				// its own scope, with its own configuration. Stopping here is
				// what makes the most specific source own its files, and what
				// keeps them from being discovered twice.
				if path != srcPath && nested[path] {
					return filepath.SkipDir
				}
				// An excluded directory is not descended into: reading a
				// vendor tree, a build cache or a .git directory only to
				// discard every file it holds is the most expensive part of
				// the discovery on a real project.
				if path != srcPath && excludes.prunesDirectory(path, projectRoot, srcPath) {
					return filepath.SkipDir
				}
				return nil
			}
			ext := filepath.Ext(path)
			if !extSet[ext] || foreign[ext] {
				return nil
			}
			if isExcluded(path, projectRoot, srcPath, excludes) {
				return nil
			}
			fl := results[ext]
			fl.Files = append(fl.Files, path)
			if _, ok := fl.FilesByDirectory[srcPath]; !ok {
				fl.FilesByDirectory[srcPath] = []string{}
			}
			fl.FilesByDirectory[srcPath] = append(fl.FilesByDirectory[srcPath], path)
			results[ext] = fl
			return nil
		})
	}

	return results
}

// foreignExtensions returns the extra extensions another scope declared for its
// own files, and this one did not. The discovery walks once for the union of
// the extensions declared across scopes, so a scope has to leave out the ones
// that are none of its business. Built-in extensions are never foreign: every
// scope analyzes the languages the tool supports out of the box.
func foreignExtensions(scopes []configuration.Scope, current configuration.Scope) map[string]bool {
	var own map[string]bool
	for _, ext := range current.Configuration.DeclaredExtensions() {
		if own == nil {
			own = make(map[string]bool)
		}
		own[ext] = true
	}

	var foreign map[string]bool
	for _, scope := range scopes {
		if scope.Configuration == current.Configuration {
			continue
		}
		for _, ext := range scope.Configuration.DeclaredExtensions() {
			if own[ext] {
				continue
			}
			if foreign == nil {
				foreign = make(map[string]bool)
			}
			foreign[ext] = true
		}
	}

	return foreign
}

// nestedScopes returns the analyzed sources lying strictly inside root, spelled
// the way the walk of root will spell them.
func nestedScopes(scopes []configuration.Scope, root string) map[string]bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return nil
	}
	rootAbs = filepath.Clean(rootAbs)

	var nested map[string]bool
	for _, scope := range scopes {
		abs, err := filepath.Abs(strings.TrimRight(scope.Path, "/"))
		if err != nil {
			continue
		}
		abs = filepath.Clean(abs)
		if abs == rootAbs {
			continue
		}
		rel, ok := relativeTo(rootAbs, abs)
		if !ok {
			continue
		}
		if nested == nil {
			nested = make(map[string]bool)
		}
		nested[filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(rel, "/")))] = true
	}

	return nested
}

// inNestedScope reports whether a file lies under a more specific analyzed
// source, which discovers it on its own.
func inNestedScope(nested map[string]bool, file string) bool {
	for dir := range nested {
		if strings.HasPrefix(file, dir+string(filepath.Separator)) {
			return true
		}
	}

	return false
}

func MergeFileLists(lists ...FileList) FileList {
	result := FileList{Files: []string{}, FilesByDirectory: map[string][]string{}}
	for _, fl := range lists {
		result.Files = append(result.Files, fl.Files...)
		for dir, files := range fl.FilesByDirectory {
			result.FilesByDirectory[dir] = append(result.FilesByDirectory[dir], files...)
		}
	}
	return result
}

// excludeMatcher holds the compiled exclude patterns, and among them the ones
// that can decide the fate of a whole directory during a walk.
type excludeMatcher struct {
	all []*regexp.Regexp
	// prunable holds the patterns that match a prefix of the path, so that
	// matching a directory proves that every file under it matches too. A
	// pattern anchored on the end of the path ("$", "\z") is not one of them:
	// "/vendor/" excludes everything under vendor, but "_test\.php$" says
	// nothing about the directory holding the file.
	prunable []*regexp.Regexp
}

func compileExcludes(patterns []string) excludeMatcher {
	m := excludeMatcher{
		all:      make([]*regexp.Regexp, 0, len(patterns)),
		prunable: make([]*regexp.Regexp, 0, len(patterns)),
	}
	for _, pattern := range patterns {
		re, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		m.all = append(m.all, re)
		if !strings.Contains(pattern, "$") && !strings.Contains(pattern, `\z`) {
			m.prunable = append(m.prunable, re)
		}
	}
	return m
}

// prunesDirectory reports whether a directory can be skipped whole. It is only
// true when a prefix pattern matches the directory path itself, which makes it
// match every path below it: skipping is then exactly equivalent to excluding
// each of those files one by one.
func (m excludeMatcher) prunesDirectory(dir, projectRoot, sourceRoot string) bool {
	if len(m.prunable) == 0 {
		return false
	}
	target, ok := relativeTo(projectRoot, dir)
	if !ok {
		if target, ok = relativeTo(sourceRoot, dir); !ok {
			target = dir
		}
	}
	// patterns are written with delimiters ("/vendor/"), so the directory is
	// matched as the prefix it is
	target += "/"
	for _, re := range m.prunable {
		if re.MatchString(target) {
			return true
		}
	}
	return false
}

// isExcluded reports whether a discovered file must be skipped. Exclude
// patterns are matched against the file path relative to the project root
// (the working directory, where the configuration file lives), with a
// leading "/" and forward slashes. This keeps two properties at once:
//   - the absolute location of the project never causes a false match (a
//     project served from /var/www, or a macOS temp directory under
//     /var/folders, is not emptied out by the default "/var/" pattern);
//   - with several sources (e.g. "./a" and "./b"), a pattern can target one
//     of them ("/b/file") because the source prefix is preserved.
//
// When the file lies outside the project root, the path relative to the
// analyzed source root is used instead, and as a last resort the absolute
// path.
func isExcluded(file, projectRoot, sourceRoot string, excludes excludeMatcher) bool {
	target, ok := relativeTo(projectRoot, file)
	if !ok {
		if target, ok = relativeTo(sourceRoot, file); !ok {
			target = file
		}
	}
	for _, re := range excludes.all {
		if re.MatchString(target) {
			return true
		}
	}
	return false
}

// relativeTo returns file relative to root, with a leading "/" and forward
// slashes. ok is false when root is empty or file is not located under root.
func relativeTo(root, file string) (target string, ok bool) {
	if root == "" {
		return "", false
	}
	rel, err := filepath.Rel(root, file)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return "/" + filepath.ToSlash(rel), true
}
