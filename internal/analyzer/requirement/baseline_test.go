package requirement

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBaseline_FilterIgnoresKnownViolations(t *testing.T) {
	baseline := NewBaselineFromOutcomes([]RuleOutcome{
		{Rule: "cyclomatic_complexity", File: "a.go", Message: "too complex"},
	})

	outcomes := []RuleOutcome{
		{Rule: "cyclomatic_complexity", File: "a.go", Message: "too complex"},
		{Rule: "cyclomatic_complexity", File: "b.go", Message: "too complex"},
	}

	kept, baselined := baseline.Filter(outcomes)

	assert.Equal(t, 1, baselined)
	assert.Equal(t, 1, len(kept))
	assert.Equal(t, "b.go", kept[0].File)
}

func TestBaseline_FilterOnlyHidesBaselinedCount(t *testing.T) {
	// One occurrence was baselined; a second, newly introduced occurrence of
	// the exact same message must still be reported.
	baseline := NewBaselineFromOutcomes([]RuleOutcome{
		{Rule: "max_public_methods", File: "a.go", Message: "too many public methods"},
	})

	outcomes := []RuleOutcome{
		{Rule: "max_public_methods", File: "a.go", Message: "too many public methods"},
		{Rule: "max_public_methods", File: "a.go", Message: "too many public methods"},
	}

	kept, baselined := baseline.Filter(outcomes)

	assert.Equal(t, 1, baselined)
	assert.Equal(t, 1, len(kept))
}

func TestBaseline_SaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.yaml")

	original := NewBaselineFromOutcomes([]RuleOutcome{
		{Rule: "max_loc", File: "a.go", Message: "too many lines"},
		{Rule: "max_loc", File: "a.go", Message: "too many lines"},
		{Rule: "max_loc", File: "b.go", Message: "too many lines"},
	})
	assert.NoError(t, original.Save(path))

	loaded, err := LoadBaseline(path)
	assert.NoError(t, err)
	assert.Equal(t, 2, loaded.Len())

	kept, baselined := loaded.Filter([]RuleOutcome{
		{Rule: "max_loc", File: "a.go", Message: "too many lines"},
		{Rule: "max_loc", File: "a.go", Message: "too many lines"},
		{Rule: "max_loc", File: "b.go", Message: "too many lines"},
	})
	assert.Equal(t, 3, baselined)
	assert.Equal(t, 0, len(kept))
}

func TestLoadBaseline_MissingFileIsNotAnError(t *testing.T) {
	baseline, err := LoadBaseline(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	assert.NoError(t, err)
	assert.Equal(t, 0, baseline.Len())
}

func TestResolveBaselinePath(t *testing.T) {
	assert.Equal(t, "custom.yaml", ResolveBaselinePath("custom.yaml"))

	dir := t.TempDir()
	wd, err := os.Getwd()
	assert.NoError(t, err)
	defer os.Chdir(wd)
	assert.NoError(t, os.Chdir(dir))

	assert.Equal(t, "", ResolveBaselinePath(""))

	assert.NoError(t, os.WriteFile(DefaultBaselineFilename, []byte("ignored: []"), 0644))
	assert.Equal(t, DefaultBaselineFilename, ResolveBaselinePath(""))
}
