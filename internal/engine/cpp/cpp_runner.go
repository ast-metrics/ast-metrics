package cpp

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	Treesitter "github.com/ast-metrics/ast-metrics/internal/engine/treesitter"
	"github.com/ast-metrics/ast-metrics/internal/file"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/pterm/pterm"
	sitter "github.com/smacker/go-tree-sitter"
)

// CppRunner provides initial, syntax-level C++ support through Tree-sitter.
// It deliberately does not attempt preprocessing or semantic type resolution.
type CppRunner struct {
	progressbar   *pterm.SpinnerPrinter
	Configuration *configuration.Configuration
	foundFiles    file.FileList
}

func (r CppRunner) Name() string                                     { return "C++" }
func (r CppRunner) IsRequired() bool                                 { return len(r.getFileList().Files) > 0 }
func (r *CppRunner) Ensure() error                                   { return nil }
func (r *CppRunner) SetProgressbar(p *pterm.SpinnerPrinter)          { r.progressbar = p }
func (r *CppRunner) SetConfiguration(c *configuration.Configuration) { r.Configuration = c }

func (r CppRunner) DumpAST() []*pb.File {
	return engine.DumpFiles(r.getFileList().Files, r.progressbar, r.Parse, engine.DumpOptions{Label: r.Name()})
}

func (r CppRunner) Finish() error {
	if r.progressbar != nil {
		r.progressbar.Stop()
	}
	return nil
}

func (r CppRunner) Parse(path string) (*pb.File, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return &pb.File{Path: path, ProgrammingLanguage: "C++"}, err
	}
	parser := sitter.NewParser()
	adapter := NewTreeSitterAdapter(src)
	parser.SetLanguage(adapter.Language())
	tree := parser.Parse(nil, src)
	adapter.SetRootNode(tree.RootNode())
	v := Treesitter.NewVisitor(adapter, path, src)
	v.Visit(tree.RootNode())
	result := v.Result()
	result.ProgrammingLanguage = "C++"
	result.IsTest = r.isTestFile(path, src)
	return result, nil
}

func (r CppRunner) getFileList() file.FileList {
	if r.foundFiles.Files != nil {
		return r.foundFiles
	}
	if r.Configuration == nil {
		r.Configuration = &configuration.Configuration{}
	}
	finder := file.Finder{Configuration: *r.Configuration}
	if fd, ok := r.Configuration.FileDiscovery.(*file.FileDiscovery); ok {
		finder.Discovery = fd
	}
	var lists []file.FileList
	extensions := r.Configuration.GetExtensionsForLanguage("cpp")
	for _, ext := range extensions {
		lists = append(lists, finder.Search(ext))
	}
	lists = append(lists, r.claimHeaderFiles(finder, extensions))
	r.foundFiles = file.MergeFileLists(lists...)
	return r.foundFiles
}

// claimHeaderFiles discovers `.h` headers, which may hold C or C++, and keeps
// those whose content looks like C++. A `.h` explicitly listed in the
// configured extensions is already claimed as a whole: the content check then
// runs for no other extension.
func (r CppRunner) claimHeaderFiles(finder file.Finder, extensions []string) file.FileList {
	claimed := false
	for _, ext := range extensions {
		if ext == ".h" {
			claimed = true
		}
	}
	if claimed {
		return file.FileList{}
	}
	list := finder.Search(".h")
	kept := file.FileList{Files: make([]string, 0, len(list.Files))}
	for _, path := range list.Files {
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if looksLikeCpp(src) {
			kept.Files = append(kept.Files, path)
		}
	}
	return kept
}

// cppContentMarkers match constructs that exist in C++ but not in C.
var cppContentMarkers = []*regexp.Regexp{
	regexp.MustCompile(`\bclass\s+[A-Za-z_]\w*`),
	regexp.MustCompile(`\bnamespace\s+[A-Za-z_]\w*`),
	regexp.MustCompile(`\btemplate\s*<`),
	regexp.MustCompile(`\b(public|private|protected)\s*:`),
	regexp.MustCompile(`\b(typename|constexpr|nullptr)\b`),
	regexp.MustCompile(`::`),
}

// looksLikeCpp reports whether a header's content uses C++-only constructs.
// Objective-C headers are excluded first: they share the `.h` extension and
// can mention `class` or `::` only rarely, but are not C++.
func looksLikeCpp(src []byte) bool {
	content := string(src)
	if strings.Contains(content, "@interface") || strings.Contains(content, "@implementation") {
		return false
	}
	for _, marker := range cppContentMarkers {
		if marker.MatchString(content) {
			return true
		}
	}
	return false
}

// isTestFile determines if a C++ file is a test file based on the file name
// (`foo-test.cc`, `foo.test.cpp`, `foo_test.cc`, `Test*.cc`, `test_*.cc`),
// the position under a `tests/` directory, and test-framework markers in the
// source (gtest, Catch2, doctest, Boost.Test).
func (r CppRunner) isTestFile(path string, src []byte) bool {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	// PascalCase conventions: FooTest.cpp, FooTests.cpp, TestFoo.cpp
	if strings.HasSuffix(base, "Test") || strings.HasSuffix(base, "Tests") || strings.HasPrefix(base, "Test") {
		return true
	}
	// separator conventions: foo-test.cc, foo.test.cpp, foo_test.cc,
	// foo_tests.cc, test_foo.cc, test.cc, tests.cc
	if testFileNamePattern.MatchString(strings.ToLower(base)) {
		return true
	}
	dir := strings.ToLower(filepath.ToSlash(filepath.Dir(path)))
	if dir == "tests" || strings.HasSuffix(dir, "/tests") || strings.Contains(dir, "/tests/") {
		return true
	}

	source := string(src)
	for _, marker := range []string{"TEST", "TEST_F", "TYPED_TEST", "TEST_CASE", "SCENARIO"} {
		if containsIdentifierCall(source, marker) {
			return true
		}
	}
	for _, include := range []string{
		"#include <gtest/", "#include \"gtest/",
		"#include <catch2/", "#include \"catch2/",
		"#include <doctest/", "#include \"doctest/",
		"#include <boost/test/", "#include \"boost/test/",
	} {
		if strings.Contains(source, include) {
			return true
		}
	}
	return false
}

// testFileNamePattern matches the lowercase test file names. The separator
// before a suffix is required so that words merely ending in "test"
// (`latest`) do not match.
var testFileNamePattern = regexp.MustCompile(`^test_|^tests?$|[._-]tests?$`)

// containsIdentifierCall reports whether source contains a call of the given
// identifier: the name followed by "(", preceded by a character that cannot
// belong to an identifier, so `LATEST(` does not count as `TEST(`.
func containsIdentifierCall(source, name string) bool {
	target := name + "("
	for offset := 0; offset < len(source); {
		idx := strings.Index(source[offset:], target)
		if idx < 0 {
			return false
		}
		idx += offset
		if idx == 0 || !isIdentifierChar(source[idx-1]) {
			return true
		}
		offset = idx + len(name)
	}
	return false
}

func isIdentifierChar(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
