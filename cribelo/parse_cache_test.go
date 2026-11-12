package cribelo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func cacheEntries(t *testing.T, root string) int {
	t.Helper()
	count := 0
	filepath.WalkDir(filepath.Join(root, ".git", "cribelo", "parsed"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() != "swept" {
			count++
		}
		return nil
	})
	return count
}

func TestParseTreeReadsBackWhatItParsed(t *testing.T) {
	root := initRepo(t)
	put(t, root, "go.mod", "module example.com/app\n\ngo 1.22\n")
	put(t, root, "svc/a.go", simpleProcess)
	put(t, root, "svc/b.go", complexProcess)
	put(t, root, "README.md", "# app\n")
	tree := []string{"go.mod", "svc/a.go", "svc/b.go", "README.md"}

	SetParseCache(false)
	plain, err := parseTree(root, tree)
	require.NoError(t, err)
	assert.Equal(t, 0, cacheEntries(t, root), "nothing is kept when the cache is off")

	SetParseCache(true)
	defer SetParseCache(true)
	cold, err := parseTree(root, tree)
	require.NoError(t, err)
	assert.Equal(t, 2, cacheEntries(t, root))
	warm, err := parseTree(root, tree)
	require.NoError(t, err)

	require.Len(t, warm.All, 2)
	for i := range plain.All {
		assert.True(t, proto.Equal(plain.All[i], cold.All[i]), "cold: %s", plain.All[i].Path)
		assert.True(t, proto.Equal(plain.All[i], warm.All[i]), "warm: %s", plain.All[i].Path)
	}
	assert.Equal(t, filepath.Join(root, "svc", "a.go"), warm.Files["svc/a.go"].Path)

	// A file that changes is parsed again, the other is read back.
	put(t, root, "svc/a.go", complexProcess)
	_, err = parseTree(root, tree)
	require.NoError(t, err)
	assert.Equal(t, 3, cacheEntries(t, root))

	// A manifest that changes names the packages anew: everything is parsed again.
	put(t, root, "go.mod", "module example.com/other\n\ngo 1.22\n")
	renamed, err := parseTree(root, tree)
	require.NoError(t, err)
	assert.Equal(t, 5, cacheEntries(t, root))
	assert.False(t, proto.Equal(warm.Files["svc/b.go"], renamed.Files["svc/b.go"]), "the package is named after the module")
}

func TestParseTreeWithoutGitKeepsNothing(t *testing.T) {
	root := t.TempDir()
	put(t, root, "svc/a.go", simpleProcess)
	parsed, err := parseTree(root, []string{"svc/a.go"})
	require.NoError(t, err)
	require.Len(t, parsed.All, 1)
	_, err = os.Stat(filepath.Join(root, ".git"))
	assert.True(t, os.IsNotExist(err))
}
