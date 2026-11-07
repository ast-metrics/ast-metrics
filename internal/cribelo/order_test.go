package cribelo

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func readReport(path string, score float64, toRead int) FileReport {
	return FileReport{Path: path, Score: score, ToRead: toRead, Added: toRead, Minutes: 1, Read: true, Hunks: []HunkReport{{Nature: NatureBehavior}}}
}

func paths(files []FileReport) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		out = append(out, f.Path)
	}
	return out
}

func TestOrderReadsCalleeFirstThenTests(t *testing.T) {
	plan := &Plan{Files: []FileReport{
		readReport("app/handler.go", 0.9, 80),
		readReport("app/service.go", 0.6, 40),
		readReport("app/store.go", 0.5, 30),
		readReport("other/x.go", 0.7, 10),
		{Path: "app/skipped.go", Score: 0.1},
	}, ReadCount: 4, ReadMinutes: 4}
	efferent := map[string][]string{
		"app/handler.go": {"app/service.go", "lib/log.go"},
		"app/service.go": {"app/store.go"},
	}
	tests := []FileReport{
		{Path: "app/service_test.go", Minutes: 2, ToRead: 12},
		{Path: "elsewhere/unrelated_test.go", Minutes: 1},
	}
	orderReads(plan, efferent, nil, tests)

	require.Len(t, plan.Groups, 2)
	assert.Equal(t, ReadingGroup{Files: []string{"app/store.go", "app/service.go", "app/service_test.go", "app/handler.go"}, Linked: true}, plan.Groups[0])
	assert.Equal(t, ReadingGroup{Files: []string{"other/x.go"}}, plan.Groups[1])
	assert.Equal(t, []string{"app/store.go", "app/service.go", "app/service_test.go", "app/handler.go", "other/x.go", "app/skipped.go"}, paths(plan.Files))
	assert.True(t, plan.Files[2].Test)
	assert.True(t, plan.Files[2].Read)
	assert.Equal(t, 5, plan.ReadCount)
	assert.Equal(t, 6, plan.ReadMinutes)
}

func TestOrderReadsBreaksCyclesAndPullsFactsForward(t *testing.T) {
	plan := &Plan{Files: []FileReport{
		readReport("big/a.go", 0.9, 100),
		readReport("cyc/p.go", 0.4, 20),
		readReport("cyc/q.go", 0.5, 20),
	}, ReadCount: 3, ReadMinutes: 3, Facts: []Fact{{Kind: FactCycle, Path: "cyc/p.go"}, {Kind: FactAPI, Path: "big/a.go"}}}
	efferent := map[string][]string{"cyc/p.go": {"cyc/q.go"}, "cyc/q.go": {"cyc/p.go"}}
	orderReads(plan, efferent, nil, nil)
	require.Len(t, plan.Groups, 2)
	// The cycle group carries a structural fact: first. Inside, the cycle is
	// broken on the best score.
	assert.Equal(t, []string{"cyc/q.go", "cyc/p.go"}, plan.Groups[0].Files)
	assert.Equal(t, 1, plan.Groups[0].Facts)
	assert.Equal(t, []string{"big/a.go"}, plan.Groups[1].Files)
	assert.Equal(t, 0, plan.Groups[1].Facts, "an unused export does not reorder")
}

func TestOrderReadsNothingRead(t *testing.T) {
	plan := &Plan{Files: []FileReport{{Path: "a.go"}}}
	orderReads(plan, nil, nil, []FileReport{{Path: "a_test.go"}})
	assert.Nil(t, plan.Groups)
	assert.Equal(t, []string{"a.go"}, paths(plan.Files))
}

func TestSiblingUsesLinkFilesOfOnePackage(t *testing.T) {
	head := parsedTree(t, map[string]string{
		"go.mod":     goMod,
		"p/store.go": "package p\n\nfunc LoadAll() int { return 1 }\n",
		"p/serve.go": "package p\n\nfunc Serve() int { return LoadAll() + helper() }\n\nfunc helper() int { return 0 }\n",
		"p/other.go": "package p\n\nfunc Other() int { return 2 }\n",
		"q/far.go":   "package q\n\nfunc Far() int { return 3 }\n",
	})
	uses := siblingUses(head, []string{"p/store.go", "p/serve.go", "p/other.go", "q/far.go"})
	assert.Equal(t, map[string][]string{"p/serve.go": {"p/store.go"}}, uses)
}

func TestPairTestsLeavesDirectoryMatchToUnreadFile(t *testing.T) {
	better := func(a, b string) bool { return a < b }
	tests := []FileReport{{Path: "pkg/diff_test.go"}, {Path: "pkg/misc_test.go"}}
	paired := pairTests([]string{"pkg/facts.go"}, []string{"pkg/facts.go", "pkg/diff.go"}, tests, better)
	assert.Equal(t, []string{"pkg/misc_test.go"}, paths(paired["pkg/facts.go"]), "diff_test.go belongs to the unread diff.go")
}

func TestPairTestsStayInTheirDirectoryWhenItHoldsChangedCode(t *testing.T) {
	better := func(a, b string) bool { return a < b }
	tests := []FileReport{{Path: "engine/csharp/deps/resolver_test.go"}, {Path: "tests/ScannerTest.php"}}
	all := []string{"engine/php/deps/resolver.go", "engine/csharp/deps/resolver.go", "src/Scanner.php"}
	paired := pairTests([]string{"engine/php/deps/resolver.go", "src/Scanner.php"}, all, tests, better)
	// The C# test is not paired with the PHP resolver, its own one is not read.
	assert.Empty(t, paired["engine/php/deps/resolver.go"])
	// A test in a directory of its own is still matched by name across the tree.
	assert.Len(t, paired["src/Scanner.php"], 1)
}

func TestTestStrength(t *testing.T) {
	assert.Equal(t, 3, testStrength("pkg/invoice.go", "pkg/invoice_test.go"))
	assert.Equal(t, 3, testStrength("src/token.py", "tests/test_token.py"))
	assert.Equal(t, 3, testStrength("src/Session.php", "tests/SessionTest.php"))
	assert.Equal(t, 3, testStrength("ui/button.tsx", "ui/button.test.tsx"))
	assert.Equal(t, 3, testStrength("ui/button.ts", "ui/button.spec.ts"))
	assert.Equal(t, 2, testStrength("pkg/invoice.go", "pkg/invoice_render_test.go"))
	assert.Equal(t, 1, testStrength("pkg/invoice.go", "pkg/other_test.go"))
	assert.Equal(t, 0, testStrength("pkg/invoice.go", "elsewhere/other_test.go"))
	assert.Equal(t, 0, testStrength("pkg/io.go", "elsewhere/radio_test.go"), "two-letter stems match nothing")
}

func TestSelectReadsLeavesMechanicalOut(t *testing.T) {
	plan := &Plan{Files: []FileReport{
		{Path: "moved.go", Score: 0.9, Minutes: 3, Hunks: []HunkReport{{Nature: NatureMoved}}, Natures: map[Nature]natureCount{NatureMoved: {Hunks: 1, Lines: 60}}},
		{Path: "real.go", Score: 0.5, Minutes: 2, Hunks: []HunkReport{{Nature: NatureBehavior}}, Natures: map[Nature]natureCount{NatureBehavior: {Hunks: 1, Lines: 10}}},
	}}
	selectReads(plan, Options{Top: 1})
	assert.False(t, plan.Files[0].Read)
	assert.True(t, plan.Files[1].Read)
	assert.Equal(t, 1, plan.ReadCount)
}

func TestTextShowsGroupsAndTests(t *testing.T) {
	plan := &Plan{Changed: 4, Files: []FileReport{
		readReport("app/store.go", 0.5, 30),
		readReport("app/handler.go", 0.9, 80),
		readReport("other/x.go", 0.7, 10),
	}, ReadCount: 3, ReadMinutes: 3, LinesChanged: 120, LinesToRead: 120}
	orderReads(plan, map[string][]string{"app/handler.go": {"app/store.go"}}, nil, []FileReport{{Path: "app/store_test.go", Minutes: 1, ToRead: 5, Added: 5}})
	text := plan.Text()
	assert.Contains(t, text, "↳ store.go, then what uses it")
	assert.Contains(t, text, "app/store_test.go")
	assert.Regexp(t, `store_test\.go\s+test`, text)
	assert.Less(t, strings.Index(text, "app/store.go"), strings.Index(text, "app/handler.go"))
	md := plan.Markdown()
	assert.Contains(t, md, "| | *store.go, then what uses it* |")
	assert.Contains(t, md, "`app/store_test.go` _test_")
}

func TestTextAllMechanical(t *testing.T) {
	plan := &Plan{Changed: 1, Files: []FileReport{{Path: "moved.go", Added: 40, Hunks: []HunkReport{{Nature: NatureMoved}}, Natures: map[Nature]natureCount{NatureMoved: {Hunks: 1, Lines: 40}}}}, LinesChanged: 40}
	assert.Contains(t, plan.Text(), "every change is mechanical")
	assert.Contains(t, plan.Text(), "0 of 40 changed lines to read")
	assert.Contains(t, plan.Markdown(), "nothing to read (1 file changed, every change is mechanical)")
}

func TestDescribe(t *testing.T) {
	plan := &Plan{Changed: 5, LinesChanged: 200, LinesToRead: 150, Natures: map[Nature]natureCount{NatureRename: {Hunks: 4, Lines: 50}},
		Files:     []FileReport{readReport("app/store.go", 0.5, 30), readReport("app/handler.go", 0.9, 80)},
		ReadCount: 2, ReadMinutes: 8,
		Facts:   []Fact{{Kind: FactSignature, Path: "app/store.go", Text: "Load() changed its parameters (id → id, ctx), called by name from 3 files"}, {Kind: FactCycle, Path: "app/handler.go", Text: "introduces a cycle: handler.go → store.go → handler.go"}},
		Renames: []Rename{{Pair: "old → new", Hunks: 4}},
		Skipped: []Skipped{{Path: "README.md", Kind: "not analyzed", Reason: "not analyzed (docs, config, other languages)"}}}
	orderReads(plan, map[string][]string{"app/handler.go": {"app/store.go"}}, nil, nil)
	out := plan.Describe()
	assert.Contains(t, out, "5 files changed, 200 lines, 150 to read once the mechanical hunks are set aside.")
	assert.Contains(t, out, "**Signatures and API**")
	assert.Contains(t, out, "- `app/store.go`: Load() changed its parameters")
	assert.Contains(t, out, "**Structure**")
	assert.Contains(t, out, "- `old → new` in 4 hunks")
	assert.Contains(t, out, "1. `app/store.go`: the others use it\n2. `app/handler.go`\n")
	assert.Contains(t, out, "Left out: not analyzed")
	assert.Equal(t, "Nothing changed.\n", (&Plan{}).Describe())
}
