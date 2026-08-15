package cpp

import (
	"os"
	"path/filepath"
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

func (r *CppRunner) getFileList() file.FileList {
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
	extensions := []string{".cpp", ".cc", ".cxx", ".hpp", ".hh", ".hxx"}
	for _, ext := range r.Configuration.GetExtensionsForLanguage("cpp") {
		if ext != ".cpp" {
			extensions = append(extensions, ext)
		}
	}
	for _, ext := range extensions {
		lists = append(lists, finder.Search(ext))
	}
	r.foundFiles = file.MergeFileLists(lists...)
	return r.foundFiles
}

func (r CppRunner) isTestFile(path string, _ []byte) bool {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return strings.HasSuffix(base, "Test") || strings.HasSuffix(base, "Tests") ||
		strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test")
}
