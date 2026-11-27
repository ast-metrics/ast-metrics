package java

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

type JavaRunner struct {
	progressbar   *pterm.SpinnerPrinter
	Configuration *configuration.Configuration
	foundFiles    file.FileList
}

func (r JavaRunner) Name() string                                     { return "Java" }
func (r JavaRunner) IsRequired() bool                                 { return len(r.getFileList().Files) > 0 }
func (r *JavaRunner) Ensure() error                                   { return nil }
func (r *JavaRunner) SetProgressbar(p *pterm.SpinnerPrinter)          { r.progressbar = p }
func (r *JavaRunner) SetConfiguration(c *configuration.Configuration) { r.Configuration = c }

func (r JavaRunner) DumpAST() []*pb.File {
	return engine.DumpFiles(
		r.getFileList().Files,
		r.progressbar,
		func(path string) (*pb.File, error) { return r.Parse(path) },
		engine.DumpOptions{Label: r.Name()},
	)
}

func (r JavaRunner) Finish() error {
	if r.progressbar != nil {
		r.progressbar.Stop()
	}
	return nil
}

func (r JavaRunner) Parse(path string) (*pb.File, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return &pb.File{Path: path, ProgrammingLanguage: "Java"}, err
	}

	adapter := NewTreeSitterAdapter(src)
	parsed, err := Treesitter.Parse(adapter, src)
	if err != nil {
		return &pb.File{Path: path, ProgrammingLanguage: "Java"}, err
	}
	defer parsed.Close()
	root := parsed.Root

	v := Treesitter.NewVisitor(adapter, path, src)
	v.Visit(root)

	file := v.Result()
	file.ProgrammingLanguage = "Java"

	// Detect if file is a test file
	file.IsTest = r.isTestFile(path, src)

	return file, nil
}

func (r *JavaRunner) getFileList() file.FileList {
	if r.foundFiles.Files != nil {
		return r.foundFiles
	}
	finder := file.Finder{Configuration: *r.Configuration}
	if r.Configuration.FileDiscovery != nil {
		if fd, ok := r.Configuration.FileDiscovery.(*file.FileDiscovery); ok {
			finder.Discovery = fd
		}
	}
	extensions := r.Configuration.GetExtensionsForLanguage("java")
	var lists []file.FileList
	for _, ext := range extensions {
		lists = append(lists, finder.Search(ext))
	}
	r.foundFiles = file.MergeFileLists(lists...)
	return r.foundFiles
}

// isTestFile determines if a Java file is a test file based on:
// 1. Filename pattern (FooTest.java, FooTests.java, TestFoo.java)
// 2. Maven/Gradle conventional test directory (src/test/)
// 3. Source code containing JUnit/TestNG markers (@Test, org.junit, org.testng)
func (r JavaRunner) isTestFile(path string, src []byte) bool {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	if strings.HasSuffix(base, "Test") || strings.HasSuffix(base, "Tests") || strings.HasPrefix(base, "Test") {
		return true
	}
	normalized := filepath.ToSlash(path)
	if strings.Contains(normalized, "/src/test/") {
		return true
	}

	source := string(src)
	if strings.Contains(source, "@Test") || strings.Contains(source, "org.junit") || strings.Contains(source, "org.testng") {
		return true
	}

	return false
}
