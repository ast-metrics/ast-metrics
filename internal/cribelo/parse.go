package cribelo

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/csharp"
	"github.com/ast-metrics/ast-metrics/internal/engine/golang"
	"github.com/ast-metrics/ast-metrics/internal/engine/java"
	"github.com/ast-metrics/ast-metrics/internal/engine/php"
	"github.com/ast-metrics/ast-metrics/internal/engine/python"
	"github.com/ast-metrics/ast-metrics/internal/engine/rust"
	"github.com/ast-metrics/ast-metrics/internal/engine/typescript"
	filefinder "github.com/ast-metrics/ast-metrics/internal/file"
	"github.com/ast-metrics/ast-metrics/internal/review"
	pb "github.com/ast-metrics/ast-metrics/pb"
	log "github.com/sirupsen/logrus"
)

func newRunners() []engine.Engine {
	return []engine.Engine{
		&php.PhpRunner{}, &golang.GolangRunner{}, &python.PythonRunner{}, &rust.RustRunner{},
		&typescript.TypeScriptRunner{}, &java.JavaRunner{}, &csharp.CSharpRunner{},
	}
}

// Parsed is a project parsed and analyzed, indexed by path relative to root.
type Parsed struct {
	Root  string
	Files map[string]*pb.File
	All   []*pb.File
}

// analyzedExtensionList is the list form of analyzedExtensions.
func analyzedExtensionList() []string {
	exts := make([]string, 0, len(analyzedExtensions))
	for ext := range analyzedExtensions {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	return exts
}

// parseFiles parses and analyzes the given files (relative to root). Only
// those files are read: nothing is discovered by walking root, so ignored
// directories, build outputs and sample trees never slow the run down.
func parseFiles(root string, relPaths []string) (*Parsed, error) {
	log.SetLevel(log.ErrorLevel)
	config := configuration.NewConfiguration()
	if err := config.SetSourcesToAnalyzePath([]string{root}); err != nil {
		return nil, err
	}
	absolute := make([]string, 0, len(relPaths))
	for _, rel := range relPaths {
		if analyzedExtensions[strings.ToLower(filepath.Ext(rel))] {
			absolute = append(absolute, filepath.Join(root, filepath.FromSlash(rel)))
		}
	}
	sort.Strings(absolute)
	config.FileDiscovery = filefinder.NewFileDiscoveryFromFiles(absolute, analyzedExtensionList())
	files, err := engine.ParseFiles(config, newRunners())
	if err != nil {
		return nil, err
	}
	files = analyzer.AnalyzeFiles(files, nil)
	parsed := &Parsed{Root: root, Files: make(map[string]*pb.File, len(files)), All: files}
	for _, f := range files {
		if f == nil {
			continue
		}
		parsed.Files[relPath(root, f.Path)] = f
	}
	return parsed, nil
}

// parseSnapshot materializes file contents under dir, keeping relative paths,
// and parses them: the engines read an old version without the tree being
// touched.
func parseSnapshot(dir string, contents map[string][]byte) (*Parsed, error) {
	rels := make([]string, 0, len(contents))
	for rel, data := range contents {
		target := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			return nil, err
		}
		rels = append(rels, rel)
	}
	return parseFiles(dir, rels)
}

func relPath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// afferentCounts returns, per relative path, how many files outside its own
// directory depend on it. Siblings are left out: they share a package or a
// module and lean on each other by construction, which says nothing about
// how far a change travels.
func afferentCounts(p *Parsed) map[string]int {
	counts := map[string]int{}
	if p == nil {
		return counts
	}
	graph := analyzer.FileDependencies(p.All)
	for path, dependents := range graph.Afferent {
		rel := relPath(p.Root, path)
		n := 0
		for _, dependent := range dependents {
			if filepath.Dir(relPath(p.Root, dependent)) != filepath.Dir(rel) {
				n++
			}
		}
		counts[rel] = n
	}
	return counts
}

// buildCloneIndex fingerprints every function of the non-test files.
func buildCloneIndex(p *Parsed) *CloneIndex {
	idx := NewCloneIndex()
	if p == nil {
		return idx
	}
	// Files and functions are indexed in a fixed order so that two functions
	// equally similar to a candidate are always told apart the same way.
	rels := make([]string, 0, len(p.Files))
	for rel := range p.Files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		file := p.Files[rel]
		if file.GetIsTest() || IsTestPath(rel) || IsGeneratedPath(rel) {
			continue
		}
		data, err := os.ReadFile(file.Path)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		functions := review.CollectFunctions(file)
		names := make([]string, 0, len(functions))
		for name := range functions {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fn := functions[name]
			if fn.Location == nil {
				continue
			}
			idx.Add(rel, name, int(fn.Location.StartLine), sliceLines(lines, int(fn.Location.StartLine), int(fn.Location.EndLine)))
		}
	}
	return idx
}
