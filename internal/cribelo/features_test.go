package cribelo

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReadingMinutes(t *testing.T) {
	assert.Equal(t, 1, readingMinutes(0, 0))
	assert.Equal(t, 1, readingMinutes(1, 0))
	assert.Equal(t, 1, readingMinutes(20, 0))
	assert.Equal(t, 2, readingMinutes(21, 0))
	assert.Equal(t, 1, readingMinutes(0, 60), "sixty glanced lines a minute")
	assert.Equal(t, 2, readingMinutes(10, 40), "half a minute of reading and two thirds of a minute of glancing")
	assert.Equal(t, 5, readingMinutes(100, 0))
}

func TestStemOf(t *testing.T) {
	assert.Equal(t, "invoice", stemOf("pkg/invoice.go"))
	assert.Equal(t, "invoice_test", stemOf("pkg/invoice_test.go"))
	assert.Equal(t, "SessionTest", stemOf("SessionTest.php"))
	assert.Equal(t, "Makefile", stemOf("Makefile"))
	assert.Equal(t, ".env", stemOf("a/.env"), "leading dot is not an extension")
	assert.Equal(t, "a", stemOf("a.b.c"), "cut at first dot")
}

func TestHasRelatedTest(t *testing.T) {
	assert.False(t, hasRelatedTest("pkg/invoice.go", nil))
	assert.False(t, hasRelatedTest("pkg/invoice.go", []string{}))
	assert.True(t, hasRelatedTest("pkg/invoice.go", []string{"pkg/other_test.go"}), "same dir")
	assert.True(t, hasRelatedTest("pkg/invoice.go", []string{"tests/invoice_test.go"}), "stem match")
	assert.True(t, hasRelatedTest("src/Session.php", []string{"tests/SessionTest.php"}), "case-insensitive stem")
	assert.True(t, hasRelatedTest("auth/token.py", []string{"tests/test_token.py"}))
	assert.False(t, hasRelatedTest("pkg/invoice.go", []string{"tests/customer_test.go", "other/x_test.go"}))
	assert.False(t, hasRelatedTest("pkg/invoice.go", []string{"pkg2/a_test.go"}), "dir must match exactly")
}

func TestTouchedFunctionGrowth(t *testing.T) {
	assert.Equal(t, 4, TouchedFunction{New: true, After: 5}.growth())
	assert.Equal(t, 0, TouchedFunction{New: true, After: 1}.growth())
	assert.Equal(t, 3, TouchedFunction{Before: 2, After: 5}.growth())
	assert.Equal(t, -2, TouchedFunction{Before: 5, After: 3}.growth())
}

func TestCyclomaticNilSafe(t *testing.T) {
	assert.Equal(t, 0, cyclomatic(nil))
}

func TestAttributeReadsSplitsAHunkOverTheFunctionsItSpans(t *testing.T) {
	functions := []TouchedFunction{
		{Name: "a", Line: 10, End: 19, After: 6, Before: 2},
		{Name: "b", Line: 30, End: 49, After: 2, Before: 2, Glance: true},
		{Name: "inner", Line: 35, End: 39, After: 1, New: true, Glance: true},
	}
	hunks := []HunkReport{
		{Start: 1, End: 50, Added: 50, Code: 50, counted: true, Nature: NatureBehavior, decided: decidedAt(50, 3, 4)}, // a whole new file, two branching lines in the header
		{Start: 60, End: 60, Added: 0, Deleted: 4, Code: 4, counted: true, Nature: NatureBehavior},
		{Start: 12, End: 12, Added: 1, Nature: NatureFormat},
	}
	attributeReads(functions, hunks)
	assert.Equal(t, 10, functions[0].ToRead, "lines 10-19")
	assert.Equal(t, 15, functions[1].ToRead, "lines 30-49 minus the inner function")
	assert.Equal(t, 5, functions[2].ToRead, "lines 35-39 go to the innermost function")
	assert.Equal(t, 20+18, hunks[0].Glance, "the two glance functions, and the 20 lines outside minus the two that branch")
	assert.Equal(t, 4, hunks[1].Glance, "a deletion outside every function, no decision")
}

func TestTallySplitsReadAndGlance(t *testing.T) {
	f := FileReport{
		Functions: []TouchedFunction{
			{Name: "parse", Line: 10, End: 30, Before: 4, After: 7},        // gained decisions: read
			{Name: "name", Line: 40, End: 42, Before: 1, After: 1},         // straight line: glance
			{Name: "build", Line: 50, End: 80, Before: 12, After: 12},      // no gain but complex: read
			{Name: "newGetter", Line: 90, End: 92, After: 1, New: true},    // new one-path function: glance
			{Name: "newBranchy", Line: 100, End: 120, After: 5, New: true}, // new function with decisions: read
		},
		Hunks: []HunkReport{
			{Start: 12, End: 21, Added: 10, Code: 10, counted: true, Nature: NatureBehavior},
			{Start: 41, End: 41, Added: 1, Code: 1, counted: true, Nature: NatureBehavior},
			{Start: 60, End: 64, Added: 5, Code: 5, counted: true, Nature: NatureBehavior},
			{Start: 90, End: 92, Added: 3, Code: 3, counted: true, Nature: NatureBehavior},
			{Start: 100, End: 119, Added: 20, Code: 20, counted: true, Nature: NatureBehavior, decided: decidedAt(20, 2, 5, 9, 14)},
			{Start: 130, End: 133, Added: 4, Code: 4, counted: true, Nature: NatureBehavior, decided: make([]bool, 4)}, // a struct field block outside every function
			{Start: 140, End: 141, Added: 2, Code: 2, counted: true, Nature: NatureBehavior, decided: decidedAt(2, 0)}, // a top-level conditional and its body
		},
	}
	f.tally()
	assert.Equal(t, 10+5+20+1, f.ToRead)
	assert.Equal(t, 1+3+4+1, f.ToGlance, "the body line of the top-level conditional holds no decision itself")
	assert.True(t, f.Functions[1].Glance)
	assert.True(t, f.Functions[3].Glance)
	assert.False(t, f.Functions[4].Glance)
	assert.Equal(t, 2, f.Minutes, "36 lines read at 20 a minute, 9 glanced at 60")
}

func TestCodeLinesLeaveOutBlankCommentAndImportLines(t *testing.T) {
	lines := []string{
		"import \"fmt\"",
		"",
		"// the answer",
		"func answer() int {",
		"\treturn 42 // inline comments stay code",
		"}",
	}
	assert.Equal(t, 3, codeLines("Golang", lines))
	assert.Equal(t, 1, codeLines("Python", []string{"from os import path", "# comment", "x = path.join(a, b)"}))
}

func TestDecidedMarksBranchesAndLoops(t *testing.T) {
	assert.Equal(t, []bool{false, false, false}, decided([]string{"x := 1", "// if this were code", "type T struct{ A int }"}))
	assert.Equal(t, []bool{true, true, false}, decided([]string{"if a && b {", "for i := range xs {", "gifted := format(x)"}), "words that contain a keyword are not decisions")
}

// decidedAt builds a decided slice of n lines with the given offsets set.
func decidedAt(n int, offsets ...int) []bool {
	out := make([]bool, n)
	for _, o := range offsets {
		out[o] = true
	}
	return out
}
