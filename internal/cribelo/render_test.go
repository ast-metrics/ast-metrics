package cribelo

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func samplePlan() *Plan {
	return &Plan{
		Methodology: MethodologyVersion, Base: "main", BaseSha: "abcdef0123456", HeadSha: "fedcba",
		Changed: 3, ReadCount: 1, ReadMinutes: 2, ChangeMinutes: 3,
		Gate: "failed", FailOn: []string{FactCycle}, FailOverMinutes: 2, GateReasons: []string{"reading time 3 min, over the 2 min allowed", "a.go: introduces a cycle: a.go → b.go → a.go"},
		Files: []FileReport{
			{Path: "a.go", Status: "M", Added: 10, Deleted: 2, Score: 0.81, Level: LevelHigh, Minutes: 2, Read: true, Reasons: []string{"imported by 4 files", "no test touched"},
				Values:        map[Feature]float64{FeatureMaxComplexity: 5, FeatureUntested: 1, FeatureAfferent: 4, FeatureChurn90d: 7, FeatureFixes90d: 2},
				Contributions: []Contribution{{Feature: FeatureAfferent, Value: 4, Logit: 1.2}, {Feature: FeatureClone, Value: 0.9, Logit: 1.35}},
				Functions:     []TouchedFunction{{Name: "F", Line: 3, Before: 1, After: 5}, {Name: "G", Line: 9, New: true, After: 2}},
				Clone:         &CloneMatch{Path: "b.go", Function: "F ~ H", Line: 7, Similarity: 0.9}},
			{Path: "c.go", Score: 0.1, Level: LevelLow, Minutes: 1, Reasons: []string{"+2 lines"}},
		},
		Skipped: []Skipped{{Path: "c.go", Kind: "code", Reason: "no measurable change", Score: 0.1}, {Path: "README.md", Kind: "not analyzed", Reason: "not analyzed (docs, config, other languages)"}},
	}
}

func TestPlanText(t *testing.T) {
	SetColor(false)
	text := samplePlan().Text()
	assert.Contains(t, text, "3 files changed vs main (merge-base abcdef0)")
	assert.Contains(t, text, "Read 1 of 3 changed files, about 2 min.")
	assert.Contains(t, text, "lines to read closely  lines to glance at\n  1  a.go\n")
	assert.Contains(t, text, "Why these files\n           complexity  used by  commits 90d       clone  test changed\n")
	assert.Contains(t, text, "  1  a.go  ▮▯▯▯▯ 5     ▮▮▯▯▯ 4  ▮▮▮▯▯ 7, 2 fixes  90%    ✗\n")
	assert.Contains(t, text, "Totals  12 lines to read closely, 0 to glance at (20 and 60 lines a minute)\n")
	assert.Contains(t, text, "Skip (2)")
	assert.Contains(t, text, "Gate: failed\n  reading time 3 min, over the 2 min allowed\n  a.go: introduces a cycle")
	assert.NotContains(t, text, "high, 0 medium")
	assert.Contains(t, text, "generic, not yet calibrated")
}

func TestPlanTextEmptyAndNoCode(t *testing.T) {
	SetColor(false)
	assert.Contains(t, (&Plan{BaseSha: "abc"}).Text(), "Nothing changed.")
	assert.Contains(t, (&Plan{Changed: 2, BaseSha: "abc"}).Text(), "Nothing to read")
	assert.NotContains(t, (&Plan{Changed: 2, BaseSha: "abc"}).Text(), "Gate")
	passed := &Plan{Changed: 2, BaseSha: "abc", Gate: "passed", FailOn: []string{FactCycle}, FailOverMinutes: 60, ChangeMinutes: 48}
	assert.Contains(t, passed.Text(), "Gate: passed (reading time 48 min of 1 h allowed, no cycle)")
	assert.Contains(t, passed.Markdown(), "**Gate:** passed · reading time 48 min of 1 h allowed, no cycle")
}

func TestPlanMarkdown(t *testing.T) {
	md := samplePlan().Markdown()
	assert.Contains(t, md, "## Cribelo: read 1 of 3 changed files, about 2 min\n")
	assert.Contains(t, md, "| | File | Lines to read closely | Lines to glance at |\n|--:|:--|:--|:--|\n| 1 | `a.go` |  |  |\n")
	assert.Contains(t, md, "<details><summary><b>Why these files</b></summary>\n\n| | File | Complexity | Used&nbsp;by | Commits&nbsp;90d | Test changed |\n|--:|:--|:--|:--|:--|:-:|\n")
	assert.Contains(t, md, "| 1 | `a.go`<br><sub>90% similar to `b.go:H()`</sub> | 🟩&#8288;⬜&#8288;⬜&#8288;⬜&#8288;⬜&nbsp;5 | 🟨&#8288;🟨&#8288;⬜&#8288;⬜&#8288;⬜&nbsp;4 | 🟧&#8288;🟧&#8288;🟧&#8288;⬜&#8288;⬜&nbsp;7, 2 fixes | ❌ |\n")
	assert.Contains(t, md, "<sub><b>Complexity</b>: highest cyclomatic complexity among the functions touched.")
	assert.Contains(t, md, "<details><summary><b>Totals</b>: 12 lines to read closely, 0 to glance at</summary>\n\n| | File | Lines to read closely | Lines to glance at |\n|--:|:--|--:|--:|\n| 1 | `a.go` | 12 | 0 |\n\n<sub><b>Read closely</b>: code lines in functions that gained a branch, or whose complexity is above 3.")
	assert.Contains(t, md, "20 lines a minute read closely, 60 glanced at")
	assert.NotContains(t, md, "**Total**", "one file needs no total")
	assert.Contains(t, md, "<details><summary><b>Skip</b> (2 files)")
	assert.Contains(t, md, "- `README.md`")
	assert.Contains(t, md, "**Gate:** failed · reading time 3 min, over the 2 min allowed · a.go: introduces a cycle")
	assert.Contains(t, md, "methodology "+MethodologyVersion)

	assert.NotContains(t, md, "—")
	assert.Contains(t, (&Plan{}).Markdown(), "nothing changed")
	assert.Contains(t, (&Plan{Changed: 2}).Markdown(), "nothing to read (2 files changed, no code)")

	p := samplePlan()
	p.Calibrated, p.TrainedOn, p.TrainedAt = true, 120, "2026-01-01"
	assert.Contains(t, p.Markdown(), "calibrated on this repository's last 120 changes (2026-01-01)")
}

func TestPlanJSONRoundTrip(t *testing.T) {
	out, err := samplePlan().JSON()
	require.NoError(t, err)
	var back Plan
	require.NoError(t, json.Unmarshal([]byte(out), &back))
	assert.Equal(t, 3, back.Changed)
	assert.Equal(t, LevelHigh, back.Files[0].Level)
	assert.Equal(t, 0.9, back.Files[0].Clone.Similarity)
}

func TestSkipTextAndSummary(t *testing.T) {
	SetColor(false)
	p := samplePlan()
	text := p.SkipText()
	assert.Contains(t, text, "no measurable change (score 0.10)")
	assert.Contains(t, text, "README.md")
	assert.Equal(t, "Nothing skipped.\n", (&Plan{}).SkipText())

	skipped := []Skipped{{Reason: "b"}, {Reason: "a"}, {Reason: "b"}, {Reason: "c"}}
	assert.Equal(t, "2 b | 1 a | 1 c", skipSummary(skipped, " | "), "count desc, then name")
}

func TestPlanExplainUnit(t *testing.T) {
	SetColor(false)
	p := samplePlan()
	out, err := p.Explain("a.go")
	require.NoError(t, err)
	assert.Contains(t, out, "score 0.81")
	assert.Contains(t, out, "in the reading list")
	assert.Contains(t, out, "complexity 1 → 5")
	assert.Contains(t, out, "new, complexity 2")
	assert.Contains(t, out, "Possible duplicate")
	assert.Contains(t, out, "90% similar to b.go:7")

	out, err = p.Explain("README.md")
	require.NoError(t, err)
	assert.Contains(t, out, "README.md is skipped")

	_, err = p.Explain("nope.go")
	assert.Error(t, err)
}

func TestBars(t *testing.T) {
	assert.Equal(t, 0, cells(0, complexitySteps))
	assert.Equal(t, 1, cells(5, complexitySteps))
	assert.Equal(t, 3, cells(16, complexitySteps))
	assert.Equal(t, 4, cells(25, complexitySteps))
	assert.Equal(t, 5, cells(900, commitSteps))
	assert.Equal(t, "🟧&#8288;🟧&#8288;🟧&#8288;⬜&#8288;⬜&nbsp;16", barCell(16, complexitySteps).markdown, "a bar and its value never break")
	assert.Equal(t, "⬜&#8288;⬜&#8288;⬜&#8288;⬜&#8288;⬜", barCell(0, complexitySteps).markdown)
	assert.Equal(t, "▮▮▮▮▮ 16", barCell(16, complexitySteps).plain)
	assert.Equal(t, "45 min", duration(45))
	assert.Equal(t, "2 h", duration(120))
	assert.Equal(t, "2 h 05", duration(125))
}

// readingPlan is a file, its test, a file that is only glanced at, and a
// file of the same name as the first.
func readingPlan() *Plan {
	return &Plan{
		Changed: 5, ReadCount: 4, ReadMinutes: 9,
		Files: []FileReport{
			{Path: "core/store.go", Status: "A", Read: true, ToRead: 40, ToGlance: 12, Hunks: []HunkReport{{}},
				Ranges:       []LineRange{{10, 14}, {30, 30}, {40, 62}, {70, 71}, {80, 90}, {95, 99}},
				GlanceRanges: []LineRange{{3, 8}},
				Values:       map[Feature]float64{FeatureMaxComplexity: 7, FeatureAfferent: 2}},
			{Path: "core/store_test.go", Read: true, Test: true, ToRead: 10, ToGlance: 30, Hunks: []HunkReport{{}},
				Ranges: []LineRange{{8, 8}}, GlanceRanges: []LineRange{{20, 49}}, Values: map[Feature]float64{}},
			{Path: "api/handler.go", Status: "M", Read: true, ToRead: 0, ToGlance: 6, Hunks: []HunkReport{{}},
				GlanceRanges: []LineRange{{12, 12}},
				Values:       map[Feature]float64{FeatureMaxComplexity: 12, FeatureUntested: 1}},
			{Path: "cmd/store.go", Status: "M", Read: true, ToRead: 3, ToGlance: 0, Hunks: []HunkReport{{}},
				Ranges: []LineRange{{5, 7}}, Values: map[Feature]float64{},
				Clone: &CloneMatch{Path: "pkg/core/store.go", Function: "run ~ Store::Put", Similarity: 0.84}},
			{Path: "left.go"},
		},
		Groups: []ReadingGroup{
			{Files: []string{"core/store.go", "core/store_test.go", "api/handler.go"}, Linked: true},
			{Files: []string{"cmd/store.go"}},
		},
	}
}

func TestReadingText(t *testing.T) {
	SetColor(false)
	text := readingPlan().Text()
	assert.Contains(t, text, ""+
		"                               lines to read closely             lines to glance at\n"+
		"  1  core/store.go       new   10-14, 30, 40-62, 70-71, +2 more  3-8\n"+
		"  2  core/store_test.go  test  line 8                            20-49\n"+
		"  3  api/handler.go                                              line 12\n"+
		"  4  cmd/store.go              5-7\n", "a lone line is called one, so that it is not taken for a count")
	assert.Contains(t, text, "Why these files\n"+
		"                    complexity  used by  clone  test changed\n"+
		"  1  core/store.go  ▮▮▯▯▯ 7     ▮▯▯▯▯ 2         ✓\n"+
		"  3  handler.go     ▮▮▮▯▯ 12    ▯▯▯▯▯           ✗\n"+
		"  4  cmd/store.go   ▯▯▯▯▯       ▯▯▯▯▯    84%    ✓\n", "tests carry no signal, two files of the same name keep a directory")
	assert.Contains(t, text, "Totals  53 lines to read closely, 48 to glance at (20 and 60 lines a minute)\n")
	assert.NotContains(t, text, "↳")
}

func TestReadingMarkdown(t *testing.T) {
	p := readingPlan()
	md := p.Markdown()
	assert.Contains(t, md, "## Cribelo: read 4 of 5 changed files, about 9 min\n\n"+
		"| | File | Lines to read closely | Lines to glance at |\n|--:|:--|:--|:--|\n"+
		"| 1 | `core/store.go` *new* | 10-14, 30, 40-62, 70-71, +2 more | 3-8 |\n"+
		"| 2 | `core/store_test.go` *test* | line 8 | 20-49 |\n"+
		"| 3 | `api/handler.go` |  | line 12 |\n"+
		"| 4 | `cmd/store.go` | 5-7 |  |\n\n"+
		"<details><summary><b>Why these files</b></summary>\n\n"+
		"| | File | Complexity | Used&nbsp;by | Test changed |\n")
	assert.Contains(t, md, "| 4 | `cmd/store.go`<br><sub>84% similar to `pkg/core/store.go:Put()`</sub> | ⬜&#8288;⬜&#8288;⬜&#8288;⬜&#8288;⬜ | ⬜&#8288;⬜&#8288;⬜&#8288;⬜&#8288;⬜ | ✅ |\n")
	assert.NotContains(t, md, "| 2 | `store_test.go` |", "tests carry no signal")
	assert.Contains(t, md, "</details>\n\n<details><summary><b>Totals</b>: 53 lines to read closely, 48 to glance at</summary>")
	assert.Contains(t, md, "| 2 | `core/store_test.go` | 10 | 30 |\n")
	assert.Contains(t, md, "| | **Total** | **53** | **48** |\n")

	links, err := PullRequestLinks("https://github.com/o/r/pull/12/files", "abc")
	require.NoError(t, err)
	p.Links = links
	md = p.Markdown()
	assert.Regexp(t, "\\| 2 \\| `core/store_test.go` \\*test\\* \\| line \\[8\\]\\(https://github.com/o/r/pull/12/files#diff-[0-9a-f]{64}R8\\) \\| \\[20-49\\]\\(https://github.com/o/r/pull/12/files#diff-[0-9a-f]{64}R20-R49\\) \\|\n", md)
	SetColor(false)
	assert.NotContains(t, p.Text(), "https://", "a terminal gets no link")
}

func TestPullRequestLinks(t *testing.T) {
	github, err := PullRequestLinks("https://github.com/ast-metrics/ast-metrics/pull/209", "abc")
	require.NoError(t, err)
	// The anchor of a file in a GitHub diff is the SHA-256 of its path, as read on pull request 209.
	const file = "https://github.com/ast-metrics/ast-metrics/pull/209/files#diff-29027a7f75bca43b050fc18363db0152271a66e310ca57558ce6da67a71954e4"
	assert.Equal(t, file, github("internal/cribelo/table.go", 0, 0))
	assert.Equal(t, file+"R12", github("internal/cribelo/table.go", 12, 12))
	assert.Equal(t, file+"R12-R40", github("internal/cribelo/table.go", 12, 40))

	gitlab, err := PullRequestLinks("https://gitlab.example.com/group/sub/project/-/merge_requests/7", "abc123")
	require.NoError(t, err)
	assert.Equal(t, "https://gitlab.example.com/group/sub/project/-/blob/abc123/src/a%20b.go", gitlab("src/a b.go", 0, 0))
	assert.Equal(t, "https://gitlab.example.com/group/sub/project/-/blob/abc123/src/a.go#L12-40", gitlab("src/a.go", 12, 40))
	assert.Equal(t, "https://gitlab.example.com/group/sub/project/-/blob/abc123/src/a.go#L12", gitlab("src/a.go", 12, 12))

	_, err = PullRequestLinks("https://example.com/somewhere", "abc")
	assert.Error(t, err)
}

func TestShortNames(t *testing.T) {
	names := shortNames([]string{"a/deps/resolver.go", "b/deps/resolver.go", "a/util.go", "resolver.go"})
	assert.Equal(t, map[string]string{"a/deps/resolver.go": "a/deps/resolver.go", "b/deps/resolver.go": "b/deps/resolver.go", "a/util.go": "util.go", "resolver.go": "resolver.go"}, names)
	assert.Equal(t, "c/d.go", tail("a/b/c/d.go", 2))
	assert.Equal(t, "d.go", tail("d.go", 3))
}

func TestLineRanges(t *testing.T) {
	assert.Equal(t, []LineRange{{3, 9}, {14, 14}, {30, 31}}, lineRanges([]int{30, 3, 4, 5, 9, 14, 31}, nil), "lines three apart at most are one span")
	comment := map[int]bool{10: true, 11: true, 12: true, 13: true, 14: true}
	assert.Equal(t, []LineRange{{7, 16}}, lineRanges([]int{7, 8, 9, 15, 16}, comment), "filler does not cut a span, whatever its length")
	assert.Equal(t, []LineRange{{7, 9}, {15, 16}}, lineRanges([]int{7, 8, 9, 15, 16}, nil), "unchanged lines do, past three")
	assert.Equal(t, []LineRange{{7, 9}, {15, 16}}, lineRanges([]int{7, 8, 9, 15, 16}, comment, LineRange{12, 13}), "a span of the other kind always does")
	assert.Empty(t, lineRanges(nil, nil))

	read, glanced := spansOf([]int{10, 12, 20}, []int{5, 11, 14, 22}, nil)
	assert.Equal(t, []LineRange{{10, 12}, {20, 20}}, read)
	assert.Equal(t, []LineRange{{5, 5}, {14, 14}, {22, 22}}, glanced, "line 11 is read with the span around it; 14 and 22 stand on each side of line 20")

	assert.True(t, bareLine.MatchString("\t})"))
	assert.True(t, bareLine.MatchString("package analyzer"))
	assert.False(t, bareLine.MatchString("\treturn x }"))
}

func TestFactMarkdown(t *testing.T) {
	fact := newFact(FactDependency, "a.go", nil, "`core` depends on `api` for the first time")
	assert.Equal(t, "core depends on api for the first time", fact.Text)
	assert.Equal(t, "`core` depends on `api` for the first time", fact.Markdown())
	assert.Equal(t, "plain", Fact{Text: "plain"}.Markdown())

	p := samplePlan()
	p.Facts = []Fact{fact}
	assert.Contains(t, p.Markdown(), "- `a.go`: `core` depends on `api` for the first time\n")
	SetColor(false)
	assert.Contains(t, p.Text(), "a.go  core depends on api for the first time\n")
}

func TestNewFileIsTagged(t *testing.T) {
	SetColor(false)
	p := &Plan{Changed: 1, ReadCount: 1, Files: []FileReport{{Path: "n.go", Status: "A", Added: 3, Read: true, Reasons: []string{"new file"}}}}
	assert.Contains(t, p.Text(), "n.go  new")
	assert.Contains(t, p.Markdown(), "| 1 | `n.go` *new* |  |  |\n")
}
