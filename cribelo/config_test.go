package cribelo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfigMissing(t *testing.T) {
	c, err := LoadConfig(t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, Config{}, c)
}

func TestLoadConfigParses(t *testing.T) {
	dir := t.TempDir()
	yaml := "base: origin/develop\ntop: 5\nfail_on: [cycle, signature]\nfail_over: 1h\nignore:\n  - docs/**\n  - \"*.generated.ts\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, ConfigFile), []byte(yaml), 0o644))
	c, err := LoadConfig(dir)
	require.NoError(t, err)
	assert.Equal(t, Config{Base: "origin/develop", Top: 5, FailOn: FactList{"cycle", "signature"}, FailOver: "1h", Ignore: []string{"docs/**", "*.generated.ts"}}, c)

	require.NoError(t, os.WriteFile(filepath.Join(dir, ConfigFile), []byte("fail_on: cycle, signature\n"), 0o644))
	c, err = LoadConfig(dir)
	require.NoError(t, err)
	assert.Equal(t, FactList{"cycle", "signature"}, c.FailOn)
}

func TestLoadConfigInvalid(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ConfigFile), []byte("top: [1, 2\n"), 0o644))
	_, err := LoadConfig(dir)
	assert.Error(t, err)
}

func TestConfigIgnored(t *testing.T) {
	c := Config{Ignore: []string{"docs/**", "*.generated.ts", "internal/gen/*.go", "Makefile"}}
	assert.True(t, c.Ignored("docs/a.md"))
	assert.True(t, c.Ignored("docs/deep/er/a.md"))
	assert.False(t, c.Ignored("docs"))
	assert.False(t, c.Ignored("mydocs/a.md"))
	assert.True(t, c.Ignored("x.generated.ts"))
	assert.True(t, c.Ignored("src/deep/x.generated.ts"), "basename match")
	assert.False(t, c.Ignored("src/x.generated.js"))
	assert.True(t, c.Ignored("internal/gen/a.go"))
	assert.False(t, c.Ignored("internal/gen/sub/a.go"))
	assert.True(t, c.Ignored("sub/Makefile"))
	assert.False(t, c.Ignored("main.go"))
	assert.False(t, Config{}.Ignored("main.go"))
}
