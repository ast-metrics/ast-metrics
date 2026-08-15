package module

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocateNamesTheModuleFromTheRootOfThePackage(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := NewCache()
	for path, expected := range map[string]string{
		"src/artifact/entrypoint.ts": "src/artifact/entrypoint",
		"src/shared/index.ts":        "src/shared",
		"src/App.tsx":                "src/App",
		"src/types.d.ts":             "src/types",
		"index.ts":                   ".",
	} {
		location, found := cache.Locate(filepath.Join(root, path))
		if !found {
			t.Errorf("expected %q to be located", path)
			continue
		}
		if location.Module != expected {
			t.Errorf("Locate(%q).Module = %q, expected %q", path, location.Module, expected)
		}
	}
}

func TestLocateFindsNothingWithoutAManifest(t *testing.T) {
	root := t.TempDir()
	if _, found := NewCache().Locate(filepath.Join(root, "lonely.ts")); found {
		t.Error("expected no location without a manifest")
	}
}

func TestAnchor(t *testing.T) {
	from := Location{Root: "/project", Module: "src/artifact/entrypoint"}
	for specifier, expected := range map[string]string{
		"../clearing/clearing": "src/clearing/clearing",
		"./importer":           "src/artifact/importer",
		"./importer.js":        "src/artifact/importer",
		"../shared":            "src/shared",
		"../shared/index":      "src/shared",
		"../../index":          ".",
		"../../../outside":     "../../../outside",
		"react":                "react",
		"@/components/button":  "@/components/button",
	} {
		if got := Anchor(from, specifier); got != expected {
			t.Errorf("Anchor(%q) = %q, expected %q", specifier, got, expected)
		}
	}
}
