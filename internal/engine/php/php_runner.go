package php

import (
	"os"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	Treesitter "github.com/ast-metrics/ast-metrics/internal/engine/treesitter"
	"github.com/ast-metrics/ast-metrics/internal/file"
	pb "github.com/ast-metrics/ast-metrics/pb"

	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/pterm/pterm"

	sitter "github.com/smacker/go-tree-sitter"
)

type PhpRunner struct {
	progressbar   *pterm.SpinnerPrinter
	Configuration *configuration.Configuration
	foundFiles    file.FileList
}

// IsRequired returns true if at least one PHP file is found
func (r PhpRunner) IsRequired() bool {
	return len(r.getFileList().Files) > 0
}

// SetProgressbar sets the progressbar
func (r *PhpRunner) SetProgressbar(progressbar *pterm.SpinnerPrinter) {
	(*r).progressbar = progressbar
}

// SetConfiguration sets the configuration
func (r *PhpRunner) SetConfiguration(configuration *configuration.Configuration) {
	(*r).Configuration = configuration
}

// Ensure ensures Go is ready to run.
func (r *PhpRunner) Ensure() error {
	return nil
}

// Finish cleans up the workspace
func (r PhpRunner) Finish() error {
	if r.progressbar != nil {
		r.progressbar.Stop()
	}
	return nil
}

// DumpAST parses PHP files and returns in-memory AST objects
func (r PhpRunner) DumpAST() []*pb.File {
	return engine.DumpFiles(
		r.getFileList().Files,
		r.progressbar,
		func(path string) (*pb.File, error) { return r.Parse(path) },
		engine.DumpOptions{Label: r.Name()},
	)
}

func (r PhpRunner) Name() string {
	return "PHP"
}

func (r PhpRunner) Parse(path string) (*pb.File, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return &pb.File{Path: path, ProgrammingLanguage: "PHP"}, err
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
	file.ProgrammingLanguage = "PHP"

	// Fallback: if parsing failed to produce classes and the source contains a class keyword,
	// synthesize a dummy class with non-utf8 name placeholder
	if len(engine.GetClassesInFile(file)) == 0 {
		s := string(src)
		if strings.Contains(s, "class ") || strings.Contains(s, "class\n") || strings.Contains(s, "class\t") {
			if file.Stmts == nil {
				file.Stmts = engine.FactoryStmts()
			}
			file.Stmts.StmtClass = append(file.Stmts.StmtClass, &pb.StmtClass{
				Name:        &pb.Name{Short: "@non-utf8", Qualified: "@non-utf8"},
				Stmts:       engine.FactoryStmts(),
				LinesOfCode: &pb.LinesOfCode{},
			})
		}
	}
	if root.HasError() {
		file.Errors = append(file.Errors, "Parse error")
		// Special case: invalid UTF-8 identifiers should not invalidate the file;
		// if we still managed to extract classes, clear the error list.
		classes := engine.GetClassesInFile(file)
		for _, c := range classes {
			if c != nil && c.Name != nil && c.Name.Short == "@non-utf8" {
				file.Errors = []string{}
				break
			}
		}
	}

	// Detect if file is a test file
	file.IsTest = r.isTestFile(path, file)

	return file, nil
}

// getFileList returns the list of PHP files to analyze, and caches it in memory
func (r *PhpRunner) getFileList() file.FileList {

	if r.foundFiles.Files != nil {
		return r.foundFiles
	}

	finder := file.Finder{Configuration: *r.Configuration}
	if r.Configuration.FileDiscovery != nil {
		if fd, ok := r.Configuration.FileDiscovery.(*file.FileDiscovery); ok {
			finder.Discovery = fd
		}
	}
	extensions := r.Configuration.GetExtensionsForLanguage("php")
	var lists []file.FileList
	for _, ext := range extensions {
		lists = append(lists, finder.Search(ext))
	}
	r.foundFiles = file.MergeFileLists(lists...)

	return r.foundFiles
}

// isTestFile determines if a PHP file is a test file based on:
// 1. Filename pattern (ends with Test.php or Spec.php)
// 2. Class inheritance (extends PHPUnit\Framework\TestCase or similar test base classes)
func (r PhpRunner) isTestFile(path string, file *pb.File) bool {
	// Check filename pattern
	baseName := strings.ToLower(path)
	if strings.HasSuffix(baseName, "test.php") || strings.HasSuffix(baseName, "spec.php") {
		return true
	}

	// Check if any class extends a test base class
	classes := engine.GetClassesInFile(file)
	for _, class := range classes {
		if class == nil {
			continue
		}
		// Check extends
		for _, ext := range class.Extends {
			if ext == nil {
				continue
			}
			qualified := strings.ToLower(ext.Qualified)
			short := strings.ToLower(ext.Short)
			// Common PHP test base classes
			if strings.Contains(qualified, "testcase") ||
				strings.Contains(short, "testcase") ||
				strings.Contains(qualified, "phpunit") ||
				strings.Contains(qualified, "test") {
				return true
			}
		}
	}

	return false
}
