package csharp

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
)

type CSharpRunner struct {
	progressbar   *pterm.SpinnerPrinter
	Configuration *configuration.Configuration
	foundFiles    file.FileList
}

func (r CSharpRunner) Name() string                                     { return "C#" }
func (r CSharpRunner) IsRequired() bool                                 { return len(r.getFileList().Files) > 0 }
func (r *CSharpRunner) Ensure() error                                   { return nil }
func (r *CSharpRunner) SetProgressbar(p *pterm.SpinnerPrinter)          { r.progressbar = p }
func (r *CSharpRunner) SetConfiguration(c *configuration.Configuration) { r.Configuration = c }

func (r CSharpRunner) DumpAST() []*pb.File {
	return engine.DumpFiles(
		r.getFileList().Files,
		r.progressbar,
		func(path string) (*pb.File, error) { return r.Parse(path) },
		engine.DumpOptions{Label: r.Name()},
	)
}

func (r CSharpRunner) Finish() error {
	if r.progressbar != nil {
		r.progressbar.Stop()
	}
	return nil
}

func (r CSharpRunner) Parse(path string) (*pb.File, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return &pb.File{Path: path, ProgrammingLanguage: "C#"}, err
	}

	adapter := NewTreeSitterAdapter(src)
	parsed, err := Treesitter.Parse(adapter, src)
	if err != nil {
		return &pb.File{Path: path, ProgrammingLanguage: "C#"}, err
	}
	defer parsed.Close()
	root := parsed.Root

	v := Treesitter.NewVisitor(adapter, path, src)
	v.Visit(root)

	file := v.Result()
	file.ProgrammingLanguage = "C#"

	// Detect if file is a test file
	file.IsTest = r.isTestFile(path, src)

	return file, nil
}

func (r *CSharpRunner) getFileList() file.FileList {
	if r.foundFiles.Files != nil {
		return r.foundFiles
	}
	finder := file.Finder{Configuration: *r.Configuration}
	if r.Configuration.FileDiscovery != nil {
		if fd, ok := r.Configuration.FileDiscovery.(*file.FileDiscovery); ok {
			finder.Discovery = fd
		}
	}
	extensions := r.Configuration.GetExtensionsForLanguage("csharp")
	var lists []file.FileList
	for _, ext := range extensions {
		lists = append(lists, finder.Search(ext))
	}
	r.foundFiles = file.MergeFileLists(lists...)
	return r.foundFiles
}

// isTestFile determines if a C# file is a test file based on:
// 1. Filename pattern (FooTest.cs, FooTests.cs)
// 2. Source code containing xUnit/NUnit/MSTest markers
func (r CSharpRunner) isTestFile(path string, src []byte) bool {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if strings.HasSuffix(base, "Test") || strings.HasSuffix(base, "Tests") {
		return true
	}

	source := string(src)
	for _, marker := range []string{
		"[Test]", "[TestCase", "[Fact]", "[Theory]", "[TestMethod]",
		"using Xunit", "using NUnit", "using Microsoft.VisualStudio.TestTools",
	} {
		if strings.Contains(source, marker) {
			return true
		}
	}

	return false
}
