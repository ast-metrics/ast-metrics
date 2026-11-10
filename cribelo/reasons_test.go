package cribelo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func contrib(f Feature, value, logit float64) Contribution {
	return Contribution{Feature: f, Value: value, Logit: logit}
}

func TestLinesPhrase(t *testing.T) {
	assert.Equal(t, "+12 lines", linesPhrase(12, 0))
	assert.Equal(t, "+0 lines", linesPhrase(0, 0))
	assert.Equal(t, "−3 lines", linesPhrase(0, 3))
	assert.Equal(t, "+5/−2 lines", linesPhrase(5, 2))
}

func TestShortName(t *testing.T) {
	assert.Equal(t, "Process", ShortName("Invoice::Process"))
	assert.Equal(t, "Process", ShortName("Process"))
	assert.Equal(t, "c", ShortName("a::b::c"))
}

func TestReasonsNewFunction(t *testing.T) {
	r := &FileReport{
		Added:         40,
		Functions:     []TouchedFunction{{Name: "Billing::Compute", New: true, After: 6}},
		Contributions: []Contribution{contrib(FeatureComplexityAdded, 5, 2)},
	}
	assert.Equal(t, []string{"new Compute() complexity 6"}, reasons(r))
}

func TestReasonsGrownFunctionWithMore(t *testing.T) {
	r := &FileReport{
		Added: 40,
		Functions: []TouchedFunction{
			{Name: "A", Before: 2, After: 7},
			{Name: "B", Before: 1, After: 3},
			{Name: "C", Before: 3, After: 3},
			{Name: "D", New: true, After: 4},
		},
		Contributions: []Contribution{contrib(FeatureComplexityAdded, 9, 2), contrib(FeatureMaxComplexity, 7, 1)},
	}
	assert.Equal(t, []string{"A() complexity 2 → 7, +2 more"}, reasons(r), "complexity features collapse into one phrase")
}

func TestReasonsUnchangedComplexityFunction(t *testing.T) {
	r := &FileReport{
		Functions:     []TouchedFunction{{Name: "A", Before: 4, After: 4}},
		Contributions: []Contribution{contrib(FeatureMaxComplexity, 4, 1)},
	}
	assert.Equal(t, []string{"A() complexity 4"}, reasons(r))
}

func TestReasonsTrivialComplexityFallsBackToLines(t *testing.T) {
	r := &FileReport{
		Added: 8, Deleted: 0,
		Functions:     []TouchedFunction{{Name: "A", Before: 1, After: 1}},
		Contributions: []Contribution{contrib(FeatureComplexityAdded, 0, 1)},
	}
	assert.Equal(t, []string{"+8 lines"}, reasons(r))
	r.Functions = nil
	assert.Equal(t, []string{"+8 lines"}, reasons(r))
}

func TestReasonsNumbers(t *testing.T) {
	r := &FileReport{
		Added: 1,
		Contributions: []Contribution{
			contrib(FeatureAfferent, 12, 3),
			contrib(FeatureFixes90d, 3, 2),
			contrib(FeatureChurn90d, 5, 1),
		},
	}
	assert.Equal(t, []string{"imported by 12 files", "fixed 3× in 90 days", "5 commits in 90 days"}, reasons(r))
}

func TestReasonsSimpleFeatures(t *testing.T) {
	r := &FileReport{Contributions: []Contribution{
		contrib(FeatureNewFile, 1, 3), contrib(FeatureUntested, 1, 2), contrib(FeatureLinesAdded, 9, 1),
	}, Added: 9, Deleted: 4}
	assert.Equal(t, []string{"new file", "no test touched", "+9/−4 lines"}, reasons(r))
}

func TestReasonsLinesDeduplicated(t *testing.T) {
	r := &FileReport{Added: 0, Deleted: 30, Contributions: []Contribution{
		contrib(FeatureLinesDeleted, 30, 2), contrib(FeatureLinesAdded, 0, 1),
	}}
	assert.Equal(t, []string{"−30 lines"}, reasons(r))
}

func TestReasonsClone(t *testing.T) {
	r := &FileReport{
		Clone:         &CloneMatch{Path: "pkg/x.go", Function: "Foo ~ T::Bar", Similarity: 0.876},
		Contributions: []Contribution{contrib(FeatureClone, 0.876, 1.3)},
	}
	assert.Equal(t, []string{"Foo() 88% similar to pkg/x.go:Bar()"}, reasons(r))
	// a clone contribution without a match is silently skipped
	r.Clone = nil
	r.Added = 3
	assert.Equal(t, []string{"+3 lines"}, reasons(r))
}

func TestReasonsMaxThree(t *testing.T) {
	r := &FileReport{Added: 10, Contributions: []Contribution{
		contrib(FeatureAfferent, 4, 5), contrib(FeatureFixes90d, 2, 4), contrib(FeatureChurn90d, 6, 3),
		contrib(FeatureNewFile, 1, 2), contrib(FeatureUntested, 1, 1),
	}}
	got := reasons(r)
	assert.Len(t, got, 3)
	assert.Equal(t, []string{"imported by 4 files", "fixed 2× in 90 days", "6 commits in 90 days"}, got)
}

func TestReasonsNeverMentionNegatives(t *testing.T) {
	r := &FileReport{Added: 7, Contributions: []Contribution{
		contrib(FeatureAfferent, 4, 2),
		contrib(FeatureUntested, 1, 0),
		contrib(FeatureFixes90d, 9, -1),
		contrib(FeatureNewFile, 1, -2),
	}}
	assert.Equal(t, []string{"imported by 4 files"}, reasons(r))
}

func TestReasonsFallbackWhenNoPositive(t *testing.T) {
	r := &FileReport{Added: 7, Deleted: 2, Contributions: []Contribution{contrib(FeatureAfferent, 4, -1)}}
	assert.Equal(t, []string{"+7/−2 lines"}, reasons(r))
	r = &FileReport{Added: 3}
	assert.Equal(t, []string{"+3 lines"}, reasons(r))
}
