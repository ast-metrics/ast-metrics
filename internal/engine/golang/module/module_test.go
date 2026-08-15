package module

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestModulePathIn(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "plain declaration",
			content: "module example.com/demo\n\ngo 1.21\n",
			want:    "example.com/demo",
		},
		{
			name:    "leading blank lines and comments",
			content: "// a comment\n\n   module   example.com/demo   \n",
			want:    "example.com/demo",
		},
		{
			name:    "trailing comment",
			content: "module example.com/demo // the main module\n",
			want:    "example.com/demo",
		},
		{
			name:    "quoted path",
			content: "module \"example.com/demo\"\n",
			want:    "example.com/demo",
		},
		{
			// The require block also holds lines starting with a module path,
			// but the directive itself comes first.
			name:    "require block does not win",
			content: "module example.com/demo\n\nrequire (\n\tmodule.example/other v1.0.0\n)\n",
			want:    "example.com/demo",
		},
		{
			name:    "no module directive",
			content: "go 1.21\n",
			want:    "",
		},
		{
			// "modules" is not "module": a prefix match alone would take it.
			name:    "a longer word is not the directive",
			content: "modules example.com/demo\n",
			want:    "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := modulePathIn(test.content); got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}

func TestImportPathOf(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "internal", "analyzer")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	cache := NewCache()
	for _, test := range []struct{ directory, expected string }{
		{root, "example.com/demo"},
		{nested, "example.com/demo/internal/analyzer"},
	} {
		if got := cache.ImportPathOf(test.directory); got != test.expected {
			t.Errorf("ImportPathOf(%q) = %q, expected %q", test.directory, got, test.expected)
		}
	}

	// A directory standing above every go.mod belongs to no module.
	if got := cache.ImportPathOf(filepath.Dir(root)); got != "" {
		t.Errorf("expected no import path above the module, got %q", got)
	}
}

// The files of a project are parsed in parallel, so the cache is read and
// filled from several goroutines at once. Run with -race.
func TestImportPathOfIsSafeForConcurrentUse(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cache := NewCache()
	var group sync.WaitGroup
	for i := 0; i < 32; i++ {
		group.Add(1)
		go func(i int) {
			defer group.Done()
			directory := filepath.Join(root, "package", string(rune('a'+i%26)))
			cache.ImportPathOf(directory)
		}(i)
	}
	group.Wait()
}
