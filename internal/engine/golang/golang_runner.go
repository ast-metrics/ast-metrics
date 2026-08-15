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
// way an import statement spells it.
//
// A Go file names its package with a bare word, "analyzer", where every file
// importing it names it by its import path, "example.com/demo/internal/
// analyzer". Left as they are, the two ends of a dependency are not written in
// the same language: nothing links a package to the packages using it, and two
// directories that happen to hold a package of the same name are one and the
// same. The short name is kept: it is how the package reads in the report.
//
// A file outside of any module keeps the bare name, which is all it has.
func (r *GolangRunner) nameThePackageAfterItsImportPath(file *pb.File, path string) {
	if file == nil || file.Stmts == nil || len(file.Stmts.StmtNamespace) == 0 {
		return
	}
	namespace := file.Stmts.StmtNamespace[0]
	if namespace == nil || namespace.Name == nil {
		return
	}

	importPath := r.modules.ImportPathOf(filepath.Dir(path))
	if importPath == "" {
		return
	}

	packageName := namespace.Name.Qualified
	namespace.Name.Qualified = importPath
	// The dependencies were named after the package while it was being read, so
	// they carry the bare name and have to be spelled again.
	for _, dependency := range dependenciesIn(file) {
		if dependency != nil && dependency.From == packageName {
			dependency.From = importPath
		}
	}
}

// dependenciesIn lists the dependencies held by a file, at every scope they can
// be attached to. The visitor attaches one dependency to several scopes at
// once, so the same one can be listed more than once.
func dependenciesIn(file *pb.File) []*pb.StmtExternalDependency {
	dependencies := file.Stmts.StmtExternalDependencies
	for _, namespace := range file.Stmts.StmtNamespace {
		if namespace != nil && namespace.Stmts != nil {
			dependencies = append(dependencies, namespace.Stmts.StmtExternalDependencies...)
		}
	}
	for _, class := range engine.GetClassesInFile(file) {
		if class != nil && class.Stmts != nil {
			dependencies = append(dependencies, class.Stmts.StmtExternalDependencies...)
		}
	}
	return dependencies
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
