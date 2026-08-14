package file

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/configuration"
)

func TestFinder_Search(t *testing.T) {
	t.Run("should return a list of files under multiple directories", func(t *testing.T) {

		// First we create two directories with files (portable)
		base := t.TempDir()
		dir1 := filepath.Join(base, "test1")
		dir2 := filepath.Join(base, "test2")
		_ = os.MkdirAll(dir1, 0o777)
		_ = os.MkdirAll(dir2, 0o777)
		_ = os.WriteFile(filepath.Join(dir1, "file1.js"), []byte("// test\n"), 0o644)
		_ = os.WriteFile(filepath.Join(dir1, "file2.js"), []byte("// test\n"), 0o644)
		_ = os.WriteFile(filepath.Join(dir2, "file3.js"), []byte("// test\n"), 0o644)

		// Then we create a Finder
		finder := Finder{Configuration: configuration.Configuration{SourcesToAnalyzePath: []string{dir1, dir2}}}

		// Then we search for files
		result := finder.Search(".js")

		// Then we check the result
		if len(result.Files) != 3 {
			t.Errorf("Expected 3 files, got %d", len(result.Files))
		}

		if len(result.FilesByDirectory) != 2 {
			t.Errorf("Expected 2 directories, got %d", len(result.FilesByDirectory))
		}

		if len(result.FilesByDirectory[dir1]) != 2 {
			t.Errorf("Expected 2 files in %s, got %d", dir1, len(result.FilesByDirectory[dir1]))
		}

		if len(result.FilesByDirectory[dir2]) != 1 {
			t.Errorf("Expected 1 file in %s, got %d", dir2, len(result.FilesByDirectory[dir2]))
		}

		if !strings.Contains(result.FilesByDirectory[dir1][0], filepath.Join(dir1, "file1.js")) && !strings.Contains(result.FilesByDirectory[dir1][1], filepath.Join(dir1, "file1.js")) {
			t.Errorf("Expected %s in directory listing", filepath.Join(dir1, "file1.js"))
		}

		if !strings.Contains(result.FilesByDirectory[dir1][0], filepath.Join(dir1, "file2.js")) && !strings.Contains(result.FilesByDirectory[dir1][1], filepath.Join(dir1, "file2.js")) {
			t.Errorf("Expected %s in directory listing", filepath.Join(dir1, "file2.js"))
		}
	})
}

func TestFinder_SearchRootLevelFiles(t *testing.T) {
	// A file sitting directly in the analyzed directory (zero sub-directory
	// levels) must be discovered. The recursive "**" glob mishandled this case
	// on some platforms (notably macOS), so it is covered explicitly here.
	t.Run("finds a file directly in the root directory", func(t *testing.T) {
		base := t.TempDir()
		_ = os.WriteFile(filepath.Join(base, "Root.cs"), []byte("// test\n"), 0o644)

		finder := Finder{Configuration: configuration.Configuration{SourcesToAnalyzePath: []string{base}}}
		result := finder.Search(".cs")

		if len(result.Files) != 1 {
			t.Fatalf("Expected 1 root-level .cs file, got %d (%v)", len(result.Files), result.Files)
		}
		if filepath.Base(result.Files[0]) != "Root.cs" {
			t.Errorf("Expected Root.cs, got %s", result.Files[0])
		}
	})

	t.Run("finds root-level and nested files without duplicates", func(t *testing.T) {
		base := t.TempDir()
		nested := filepath.Join(base, "pkg", "sub")
		_ = os.MkdirAll(nested, 0o777)
		_ = os.WriteFile(filepath.Join(base, "Root.java"), []byte("// test\n"), 0o644)
		_ = os.WriteFile(filepath.Join(nested, "Deep.java"), []byte("// test\n"), 0o644)

		finder := Finder{Configuration: configuration.Configuration{SourcesToAnalyzePath: []string{base}}}
		result := finder.Search(".java")

		if len(result.Files) != 2 {
			t.Fatalf("Expected 2 .java files (one root, one nested), got %d (%v)", len(result.Files), result.Files)
		}
	})
}

func TestFinder_ExcludeRelativeToRoot(t *testing.T) {
	// Exclude patterns must never be matched against the absolute path.
	// Otherwise a project whose absolute location happens to contain an
	// excluded segment (e.g. a macOS temp dir under /var/folders, or a
	// project served from /var/www) would be wrongly emptied out by the
	// default "/var/" pattern. Here the sources live outside the project
	// root (the working directory), so matching falls back to the path
	// relative to the analyzed source root.
	t.Run("does not exclude files because of the root's absolute path", func(t *testing.T) {
		// A root that itself sits under a "/var/" path.
		base := filepath.Join(t.TempDir(), "var", "www", "app")
		_ = os.MkdirAll(base, 0o777)
		_ = os.WriteFile(filepath.Join(base, "Service.cs"), []byte("// test\n"), 0o644)

		finder := Finder{Configuration: configuration.Configuration{
			SourcesToAnalyzePath: []string{base},
			ExcludePatterns:      []string{"/var/", "/vendor/", "/node_modules/"},
		}}
		result := finder.Search(".cs")

		if len(result.Files) != 1 {
			t.Fatalf("Expected 1 file (root path under /var/ must not be excluded), got %d (%v)", len(result.Files), result.Files)
		}
	})

	t.Run("still excludes matching sub-directories inside the project", func(t *testing.T) {
		base := t.TempDir()
		vendor := filepath.Join(base, "vendor", "lib")
		varCache := filepath.Join(base, "var", "cache")
		_ = os.MkdirAll(vendor, 0o777)
		_ = os.MkdirAll(varCache, 0o777)
		_ = os.WriteFile(filepath.Join(base, "Main.cs"), []byte("// test\n"), 0o644)
		_ = os.WriteFile(filepath.Join(vendor, "Dep.cs"), []byte("// test\n"), 0o644)
		_ = os.WriteFile(filepath.Join(varCache, "Gen.cs"), []byte("// test\n"), 0o644)

		finder := Finder{Configuration: configuration.Configuration{
			SourcesToAnalyzePath: []string{base},
			ExcludePatterns:      []string{"/vendor/", "/var/"},
		}}
		result := finder.Search(".cs")

		if len(result.Files) != 1 {
			t.Fatalf("Expected 1 file (vendor/ and var/ sub-dirs excluded), got %d (%v)", len(result.Files), result.Files)
		}
		if filepath.Base(result.Files[0]) != "Main.cs" {
			t.Errorf("Expected Main.cs to be the only kept file, got %s", result.Files[0])
		}
	})
}

func TestFinder_ExcludeRelativeToProjectRoot(t *testing.T) {
	// https://github.com/ast-metrics/ast-metrics/issues/147
	// With several sources, patterns are matched relative to the project
	// root (not to each analyzed root), so a pattern can target a file in
	// one source without touching its sibling in another source.
	setup := func(t *testing.T) (root, dirA, dirB string) {
		root = t.TempDir()
		dirA = filepath.Join(root, "a")
		dirB = filepath.Join(root, "b")
		_ = os.MkdirAll(dirA, 0o777)
		_ = os.MkdirAll(dirB, 0o777)
		_ = os.WriteFile(filepath.Join(dirA, "file.php"), []byte("<?php\n"), 0o644)
		_ = os.WriteFile(filepath.Join(dirB, "file.php"), []byte("<?php\n"), 0o644)
		return root, dirA, dirB
	}

	t.Run("Search can exclude a file in one source only", func(t *testing.T) {
		root, dirA, dirB := setup(t)
		finder := Finder{
			Configuration: configuration.Configuration{
				SourcesToAnalyzePath: []string{dirA, dirB},
				ExcludePatterns:      []string{"/b/file"},
			},
			projectRoot: root,
		}
		result := finder.Search(".php")

		if len(result.Files) != 1 {
			t.Fatalf("Expected only a/file.php to remain, got %d files (%v)", len(result.Files), result.Files)
		}
		if result.Files[0] != filepath.Join(dirA, "file.php") {
			t.Errorf("Expected a/file.php to be kept, got %s", result.Files[0])
		}
	})

	t.Run("SearchMultiple can exclude a file in one source only", func(t *testing.T) {
		root, dirA, dirB := setup(t)
		finder := Finder{
			Configuration: configuration.Configuration{
				SourcesToAnalyzePath: []string{dirA, dirB},
				ExcludePatterns:      []string{"/b/file"},
			},
			projectRoot: root,
		}
		results := finder.SearchMultiple([]string{".php"})

		files := results[".php"].Files
		if len(files) != 1 {
			t.Fatalf("Expected only a/file.php to remain, got %d files (%v)", len(files), files)
		}
		if files[0] != filepath.Join(dirA, "file.php") {
			t.Errorf("Expected a/file.php to be kept, got %s", files[0])
		}
	})

	t.Run("project location under /var/ still never matches", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "var", "www", "app")
		src := filepath.Join(root, "src")
		_ = os.MkdirAll(src, 0o777)
		_ = os.WriteFile(filepath.Join(src, "Service.php"), []byte("<?php\n"), 0o644)

		finder := Finder{
			Configuration: configuration.Configuration{
				SourcesToAnalyzePath: []string{src},
				ExcludePatterns:      []string{"/var/", "/vendor/"},
			},
			projectRoot: root,
		}
		result := finder.Search(".php")

		if len(result.Files) != 1 {
			t.Fatalf("Expected 1 file (project path under /var/ must not be excluded), got %d (%v)", len(result.Files), result.Files)
		}
	})
}

func TestMergeFileLists(t *testing.T) {
	t.Run("merges multiple file lists", func(t *testing.T) {
		list1 := FileList{
			Files:            []string{"a.php", "b.php"},
			FilesByDirectory: map[string][]string{"/src": {"a.php", "b.php"}},
		}
		list2 := FileList{
			Files:            []string{"c.inc"},
			FilesByDirectory: map[string][]string{"/src": {"c.inc"}},
		}
		merged := MergeFileLists(list1, list2)
		if len(merged.Files) != 3 {
			t.Errorf("Expected 3 files, got %d", len(merged.Files))
		}
		if len(merged.FilesByDirectory["/src"]) != 3 {
			t.Errorf("Expected 3 files in /src, got %d", len(merged.FilesByDirectory["/src"]))
		}
	})

	t.Run("handles empty lists", func(t *testing.T) {
		merged := MergeFileLists()
		if len(merged.Files) != 0 {
			t.Errorf("Expected 0 files, got %d", len(merged.Files))
		}
	})
}

func TestFinder_SearchMultiple(t *testing.T) {
	t.Run("should find files of multiple extensions in a single walk", func(t *testing.T) {
		base := t.TempDir()
		_ = os.WriteFile(filepath.Join(base, "main.go"), []byte("package main\n"), 0o644)
		_ = os.WriteFile(filepath.Join(base, "index.php"), []byte("<?php\n"), 0o644)
		_ = os.WriteFile(filepath.Join(base, "app.py"), []byte("pass\n"), 0o644)
		_ = os.WriteFile(filepath.Join(base, "lib.rs"), []byte("fn main() {}\n"), 0o644)
		_ = os.WriteFile(filepath.Join(base, "readme.txt"), []byte("hello\n"), 0o644)

		finder := Finder{Configuration: configuration.Configuration{SourcesToAnalyzePath: []string{base}}}
		results := finder.SearchMultiple([]string{".go", ".php", ".py", ".rs"})

		if len(results[".go"].Files) != 1 {
			t.Errorf("Expected 1 .go file, got %d", len(results[".go"].Files))
		}
		if len(results[".php"].Files) != 1 {
			t.Errorf("Expected 1 .php file, got %d", len(results[".php"].Files))
		}
		if len(results[".py"].Files) != 1 {
			t.Errorf("Expected 1 .py file, got %d", len(results[".py"].Files))
		}
		if len(results[".rs"].Files) != 1 {
			t.Errorf("Expected 1 .rs file, got %d", len(results[".rs"].Files))
		}
	})

	// An excluded directory is skipped whole instead of being walked to throw
	// each of its files away afterwards. The result must not change, whatever
	// the shape of the pattern.
	t.Run("skips an excluded directory without changing the result", func(t *testing.T) {
		base := t.TempDir()
		vendor := filepath.Join(base, "vendor", "lib", "deep")
		_ = os.MkdirAll(vendor, 0o777)
		_ = os.WriteFile(filepath.Join(base, "main.go"), []byte("package main\n"), 0o644)
		_ = os.WriteFile(filepath.Join(vendor, "dep.go"), []byte("package lib\n"), 0o644)

		finder := Finder{Configuration: configuration.Configuration{
			SourcesToAnalyzePath: []string{base},
			ExcludePatterns:      []string{"/vendor/"},
		}}
		finder.projectRoot = base
		results := finder.SearchMultiple([]string{".go"})

		if len(results[".go"].Files) != 1 {
			t.Fatalf("Expected 1 .go file (vendor pruned), got %d (%v)", len(results[".go"].Files), results[".go"].Files)
		}
	})

	t.Run("keeps a directory whose pattern is anchored on the end of the path", func(t *testing.T) {
		// "/tmp/$" excludes a file named tmp, not the content of a directory
		// named tmp: pruning on such a pattern would drop files the
		// file-by-file filter keeps.
		base := t.TempDir()
		tmp := filepath.Join(base, "tmp")
		_ = os.MkdirAll(tmp, 0o777)
		_ = os.WriteFile(filepath.Join(tmp, "kept.go"), []byte("package tmp\n"), 0o644)

		finder := Finder{Configuration: configuration.Configuration{
			SourcesToAnalyzePath: []string{base},
			ExcludePatterns:      []string{"/tmp/$"},
		}}
		finder.projectRoot = base
		results := finder.SearchMultiple([]string{".go"})

		if len(results[".go"].Files) != 1 {
			t.Fatalf("Expected 1 .go file (end-anchored pattern must not prune), got %d (%v)", len(results[".go"].Files), results[".go"].Files)
		}
	})

	t.Run("should use SearchMultiple cache in Search", func(t *testing.T) {
		base := t.TempDir()
		_ = os.WriteFile(filepath.Join(base, "main.go"), []byte("package main\n"), 0o644)
		_ = os.WriteFile(filepath.Join(base, "other.go"), []byte("package main\n"), 0o644)

		finder := Finder{Configuration: configuration.Configuration{SourcesToAnalyzePath: []string{base}}}
		discovery := &FileDiscovery{}
		discovery.Precompute(finder, []string{".go"})
		finder.Discovery = discovery

		result := finder.Search(".go")
		if len(result.Files) != 2 {
			t.Errorf("Expected 2 .go files from cache, got %d", len(result.Files))
		}
	})
}
