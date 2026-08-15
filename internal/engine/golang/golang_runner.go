package golang

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/configuration"
	engine "github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/golang/module"
	Treesitter "github.com/ast-metrics/ast-metrics/internal/engine/treesitter"
	File "github.com/ast-metrics/ast-metrics/internal/file"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/pterm/pterm"

	sitter "github.com/smacker/go-tree-sitter"
)

type GolangRunner struct {
	progressbar   *pterm.SpinnerPrinter
	Configuration *configuration.Configuration
	foundFiles    File.FileList
	// modules names the package a file belongs to. Files are parsed in
	// parallel, and the cache is shared by every one of them.
	modules *module.Cache
}

// IsRequired returns true if at least one Go file is found
func (r GolangRunner) IsRequired() bool {
	// If at least one Go file is found, we need to run PHP engine
	return len(r.getFileList().Files) > 0
}

// SetProgressbar sets the progressbar
func (r *GolangRunner) SetProgressbar(progressbar *pterm.SpinnerPrinter) {
	(*r).progressbar = progressbar
}

// SetConfiguration sets the configuration
func (r *GolangRunner) SetConfiguration(configuration *configuration.Configuration) {
	(*r).Configuration = configuration
}

// Ensure ensures Go is ready to run.
func (r *GolangRunner) Ensure() error {
	return nil
}

// Finish cleans up the workspace
func (r GolangRunner) Finish() error {
	if r.progressbar != nil {
		r.progressbar.Stop()
	}
	return nil
}

// DumpAST parses Go files and returns in-memory AST objects
func (r GolangRunner) DumpAST() []*pb.File {
	// One cache for the whole run: every file of a directory, and every
	// directory of a module, otherwise reads the same go.mod again.
	r.modules = module.NewCache()
	return engine.DumpFiles(
		r.getFileList().Files,
		r.progressbar,
		func(path string) (*pb.File, error) { return r.Parse(path) },
		engine.DumpOptions{Label: r.Name()},
	)
}

func (r GolangRunner) Name() string {
	return "Golang"
}

// nameThePackageAfterItsImportPath spells the namespace of a parsed file the
// way an import statement spells it: "example.com/demo/internal/analyzer"
// where the file says "analyzer". A file outside of any module keeps the bare
// name, which is all it has.
func (r *GolangRunner) nameThePackageAfterItsImportPath(file *pb.File, path string) {
	engine.RenameNamespace(file, r.modules.ImportPathOf(filepath.Dir(path)))
}

func (r *GolangRunner) Parse(path string) (*pb.File, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return &pb.File{Path: path, ProgrammingLanguage: "Golang"}, err
	}

	parser := sitter.NewParser()
	adapter := NewTreeSitterAdapter(src)
	parser.SetLanguage(adapter.Language())

	tree := parser.Parse(nil, src)
	root := tree.RootNode()

	v := Treesitter.NewVisitor(adapter, path, src)
	v.Visit(root)
	file := v.Result()
	file.ProgrammingLanguage = "Golang"
	if root.HasError() {
		file.Errors = append(file.Errors, "Parse error")
	}
	r.nameThePackageAfterItsImportPath(file, path)

	// Detect if file is a test file
	file.IsTest = r.isTestFile(path, file)
	if file.IsTest {
		attachTestSymbolRefs(file, adapter, root, src)
	}

	return file, nil
}

// getFileList returns the list of PHP files to analyze, and caches it in memory
func (r *GolangRunner) getFileList() File.FileList {

	if r.foundFiles.Files != nil {
		return r.foundFiles
	}

	finder := File.Finder{Configuration: *r.Configuration}
	if r.Configuration.FileDiscovery != nil {
		if fd, ok := r.Configuration.FileDiscovery.(*File.FileDiscovery); ok {
			finder.Discovery = fd
		}
	}
	extensions := r.Configuration.GetExtensionsForLanguage("go")
	var lists []File.FileList
	for _, ext := range extensions {
		lists = append(lists, finder.Search(ext))
	}
	r.foundFiles = File.MergeFileLists(lists...)

	return r.foundFiles
}

// isTestFile determines if a Go file is a test file based on:
// 1. Filename pattern (ends with _test.go)
// 2. Function names starting with Test, Benchmark, or Example
func (r GolangRunner) isTestFile(path string, file *pb.File) bool {
	// Check filename pattern
	if strings.HasSuffix(path, "_test.go") {
		return true
	}

	return false
}
