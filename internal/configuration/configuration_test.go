package configuration

import (
	"testing"
)

func TestConfigurationAcceptsSourcesToAnalyzePath(t *testing.T) {
	// create temporary folders (portable)
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	configuration := NewConfiguration()
	err := configuration.SetSourcesToAnalyzePath([]string{dir1, dir2})
	if err != nil {
		t.Errorf("Error setting sources to analyze path")
		return
	}

	// expect two entries
	if len(configuration.SourcesToAnalyzePath) != 2 {
		t.Errorf("SourcesToAnalyzePath should have 2 elements")
		return
	}

	if configuration.SourcesToAnalyzePath[0] != dir1 {
		t.Errorf("SourcesToAnalyzePath = %s; want %s", configuration.SourcesToAnalyzePath[0], dir1)
	}

	if configuration.SourcesToAnalyzePath[1] != dir2 {
		t.Errorf("SourcesToAnalyzePath = %s; want %s", configuration.SourcesToAnalyzePath[1], dir2)
	}
}

func TestGetExtensionsForLanguage(t *testing.T) {
	t.Run("returns default extension when no extras configured", func(t *testing.T) {
		config := NewConfiguration()
		exts := config.GetExtensionsForLanguage("php")
		if len(exts) != 1 || exts[0] != ".php" {
			t.Errorf("Expected [.php], got %v", exts)
		}
	})

	t.Run("merges extra extensions with default", func(t *testing.T) {
		config := NewConfiguration()
		config.Extensions = map[string][]string{
			"php": {".inc", ".module"},
		}
		exts := config.GetExtensionsForLanguage("php")
		if len(exts) != 3 {
			t.Errorf("Expected 3 extensions, got %d: %v", len(exts), exts)
		}
	})

	t.Run("adds dot prefix if missing", func(t *testing.T) {
		config := NewConfiguration()
		config.Extensions = map[string][]string{
			"php": {"inc"},
		}
		exts := config.GetExtensionsForLanguage("php")
		found := false
		for _, e := range exts {
			if e == ".inc" {
				found = true
			}
		}
		if !found {
			t.Errorf("Expected .inc in extensions, got %v", exts)
		}
	})

	t.Run("deduplicates extensions", func(t *testing.T) {
		config := NewConfiguration()
		config.Extensions = map[string][]string{
			"php": {".php", ".inc"},
		}
		exts := config.GetExtensionsForLanguage("php")
		if len(exts) != 2 {
			t.Errorf("Expected 2 unique extensions, got %d: %v", len(exts), exts)
		}
	})
}

func TestConfigurationAcceptsExcludePatterns(t *testing.T) {

	configuration := NewConfiguration()
	configuration.SetExcludePatterns([]string{"/foo"})

	if configuration.ExcludePatterns[0] != "/foo" {
		t.Errorf("ExcludePatterns = %s; want %s", configuration.ExcludePatterns[0], "/foo")
	}
}

func TestConfigurationAllExtensions(t *testing.T) {
	configuration := NewConfiguration()

	all := configuration.AllExtensions()

	for _, ext := range []string{".php", ".go", ".py", ".rs", ".ts", ".java", ".cs", ".cpp", ".cc", ".cxx", ".hpp", ".hh", ".hxx"} {
		found := false
		for _, candidate := range all {
			if candidate == ext {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AllExtensions() = %v, want it to contain built-in %s", all, ext)
		}
	}

	configuration.Extensions = map[string][]string{"cpp": {".h", ".ino"}}
	all = configuration.AllExtensions()
	for _, ext := range []string{".h", ".ino"} {
		found := false
		for _, candidate := range all {
			if candidate == ext {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AllExtensions() = %v, want it to contain declared %s", all, ext)
		}
	}
}
