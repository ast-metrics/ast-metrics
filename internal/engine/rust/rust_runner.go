package rust

import (
	"os"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/rust/module"
	Treesitter "github.com/ast-metrics/ast-metrics/internal/engine/treesitter"
	"github.com/ast-metrics/ast-metrics/internal/file"
	pb "github.com/ast-metrics/ast-metrics/pb"

	"github.com/pterm/pterm"
	sitter "github.com/smacker/go-tree-sitter"
)

type RustRunner struct {
	progressbar   *pterm.SpinnerPrinter
	Configuration *configuration.Configuration
	foundFiles    file.FileList
	// crates locates the files in the module tree of their crate. Files are
	// parsed in parallel, and the cache is shared by every one of them.
	crates *module.Cache
}

func (r RustRunner) Name() string                                     { return "Rust" }
func (r RustRunner) IsRequired() bool                                 { return len(r.getFileList().Files) > 0 }
func (r *RustRunner) Ensure() error                                   { return nil }
func (r *RustRunner) SetProgressbar(p *pterm.SpinnerPrinter)          { r.progressbar = p }
func (r *RustRunner) SetConfiguration(c *configuration.Configuration) { r.Configuration = c }

func (r RustRunner) DumpAST() []*pb.File {
	// One cache for the whole run: every file of a crate otherwise reads the
	// same Cargo.toml again.
	r.crates = module.NewCache()
	return engine.DumpFiles(
		r.getFileList().Files,
		r.progressbar,
		func(path string) (*pb.File, error) { return r.Parse(path) },
		engine.DumpOptions{Label: r.Name()},
	)
}

func (r RustRunner) Finish() error {
	if r.progressbar != nil {
		r.progressbar.Stop()
	}
	return nil
}

func (r RustRunner) Parse(path string) (*pb.File, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return &pb.File{Path: path, ProgrammingLanguage: "Rust"}, err
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
	file.ProgrammingLanguage = "Rust"
	r.nameTheModuleAfterItsPath(file, path)

	// Detect if file is a test file
	file.IsTest = r.isTestFile(path, src)

	return file, nil
}

// nameTheModuleAfterItsPath spells the namespace of a parsed file the way a
// use statement spells it from outside, "demo::artifact" where the file is
// src/artifact/mod.rs of the crate demo, and anchors the use paths of the file
// the same way: `use crate::clearing::Clearing` depends on demo::clearing, and
// `use super::shared` on the module beside the importing one. A file outside
// of any crate keeps its bare name, which is all it has.
func (r RustRunner) nameTheModuleAfterItsPath(file *pb.File, path string) {
	location, found := r.crates.Locate(path)
	if !found {
		return
	}
	engine.RenameNamespace(file, location.Path())
	for _, dependency := range engine.DependenciesAtEveryScope(file) {
		if dependency != nil && dependency.Namespace != "" {
			dependency.Namespace = module.Anchor(location, dependency.Namespace)
		}
	}
}

func (r *RustRunner) getFileList() file.FileList {
	if r.foundFiles.Files != nil {
		return r.foundFiles
	}
	finder := file.Finder{Configuration: *r.Configuration}
	if r.Configuration.FileDiscovery != nil {
		if fd, ok := r.Configuration.FileDiscovery.(*file.FileDiscovery); ok {
			finder.Discovery = fd
		}
	}
	extensions := r.Configuration.GetExtensionsForLanguage("rust")
	var lists []file.FileList
	for _, ext := range extensions {
		lists = append(lists, finder.Search(ext))
	}
	r.foundFiles = file.MergeFileLists(lists...)
	return r.foundFiles
}

// isTestFile determines if a Rust file is a test file based on:
// 1. Filename pattern (ends with _test.rs)
// 2. Source code contains #[test] or #[cfg(test)] attributes
func (r RustRunner) isTestFile(path string, src []byte) bool {
	// Check filename pattern
	if strings.HasSuffix(path, "_test.rs") {
		return true
	}

	// Check for test attributes in source code
	source := string(src)
	if strings.Contains(source, "#[test]") || strings.Contains(source, "#[cfg(test)]") {
		return true
	}

	return false
}
