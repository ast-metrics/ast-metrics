package cribelo

import (
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
	orderReads(plan, efferent, nil, tests, 0)

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
	orderReads(plan, efferent, nil, nil, 0)
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
	orderReads(plan, nil, nil, []FileReport{{Path: "a_test.go"}}, 0)
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

func TestOrderReadsKeepsPairedTestsWithinTheBudget(t *testing.T) {
	plan := &Plan{Changed: 2, ReadCount: 1, ReadMinutes: 4, Files: []FileReport{{Path: "pkg/a.go", Read: true, Minutes: 4, Score: .9}}}
	tests := []FileReport{{Path: "pkg/a_test.go", Minutes: 3}}
	orderReads(plan, nil, nil, tests, 5)
	assert.Equal(t, 1, plan.ReadCount, "the test does not fit in the 5 minutes left")
	assert.Equal(t, []string{"pkg/a.go"}, plan.Groups[0].Files)

	plan = &Plan{Changed: 2, ReadCount: 1, ReadMinutes: 4, Files: []FileReport{{Path: "pkg/a.go", Read: true, Minutes: 4, Score: .9}}}
	orderReads(plan, nil, nil, tests, 8)
	assert.Equal(t, 2, plan.ReadCount)
	assert.Equal(t, 7, plan.ReadMinutes)
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
		{Path: "moved.go", Score: 0.9, Minutes: 3, Hunks: []HunkReport{{Nature: NatureMoved}}, Natures: map[Nature]NatureCount{NatureMoved: {Hunks: 1, Lines: 60}}},
		{Path: "real.go", Score: 0.5, Minutes: 2, Hunks: []HunkReport{{Nature: NatureBehavior}}, Natures: map[Nature]NatureCount{NatureBehavior: {Hunks: 1, Lines: 10}}},
	}}
	selectReads(plan, Options{Top: 1})
	assert.False(t, plan.Files[0].Read)
	assert.True(t, plan.Files[1].Read)
	assert.Equal(t, 1, plan.ReadCount)
}

func TestOrderPlacesTestsAfterTheirFile(t *testing.T) {
	plan := &Plan{Changed: 4, Files: []FileReport{
		readReport("app/store.go", 0.5, 30),
		readReport("app/handler.go", 0.9, 80),
		readReport("other/x.go", 0.7, 10),
	}, ReadCount: 3, ReadMinutes: 3, LinesChanged: 120, LinesToRead: 120}
	orderReads(plan, map[string][]string{"app/handler.go": {"app/store.go"}}, nil, []FileReport{{Path: "app/store_test.go", Minutes: 1, ToRead: 5, Added: 5}}, 0)
	assert.Equal(t, []string{"app/store.go", "app/store_test.go", "app/handler.go", "other/x.go"}, paths(plan.Files))
	test := plan.File("app/store_test.go")
	require.NotNil(t, test)
	assert.True(t, test.Test)
	assert.True(t, test.Read)
	require.Len(t, plan.Groups, 2)
	assert.Equal(t, []string{"app/store.go", "app/store_test.go", "app/handler.go"}, plan.Groups[0].Files)
	assert.True(t, plan.Groups[0].Linked)
}
