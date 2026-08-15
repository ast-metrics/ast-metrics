package typescript

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	Treesitter "github.com/ast-metrics/ast-metrics/internal/engine/treesitter"
	"github.com/ast-metrics/ast-metrics/internal/engine/typescript/module"
	"github.com/ast-metrics/ast-metrics/internal/file"
	pb "github.com/ast-metrics/ast-metrics/pb"

	"github.com/pterm/pterm"

	sitter "github.com/smacker/go-tree-sitter"
)

type TypeScriptRunner struct {
	progressbar   *pterm.SpinnerPrinter
	Configuration *configuration.Configuration
	foundFiles    file.FileList
	// packages locates the files in their package. Files are parsed in
	// parallel, and the cache is shared by every one of them.
	packages *module.Cache
}

func (r TypeScriptRunner) IsRequired() bool {
	return len(r.getFileList().Files) > 0
}

func (r *TypeScriptRunner) Ensure() error { return nil }

func (r TypeScriptRunner) DumpAST() []*pb.File {
	// One cache for the whole run: every file of a directory otherwise looks
	// for the same manifest again.
	r.packages = module.NewCache()
	return engine.DumpFiles(
		r.getFileList().Files,
		r.progressbar,
		func(path string) (*pb.File, error) { return r.Parse(path) },
		engine.DumpOptions{Label: r.Name()},
	)
}

func (r TypeScriptRunner) Name() string {
	return "TypeScript"
}

func (r TypeScriptRunner) Finish() error {
	if r.progressbar != nil {
		r.progressbar.Stop()
	}
	return nil
}

func (r *TypeScriptRunner) SetProgressbar(progressbar *pterm.SpinnerPrinter) {
	r.progressbar = progressbar
}

func (r *TypeScriptRunner) SetConfiguration(configuration *configuration.Configuration) {
	r.Configuration = configuration
}

func (r TypeScriptRunner) Parse(path string) (*pb.File, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return &pb.File{Path: path, ProgrammingLanguage: "TypeScript"}, err
	}

	parser := sitter.NewParser()
	adapter := NewTreeSitterAdapter(src)
	parser.SetLanguage(adapter.Language())

	tree := parser.Parse(nil, src)
	root := tree.RootNode()

	v := Treesitter.NewVisitor(adapter, path, src)
	v.Visit(root)
	file := v.Result()
	file.ProgrammingLanguage = "TypeScript"
	r.nameTheModuleAfterItsPath(file, path)

	file.IsTest = r.isTestFile(path)

	return file, nil
}

// nameTheModuleAfterItsPath spells the namespace of a parsed file by its path
// from the root of its package, "src/artifact/entrypoint" where the file says
// "entrypoint", and spells the relative imports of the file the same way:
// "../clearing/clearing" written there depends on src/clearing/clearing. A
// file under no package keeps its bare name, which is all it has.
func (r TypeScriptRunner) nameTheModuleAfterItsPath(file *pb.File, path string) {
	location, found := r.packages.Locate(path)
	if !found {
		return
	}
	engine.RenameNamespace(file, location.Module)
	for _, dependency := range engine.DependenciesAtEveryScope(file) {
		if dependency != nil && dependency.Namespace != "" {
			dependency.Namespace = module.Anchor(location, dependency.Namespace)
		}
	}
}

func (r *TypeScriptRunner) getFileList() file.FileList {
	if r.foundFiles.Files != nil {
		return r.foundFiles
	}

	finder := file.Finder{Configuration: *r.Configuration}
	if r.Configuration.FileDiscovery != nil {
		if fd, ok := r.Configuration.FileDiscovery.(*file.FileDiscovery); ok {
			finder.Discovery = fd
		}
	}
	extensions := r.Configuration.GetExtensionsForLanguage("typescript")
	// Always include .tsx as well
	hasTsx := false
	for _, ext := range extensions {
		if ext == ".tsx" {
			hasTsx = true
			break
		}
	}
	if !hasTsx {
		extensions = append(extensions, ".tsx")
	}
	var lists []file.FileList
	for _, ext := range extensions {
		lists = append(lists, finder.Search(ext))
	}
	r.foundFiles = file.MergeFileLists(lists...)
	return r.foundFiles
}

func (r TypeScriptRunner) isTestFile(path string) bool {
	baseName := strings.ToLower(filepath.Base(path))

	// Check filename patterns
	if strings.HasSuffix(baseName, ".test.ts") || strings.HasSuffix(baseName, ".test.tsx") ||
		strings.HasSuffix(baseName, ".spec.ts") || strings.HasSuffix(baseName, ".spec.tsx") {
		return true
	}

	// Check directory patterns
	dir := strings.ToLower(filepath.Dir(path))
	if strings.Contains(dir, "__tests__") || strings.Contains(dir, "__test__") {
		return true
	}

	return false
}
