package cribelo

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const goMod = "module example.com/app\n\ngo 1.22\n"

func factTexts(facts []Fact, kind string) []string {
	var out []string
	for _, f := range facts {
		if f.Kind == kind {
			out = append(out, f.Text)
		}
	}
	return out
}

func TestGraphFactsCycleAndFirstDependency(t *testing.T) {
	base := parsedTree(t, map[string]string{
		"go.mod":    goMod,
		"a/a.go":    "package a\n\nimport \"example.com/app/b\"\n\nfunc A() int { return b.B() }\n",
		"b/b.go":    "package b\n\nfunc B() int { return 1 }\n",
		"c/c.go":    "package c\n\nfunc C() int { return 2 }\n",
		"util/u.go": "package util\n\nfunc U() int { return 3 }\n",
	})
	head := parsedTree(t, map[string]string{
		"go.mod":    goMod,
		"a/a.go":    "package a\n\nimport \"example.com/app/b\"\n\nfunc A() int { return b.B() }\n",
		"b/b.go":    "package b\n\nimport \"example.com/app/a\"\n\nfunc B() int { return a.A() }\n",
		"c/c.go":    "package c\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/app/util\"\n\t\"github.com/acme/tool\"\n)\n\nfunc C() int { fmt.Println(tool.X); return util.U() }\n",
		"util/u.go": "package util\n\nfunc U() int { return 3 }\n",
	})
	changes := []Change{{Path: "b/b.go", Status: 'M'}, {Path: "c/c.go", Status: 'M'}}
	facts := graphFacts(buildGraphs(head.Root, analyzer.FileDependencies(head.All), head, base, changes, nil), changes)
	assert.Equal(t, []string{"introduces a cycle: b.go → a.go → b.go"}, factTexts(facts, FactCycle))
	assert.Equal(t, []string{"c depends on util for the first time (imports u.go)"}, factTexts(facts, FactDependency))
	assert.Equal(t, []string{"first use of github.com/acme/tool in the repository"}, factTexts(facts, FactLibrary))

	// The same change against a base where the edges already exist says nothing.
	facts = graphFacts(buildGraphs(head.Root, analyzer.FileDependencies(head.All), head, head, changes, nil), changes)
	assert.Empty(t, facts)
}

func TestGraphFactsDeletedFileBreaksNothing(t *testing.T) {
	base := parsedTree(t, map[string]string{
		"go.mod":   goMod,
		"a/a.go":   "package a\n\nimport \"example.com/app/b\"\n\nfunc A() int { return b.B() }\n",
		"b/b.go":   "package b\n\nfunc B() int { return 1 }\n",
		"b/old.go": "package b\n\nfunc Old() int { return 1 }\n",
	})
	head := parsedTree(t, map[string]string{
		"go.mod": goMod,
		"a/a.go": "package a\n\nimport \"example.com/app/b\"\n\nfunc A() int { return b.B() }\n",
		"b/b.go": "package b\n\nfunc B() int { return 2 }\n",
	})
	changes := []Change{{Path: "b/b.go", Status: 'M'}}
	facts := graphFacts(buildGraphs(head.Root, analyzer.FileDependencies(head.All), head, base, changes, []Change{{Path: "b/old.go", Status: 'D'}}), changes)
	assert.Empty(t, facts)
}

func TestCloneFacts(t *testing.T) {
	files := []FileReport{
		{Path: "a/a.go", Clone: &CloneMatch{Path: "b/b.go", Function: "Parse ~ Scan", Similarity: 0.91}},
		{Path: "c/c.go", Clone: &CloneMatch{Path: "a/a.go", Function: "X ~ Y", Similarity: 0.95}},
		{Path: "d/d.go"},
	}
	facts := cloneFacts(files, map[string]bool{"a/a.go": true, "c/c.go": true, "d/d.go": true})
	require.Len(t, facts, 1)
	assert.Equal(t, "Parse() is 91% similar to b/b.go:Scan(), which this change does not touch", facts[0].Text)
	assert.Equal(t, []string{"b/b.go"}, facts[0].Files)
}

func TestSignatureFacts(t *testing.T) {
	base := parsedTree(t, map[string]string{
		"go.mod":  goMod,
		"a/a.go":  "package a\n\nfunc Sum(a, b int) int { return a + b }\n\nfunc helper(x int) int { return x }\n",
		"b/b.go":  "package b\n\nimport \"example.com/app/a\"\n\nfunc B() int { return a.Sum(1, 2) }\n",
		"b/b2.go": "package b\n\nimport \"example.com/app/a\"\n\nfunc B2() int { return a.Sum(3, 4) }\n",
	})
	headA := "package a\n\nfunc Sum(a, b, c int) int { return a + b + c }\n\nfunc helper(x, y int) int { return x }\n\nfunc Fresh() int { return 1 }\n\nfunc Used() int { return 2 }\n\nfunc main() {}\n"
	head := parsedTree(t, map[string]string{
		"go.mod":  goMod,
		"a/a.go":  headA,
		"b/b.go":  "package b\n\nimport \"example.com/app/a\"\n\nfunc B() int { return a.Sum(1, 2, 0) + a.Used() }\n",
		"b/b2.go": "package b\n\nimport \"example.com/app/a\"\n\nfunc B2() int { return a.Sum(3, 4, 0) }\n",
	})
	lines := len(strings.Split(headA, "\n"))
	changes := []Change{{Path: "a/a.go", Status: 'M', Hunks: []Hunk{{Start: 1, End: lines}}}, {Path: "b/b.go", Status: 'M'}, {Path: "b/b2.go", Status: 'M'}}
	facts := signatureFacts(head, base, changes, func() *nameIndex { return buildNameIndex(head) })
	assert.Equal(t, []string{"Sum() changed its parameters (a, b → a, b, c), called by name from 2 files"}, factTexts(facts, FactSignature))
	assert.Equal(t, []string{"1 new exported function called nowhere yet: Fresh()"}, factTexts(facts, FactAPI))
}

func TestIsExported(t *testing.T) {
	assert.True(t, isExported("Golang", "Parse", "func Parse() {}"))
	assert.False(t, isExported("Golang", "parse", "func parse() {}"))
	assert.False(t, isExported("Python", "Foo::_hidden", "    def _hidden(self):"))
	assert.True(t, isExported("Python", "Foo::run", "    def run(self):"))
	assert.True(t, isExported("PHP", "Foo::bar", "    public function bar() {"))
	assert.False(t, isExported("PHP", "Foo::bar", "    private function bar() {"))
	assert.True(t, isExported("PHP", "Foo::bar", "    function bar() {"))
	assert.True(t, isExported("PHP", "helper", "function helper() {"))
	assert.True(t, isExported("TypeScript", "helper", "export function helper() {"))
	assert.False(t, isExported("TypeScript", "helper", "function helper() {"))
	assert.True(t, isExported("Rust", "run", "pub(crate) fn run() {"))
	assert.True(t, isExported("Java", "Foo::run", "    public static void run() {"))
	assert.False(t, isExported("Java", "Foo::run", "    void run() {"))
}

func TestSortFacts(t *testing.T) {
	facts := []Fact{{Kind: FactClone, Path: "z"}, {Kind: FactCycle, Path: "b"}, {Kind: FactSignature, Path: "a"}, {Kind: FactCycle, Path: "a"}}
	sortFacts(facts)
	assert.Equal(t, []Fact{{Kind: FactCycle, Path: "a"}, {Kind: FactCycle, Path: "b"}, {Kind: FactSignature, Path: "a"}, {Kind: FactClone, Path: "z"}}, facts)
}

func TestHistoryFactsCountFixesOfAFunction(t *testing.T) {
	dir := initRepo(t)
	put(t, dir, "go.mod", goMod)
	body := func(n int) string {
		return fmt.Sprintf("package p\n\nfunc Parse(s string) int {\n\tif s == \"\" {\n\t\treturn %d\n\t}\n\treturn len(s)\n}\n\nfunc Other() int { return 1 }\n", n)
	}
	put(t, dir, "p/p.go", body(0))
	git(t, dir, "2026-01-01T10:00:00Z", "add", "-A")
	git(t, dir, "2026-01-01T10:00:00Z", "commit", "-q", "-m", "initial")
	put(t, dir, "p/p.go", body(1))
	git(t, dir, "2026-02-01T10:00:00Z", "commit", "-q", "-am", "fix: empty input")
	put(t, dir, "p/p.go", body(2))
	git(t, dir, "2026-03-01T10:00:00Z", "commit", "-q", "-am", "Revert \"empty input\"")
	put(t, dir, "p/p.go", body(3))
	git(t, dir, "2026-04-01T10:00:00Z", "commit", "-q", "-am", "feat: something else")
	// A fix elsewhere in the file does not count for Parse.
	put(t, dir, "p/p.go", body(3)+"\nfunc Later() int { return 2 }\n")
	git(t, dir, "2026-05-01T10:00:00Z", "commit", "-q", "-am", "fix: add Later")

	repo, err := OpenRepo(dir)
	require.NoError(t, err)
	baseSha, err := repo.ResolveRef("HEAD")
	require.NoError(t, err)
	base, err := parseFiles(dir, []string{"p/p.go"})
	require.NoError(t, err)
	files := []FileReport{{Path: "p/p.go", Read: true, Functions: []TouchedFunction{{Name: "Parse", After: 3}, {Name: "Other", After: 1}}}}
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC).Unix()
	facts := historyFacts(repo, baseSha, base, files, map[string]Change{}, now)
	require.Len(t, facts, 1)
	assert.Equal(t, FactHistory, facts[0].Kind)
	assert.Equal(t, "Parse() was fixed 2 times in the past year, once by a revert", facts[0].Text)

	// Outside the window, nothing is said.
	later := time.Date(2028, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	assert.Empty(t, historyFacts(repo, baseSha, base, files, map[string]Change{}, later))
}
