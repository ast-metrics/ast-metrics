package python

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/python/module"
	Treesitter "github.com/ast-metrics/ast-metrics/internal/engine/treesitter"
	"github.com/ast-metrics/ast-metrics/internal/file"
	pb "github.com/ast-metrics/ast-metrics/pb"

	"github.com/pterm/pterm"

	sitter "github.com/smacker/go-tree-sitter"
)

type PythonRunner struct {
	progressbar   *pterm.SpinnerPrinter
	Configuration *configuration.Configuration
	foundFiles    file.FileList
	// modules names the module a file implements. Files are parsed in
	// parallel, and the cache is shared by every one of them.
	modules *module.Cache
}

// IsRequired returns true when analyzed files are concerned by the programming language
func (r PythonRunner) IsRequired() bool {
	return len(r.getFileList().Files) > 0
}

// Prepare the engine
func (r *PythonRunner) Ensure() error { return nil }

// DumpAST parses Python files and returns in-memory AST objects
func (r PythonRunner) DumpAST() []*pb.File {
	// One cache for the whole run: every file of a package otherwise looks
	// for the same __init__.py again.
	r.modules = module.NewCache()
	return engine.DumpFiles(
		r.getFileList().Files,
		r.progressbar,
		func(path string) (*pb.File, error) { return r.Parse(path) },
		engine.DumpOptions{Label: r.Name()},
	)
}

func (r PythonRunner) Name() string {
	return "Python"
}

// Cleanups the engine
func (r PythonRunner) Finish() error {
	if r.progressbar != nil {
		r.progressbar.Stop()
	}
	return nil
}

// Give a UI progress bar to the engine
func (r *PythonRunner) SetProgressbar(progressbar *pterm.SpinnerPrinter) {
	r.progressbar = progressbar
}

// Give the configuration to the engine
func (r *PythonRunner) SetConfiguration(configuration *configuration.Configuration) {
	r.Configuration = configuration
}

// Parse a file and return a protobuf-compatible AST object (no store)
func (r PythonRunner) Parse(path string) (*pb.File, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return &pb.File{Path: path, ProgrammingLanguage: "Python"}, err
	}

	parser := sitter.NewParser()
	adapter := NewTreeSitterAdapter(src)
	parser.SetLanguage(adapter.Language())

	tree := parser.Parse(nil, src)
	root := tree.RootNode()
	adapter.SetRootNode(root)

	v := Treesitter.NewVisitor(adapter, path, src)
	v.Visit(root)
	file := v.Result()
	file.ProgrammingLanguage = "Python"
	r.nameTheModuleAfterItsPath(file, path)

	// Detect if file is a test file
	file.IsTest = r.isTestFile(path, file)

	return file, nil
}

// nameTheModuleAfterItsPath spells the namespace of a parsed file the way an
// import statement spells it, "company.product.artifact.entrypoint" where the
// file says "entrypoint", and spells the relative imports of the file the same
// way: "from .clearing import Clearing" written in company.product.artifact
// depends on company.product.artifact.clearing.
func (r PythonRunner) nameTheModuleAfterItsPath(file *pb.File, path string) {
	moduleName := r.modules.ModuleOf(path)
	if moduleName == "" {
		return
	}
	engine.RenameNamespace(file, moduleName)
	isPackage := filepath.Base(path) == "__init__.py"
	// A module standing under no package has nothing for a relative import to
	// climb into: the specifier is kept as written, ".pkg" naming a sibling
	// which "pkg" alone would not.
	if !isPackage && !strings.Contains(moduleName, ".") {
		return
	}
	for _, dependency := range engine.DependenciesAtEveryScope(file) {
		if dependency == nil || !strings.HasPrefix(dependency.Namespace, ".") {
			continue
		}
		if absolute, understood := module.Rebase(moduleName, isPackage, dependency.Namespace); understood {
			dependency.Namespace = absolute
		}
	}
}

func (r *PythonRunner) getFileList() file.FileList {
	if r.foundFiles.Files != nil {
		return r.foundFiles
	}

	finder := file.Finder{Configuration: *r.Configuration}
	if r.Configuration.FileDiscovery != nil {
		if fd, ok := r.Configuration.FileDiscovery.(*file.FileDiscovery); ok {
			finder.Discovery = fd
		}
	}
	extensions := r.Configuration.GetExtensionsForLanguage("python")
	var lists []file.FileList
	for _, ext := range extensions {
		lists = append(lists, finder.Search(ext))
	}
	r.foundFiles = file.MergeFileLists(lists...)
	return r.foundFiles
}

// isTestFile determines if a Python file is a test file based on:
// 1. Filename pattern (starts with test_ or ends with _test.py)
// 2. Class inheritance (extends unittest.TestCase or similar test base classes)
func (r PythonRunner) isTestFile(path string, file *pb.File) bool {
	baseName := strings.ToLower(path)
	fileName := strings.ToLower(filepath.Base(path))

	// Check filename pattern
	if strings.HasPrefix(fileName, "test_") || strings.HasSuffix(baseName, "_test.py") {
		return true
	}

	// Check if any class extends a test base class
	classes := engine.GetClassesInFile(file)
	for _, class := range classes {
		if class == nil {
			continue
		}
		// Check extends (Python uses extends for inheritance)
		for _, ext := range class.Extends {
			if ext == nil {
				continue
			}
			qualified := strings.ToLower(ext.Qualified)
			short := strings.ToLower(ext.Short)
			// Common Python test base classes
			if strings.Contains(qualified, "testcase") ||
				strings.Contains(short, "testcase") ||
				strings.Contains(qualified, "unittest") ||
				strings.Contains(qualified, "pytest") {
				return true
			}
		}
	}

	return false
}
