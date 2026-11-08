package cribelo

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOldRange(t *testing.T) {
	for _, tc := range []struct {
		header       string
		start, count int
		ok           bool
	}{
		{"@@ -12,3 +12,4 @@ func x()", 12, 3, true},
		{"@@ -7 +7 @@", 7, 1, true},
		{"@@ -0,0 +1,5 @@", 0, 0, true},
		{"@@ +1 @@", 0, 0, false},
		{"not a hunk", 0, 0, false},
	} {
		start, count, ok := oldRange(tc.header)
		assert.Equal(t, tc.ok, ok, tc.header)
		if ok {
			assert.Equal(t, [2]int{tc.start, tc.count}, [2]int{start, count}, tc.header)
		}
	}
}

func TestParseBlameSkipsBlankAndCommentLines(t *testing.T) {
	a := strings.Repeat("a", 40)
	b := strings.Repeat("b", 40)
	out := a + " 1 1 3\nauthor X\ncommitter-time 100\nfilename f.go\n\treturn x\n" +
		a + " 2 2\n\t// a comment\n" +
		a + " 3 3\n\t   \n" +
		b + " 9 4 1\ncommitter-time 200\nfilename f.go\n\tif y {\n"
	lines := parseBlame(out, "f.go")
	require.Len(t, lines, 2)
	assert.Equal(t, blamed{Commit: a, Time: 100, Path: "f.go", Line: 1}, lines[0])
	assert.Equal(t, blamed{Commit: b, Time: 200, Path: "f.go", Line: 9}, lines[1], "the line of the inducing commit")
}

func TestPickInducing(t *testing.T) {
	old, recent := strings.Repeat("1", 40), strings.Repeat("2", 40)
	lines := []blamed{{old, 100, "a.go", 0}, {recent, 200, "a.go", 0}, {recent, 200, "b.go", 0}}

	kept := pickInducing(fixCommit{}, lines)
	assert.Equal(t, []blamed{{recent, 200, "a.go", 0}, {recent, 200, "b.go", 0}}, kept, "R-SZZ keeps the latest candidate")

	kept = pickInducing(fixCommit{Reverts: old}, lines)
	assert.Equal(t, []blamed{{old, 100, "a.go", 0}}, kept, "a revert names its commit")

	var many []blamed
	for i := 0; i <= szzMaxCandidates; i++ {
		many = append(many, blamed{fmt.Sprintf("%040d", i), int64(i), "a.go", 0})
	}
	assert.Empty(t, pickInducing(fixCommit{}, many), "a fix blaming many commits is not targeted")
}

// A change writes a bug in a.go and harmless code in b.go; a later change
// edits b.go elsewhere; a fix repairs the bug. Only a.go of the first change
// is induced, while the fix-touch label marks every landing touching a.go.
func TestInducedLabelsTraceTheRepairedLines(t *testing.T) {
	dir := initRepo(t)
	put(t, dir, "a.go", "package p\n\nfunc A() int {\n\treturn 1\n}\n")
	put(t, dir, "b.go", "package p\n\nfunc B() int {\n\treturn 2\n}\n")
	git(t, dir, "2024-01-01T10:00:00Z", "add", "-A")
	git(t, dir, "2024-01-01T10:00:00Z", "commit", "-q", "-m", "initial")

	put(t, dir, "a.go", "package p\n\nfunc A() int {\n\tx := 10\n\treturn x / 0\n}\n")
	put(t, dir, "b.go", "package p\n\nfunc B() int {\n\treturn 2\n}\n\nfunc C() int {\n\treturn 3\n}\n")
	git(t, dir, "2024-02-01T10:00:00Z", "add", "-A")
	git(t, dir, "2024-02-01T10:00:00Z", "commit", "-q", "-m", "feat: compute A")
	first := strings.TrimSpace(git(t, dir, "", "rev-parse", "HEAD"))

	put(t, dir, "a.go", "package p\n\n// A computes.\nfunc A() int {\n\tx := 10\n\treturn x / 0\n}\n")
	put(t, dir, "b.go", "package p\n\nfunc B() int {\n\treturn 2\n}\n\nfunc C() int {\n\treturn 3\n}\n\nfunc D() int {\n\treturn 4\n}\n")
	git(t, dir, "2024-02-10T10:00:00Z", "add", "-A")
	git(t, dir, "2024-02-10T10:00:00Z", "commit", "-q", "-m", "feat: add D")
	second := strings.TrimSpace(git(t, dir, "", "rev-parse", "HEAD"))

	put(t, dir, "a.go", "package p\n\n// A computes.\nfunc A() int {\n\tx := 10\n\treturn x / 2\n}\n")
	git(t, dir, "2024-03-01T10:00:00Z", "add", "-A")
	git(t, dir, "2024-03-01T10:00:00Z", "commit", "-q", "-m", "fix: division by zero in A")

	repo, err := OpenRepo(dir)
	require.NoError(t, err)
	changes := []ChangeEval{
		{Sha: first, Time: 1706781600, Files: []Sample{{Path: "a.go"}, {Path: "b.go"}}},
		{Sha: second, Time: 1707559200, Files: []Sample{{Path: "a.go"}, {Path: "b.go"}}},
	}
	labels, err := repo.InducedLabels("main", changes, 2)
	require.NoError(t, err)
	assert.Equal(t, map[string]map[string]bool{first: {"a.go": true}}, labels)
}

func TestInducedLabelsFollowAReverts(t *testing.T) {
	dir := initRepo(t)
	put(t, dir, "a.go", "package p\n\nfunc A() int {\n\treturn 1\n}\n")
	git(t, dir, "2024-01-01T10:00:00Z", "add", "-A")
	git(t, dir, "2024-01-01T10:00:00Z", "commit", "-q", "-m", "initial")
	put(t, dir, "a.go", "package p\n\nfunc A() int {\n\treturn 2\n}\n")
	git(t, dir, "2024-02-01T10:00:00Z", "add", "-A")
	git(t, dir, "2024-02-01T10:00:00Z", "commit", "-q", "-m", "feat: two")
	change := strings.TrimSpace(git(t, dir, "", "rev-parse", "HEAD"))
	git(t, dir, "2024-02-05T10:00:00Z", "revert", "--no-edit", "HEAD")

	repo, err := OpenRepo(dir)
	require.NoError(t, err)
	labels, err := repo.InducedLabels("main", []ChangeEval{{Sha: change, Time: 1706781600, Files: []Sample{{Path: "a.go"}}}}, 1)
	require.NoError(t, err)
	assert.True(t, labels[change]["a.go"])
}
