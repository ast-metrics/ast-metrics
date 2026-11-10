package cribelo

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(path, content string) error { return os.WriteFile(path, []byte(content), 0o644) }

func TestIsFixMessage(t *testing.T) {
	positives := []string{
		"fix: x", "Fix typo", "fixes #12", "fixed the thing", "fixing it", "hotfix", "Hotfix: prod",
		"bugfix", "Revert \"y\"", "reverts abc", "reverted", "Regression in z", "a bug in parser",
		"chore: FIX", "feat(x): fix (y)",
	}
	for _, s := range positives {
		assert.True(t, IsFixMessage(s), s)
	}
	negatives := []string{"prefix", "fixture", "bugle", "suffix", "feat: add thing", "", "refactor", "fixtures update", "debugging"}
	for _, s := range negatives {
		assert.False(t, IsFixMessage(s), s)
	}
}

func TestSplitNul(t *testing.T) {
	assert.Equal(t, []string{"a", "b c", "d"}, splitNul("a\x00b c\x00\x00d\x00"))
	assert.Empty(t, splitNul(""))
	assert.Empty(t, splitNul("\x00\x00"))
}

func TestShortSha(t *testing.T) {
	assert.Equal(t, "abcdefg", shortSha("abcdefghijkl"))
	assert.Equal(t, "abc", shortSha("abc"))
	assert.Equal(t, "abcdefg", shortSha("abcdefg"))
}

const historyOutput = "\x01300\x00fix: second\nb.go\na.go\n\n" +
	"\x01100\x00feat: first\na.go\n\n" +
	"\x01200\x00chore: middle\na.go\n  c.go  \n\n"

func TestParseHistory(t *testing.T) {
	h := parseHistory(historyOutput)
	require.NotNil(t, h)
	assert.Equal(t, []Event{{100, false}, {200, false}, {300, true}}, h.byPath["a.go"], "sorted ascending")
	assert.Equal(t, []Event{{300, true}}, h.byPath["b.go"])
	assert.Equal(t, []Event{{200, false}}, h.byPath["c.go"], "path is trimmed")
	assert.Len(t, h.byPath, 3)
}

func TestParseHistoryEdgeCases(t *testing.T) {
	// a path before any commit header is ignored
	h := parseHistory("orphan.go\n\x01 5\x00s\nx.go\n")
	assert.NotContains(t, h.byPath, "orphan.go")
	// header without subject
	h = parseHistory("\x0150\nx.go\n")
	assert.Equal(t, []Event{{50, false}}, h.byPath["x.go"])
	assert.Empty(t, parseHistory("").byPath)
}

func TestHistoryBeforeWindow(t *testing.T) {
	h := parseHistory(historyOutput)
	// window (at-window, at]
	commits, fixes := h.Before("a.go", 300, 200) // (100, 300]
	assert.Equal(t, 2, commits, "100 excluded, 300 included")
	assert.Equal(t, 1, fixes)

	commits, fixes = h.Before("a.go", 300, 201) // (99, 300]
	assert.Equal(t, 3, commits)
	assert.Equal(t, 1, fixes)

	commits, _ = h.Before("a.go", 299, 1000)
	assert.Equal(t, 2, commits, "300 is after at")

	commits, fixes = h.Before("a.go", 50, 1000)
	assert.Equal(t, 0, commits)
	assert.Equal(t, 0, fixes)

	commits, fixes = h.Before("unknown.go", 300, 1000)
	assert.Equal(t, 0, commits)
	assert.Equal(t, 0, fixes)

	var nilH *History
	commits, fixes = nilH.Before("a.go", 300, 1000)
	assert.Equal(t, 0, commits+fixes)
}

func TestHistoryFixedAfterWindow(t *testing.T) {
	h := parseHistory(historyOutput) // fix on a.go at 300
	// window (at, at+window]
	assert.True(t, h.FixedAfter("a.go", 200, 100), "at+window inclusive")
	assert.False(t, h.FixedAfter("a.go", 200, 99))
	assert.False(t, h.FixedAfter("a.go", 300, 1000), "at itself excluded")
	assert.True(t, h.FixedAfter("a.go", 299, 1))
	assert.False(t, h.FixedAfter("c.go", 0, 1000), "non-fix commit")
	assert.False(t, h.FixedAfter("nope.go", 0, 1000))
	var nilH *History
	assert.False(t, nilH.FixedAfter("a.go", 0, 1000))
}
