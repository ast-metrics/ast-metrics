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
		Changed: 3, ReadCount: 1, ReadMinutes: 2, Gate: "failed", FailOn: "high",
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
	assert.Contains(t, text, "Start with a.go: imported by 4 files, no test touched.")
	assert.Contains(t, text, "complexity  lines     used by  commits 90d       clone  tests\n")
	assert.Contains(t, text, "1  a.go  ▮▯▯▯▯ 5     ▮▯▯▯▯ 12  ▮▮▯▯▯ 4  ▮▮▮▯▯ 7, 2 fixes  90%    ✗\n")
	assert.Contains(t, text, "Skip (2)")
	assert.Contains(t, text, "Gate: failed")
	assert.Contains(t, text, "1 high, 0 medium, fail on high")
	assert.Contains(t, text, "generic, not yet calibrated")
}

func TestPlanTextEmptyAndNoCode(t *testing.T) {
	SetColor(false)
	assert.Contains(t, (&Plan{BaseSha: "abc"}).Text(), "Nothing changed.")
	assert.Contains(t, (&Plan{Changed: 2, BaseSha: "abc", Gate: "passed"}).Text(), "Nothing to read")
}

func TestPlanMarkdown(t *testing.T) {
	md := samplePlan().Markdown()
	assert.Contains(t, md, "## Cribelo: read 1 of 3 changed files, about 2 min\n")
	assert.Contains(t, md, "Start with `a.go`: imported by 4 files, no test touched.")
	assert.Contains(t, md, "| | File | Complexity | Lines | Used by | Commits 90d | Clone | Tests |\n|--:|:--|:--|:--|:--|:--|:--|:-:|\n")
	assert.Contains(t, md, "| 1 | `a.go` | 🟩⬜⬜⬜⬜ 5 | 🟩⬜⬜⬜⬜ 12 | 🟨🟨⬜⬜⬜ 4 | 🟧🟧🟧⬜⬜ 7, 2 fixes | 90% of `b.go:H()` | ❌ |\n")
	assert.Contains(t, md, "<details><summary><b>Skip</b> (2 files)")
	assert.Contains(t, md, "- `README.md`")
	assert.Contains(t, md, "**Gate:** failed · 1 high, 0 medium")
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
	assert.Equal(t, "b 2 | a 1 | c 1", skipSummary(skipped, " | "), "count desc, then name")
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
	assert.Equal(t, 5, cells(900, lineSteps))
	assert.Equal(t, "🟧🟧🟧⬜⬜ 16", barCell(16, complexitySteps).markdown)
	assert.Equal(t, "⬜⬜⬜⬜⬜", barCell(0, complexitySteps).markdown)
	assert.Equal(t, "45 min", duration(45))
	assert.Equal(t, "2 h", duration(120))
	assert.Equal(t, "2 h 05", duration(125))
}

func TestNewFileIsTagged(t *testing.T) {
	SetColor(false)
	p := &Plan{Changed: 1, ReadCount: 1, Files: []FileReport{{Path: "n.go", Status: "A", Added: 3, Read: true, Reasons: []string{"new file"}}}}
	assert.Contains(t, p.Text(), "n.go  new")
	assert.Contains(t, p.Markdown(), "`n.go` 🆕")
	assert.NotContains(t, p.Markdown(), "Used by")
	assert.NotContains(t, p.Markdown(), "Clone")
}
