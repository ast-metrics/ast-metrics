package scm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindGitRoot(t *testing.T) {
	// Test case where .git directory is found
	t.Run("finds .git directory", func(t *testing.T) {
		// Setup a temporary directory with a .git directory inside
		tmpDir := t.TempDir()

		gitDir := filepath.Join(tmpDir, ".git")
		err := os.Mkdir(gitDir, 0755)
		if err != nil {
			t.Fatal(err)
		}

		got, err := FindGitRoot(tmpDir)
		if err != nil {
			t.Fatalf("findGitRoot() error = %v", err)
		}
		if got != tmpDir {
			t.Errorf("findGitRoot() = %v, want %v", got, tmpDir)
		}
	})

	// Test case where no .git directory is found
	t.Run("does not find .git directory", func(t *testing.T) {
		// A temp directory may itself live below a repository (for example when
		// TMPDIR points into a checkout), so use the filesystem root as the one
		// path whose ancestors cannot contain another .git entry.
		volumeRoot := filepath.VolumeName(os.TempDir()) + string(filepath.Separator)
		_, err := FindGitRoot(volumeRoot)
		if err == nil {
			t.Fatal("Expected error, got nil")
		}
		if !strings.Contains(err.Error(), "no git repository found") {
			t.Errorf("Unexpected error message: %v", err)
		}
	})
}
