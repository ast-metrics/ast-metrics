package module

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocateReadsTheCrateAndTheModule(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src", "artifact"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Cargo.toml"), []byte("[package]\nname = \"demo-app\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := NewCache()
	for path, expected := range map[string]string{
		"src/lib.rs":          "demo_app",
		"src/artifact/mod.rs": "demo_app::artifact",
		"src/artifact/foo.rs": "demo_app::artifact::foo",
		"src/clearing.rs":     "demo_app::clearing",
	} {
		location, found := cache.Locate(filepath.Join(root, path))
		if !found {
			t.Errorf("expected %q to be located", path)
			continue
		}
		if got := location.Path(); got != expected {
			t.Errorf("Locate(%q).Path() = %q, expected %q", path, got, expected)
		}
	}
	if _, found := cache.Locate(filepath.Join(root, "build.rs")); found {
		t.Error("expected a file outside of src/ not to be located")
	}
}

func TestLocateNamesACrateWithoutManifestCrate(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	location, _ := NewCache().Locate(filepath.Join(root, "src", "a.rs"))
	if got := location.Path(); got != "crate::a" {
		t.Errorf("expected crate::a, got %q", got)
	}
}

func TestAnchor(t *testing.T) {
	from := Location{Name: "demo", Module: "artifact::importer"}
	for path, expected := range map[string]string{
		"crate::clearing":        "demo::clearing",
		"crate":                  "demo",
		"self::reader":           "demo::artifact::importer::reader",
		"super::shared":          "demo::artifact::shared",
		"super::super::shared":   "demo::shared",
		"super::super::super::x": "super::super::super::x",
		"serde::de":              "serde::de",
	} {
		if got := Anchor(from, path); got != expected {
			t.Errorf("Anchor(%q) = %q, expected %q", path, got, expected)
		}
	}
}

func TestModuleOf(t *testing.T) {
	const sourceRoot = "/project/src"

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "library root", path: "/project/src/lib.rs", want: ""},
		{name: "binary root", path: "/project/src/main.rs", want: ""},
		{name: "module written as a file", path: "/project/src/model.rs", want: "model"},
		{name: "module written as a directory", path: "/project/src/store/mod.rs", want: "store"},
		{name: "nested module", path: "/project/src/store/inner.rs", want: "store::inner"},
		{name: "nested directory module", path: "/project/src/a/b/mod.rs", want: "a::b"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ModuleOf(sourceRoot, test.path); got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}

func TestPackageNameIn(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "plain package",
			content: "[package]\nname = \"demo\"\nversion = \"0.1.0\"\n",
			want:    "demo",
		},
		{
			// Cargo lets a crate be named with dashes and referred to in code
			// with underscores, which is the form a use path carries.
			name:    "dashes become underscores",
			content: "[package]\nname = \"demo-core\"\n",
			want:    "demo_core",
		},
		{
			name:    "a name outside the package section is not the crate name",
			content: "[dependencies]\nname = \"serde\"\n\n[package]\nname = \"demo\"\n",
			want:    "demo",
		},
		{
			name:    "a virtual workspace manifest declares no package",
			content: "[workspace]\nmembers = [\"core\", \"app\"]\n",
			want:    "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := PackageNameIn(test.content); got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}
