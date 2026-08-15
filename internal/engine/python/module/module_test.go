package module

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestModuleOfFollowsThePackageChain(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"company/__init__.py", "company/product/__init__.py", "company/product/artifact/__init__.py", "company/product/artifact/entrypoint.py"} {
		write(t, filepath.Join(root, p))
	}
	cache := NewCache()
	for path, expected := range map[string]string{
		"company/product/artifact/entrypoint.py": "company.product.artifact.entrypoint",
		"company/product/artifact/__init__.py":   "company.product.artifact",
		"company/__init__.py":                    "company",
	} {
		if got := cache.ModuleOf(filepath.Join(root, path)); got != expected {
			t.Errorf("ModuleOf(%q) = %q, expected %q", path, got, expected)
		}
	}
}

// A namespace package has no __init__.py: the module is read from the root of
// the project, the src/ directory of the src layout left out.
func TestModuleOfReadsANamespacePackageFromTheProjectRoot(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "pyproject.toml"))
	write(t, filepath.Join(root, "src/company/product/clearing/clearing.py"))
	write(t, filepath.Join(root, "scripts/deploy.py"))
	// A regular package below a namespace package: the chain stops at the
	// package, and the directories above come from the project root.
	write(t, filepath.Join(root, "src/company/product/shared/__init__.py"))
	write(t, filepath.Join(root, "src/company/product/shared/util.py"))

	cache := NewCache()
	for path, expected := range map[string]string{
		"src/company/product/clearing/clearing.py": "company.product.clearing.clearing",
		"src/company/product/shared/util.py":       "company.product.shared.util",
		"scripts/deploy.py":                        "scripts.deploy",
	} {
		if got := cache.ModuleOf(filepath.Join(root, path)); got != expected {
			t.Errorf("ModuleOf(%q) = %q, expected %q", path, got, expected)
		}
	}
}

func TestModuleOfKeepsTheBareNameOutsideOfAnyProject(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "lonely/script.py"))
	if got := NewCache().ModuleOf(filepath.Join(root, "lonely/script.py")); got != "script" {
		t.Errorf("expected the bare name, got %q", got)
	}
}

func TestRebase(t *testing.T) {
	for _, test := range []struct {
		module    string
		isPackage bool
		specifier string
		expected  string
		ok        bool
	}{
		{"a.b.c", false, "d", "d", true},
		{"a.b.c", false, ".d", "a.b.d", true},
		{"a.b.c", false, "..d", "a.d", true},
		{"a.b.c", false, ".", "a.b", true},
		{"a.b", true, ".d", "a.b.d", true},
		{"a.b.c", false, "....d", "", false},
		{"", false, ".d", "", false},
	} {
		got, ok := Rebase(test.module, test.isPackage, test.specifier)
		if got != test.expected || ok != test.ok {
			t.Errorf("Rebase(%q, %v, %q) = %q, %v, expected %q, %v", test.module, test.isPackage, test.specifier, got, ok, test.expected, test.ok)
		}
	}
}
