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
	assert.Equal(t, 1, readingMinutes(10, 10))
	assert.Equal(t, 2, readingMinutes(11, 10))
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
