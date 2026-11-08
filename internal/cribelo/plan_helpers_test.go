package cribelo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRankOrdering(t *testing.T) {
	files := []FileReport{
		{Path: "z.go", Score: 0.5, Added: 10},
		{Path: "b.go", Score: 0.5, Added: 10},
		{Path: "a.go", Score: 0.5, Added: 30},
		{Path: "low.go", Score: 0.1, Added: 999},
		{Path: "top.go", Score: 0.9, Added: 1},
	}
	rank(files)
	var paths []string
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	assert.Equal(t, []string{"top.go", "a.go", "b.go", "z.go", "low.go"}, paths)
}

func planWith(minutes ...int) *Plan {
	p := &Plan{}
	for i, m := range minutes {
		p.Files = append(p.Files, FileReport{Path: string(rune('a' + i)), Minutes: m, Added: 20})
	}
	return p
}

func readFlags(p *Plan) []bool {
	var out []bool
	for _, f := range p.Files {
		out = append(out, f.Read)
	}
	return out
}

func TestSelectReadsTop(t *testing.T) {
	p := planWith(1, 2, 3, 4)
	selectReads(p, Options{Top: 2})
	assert.Equal(t, []bool{true, true, false, false}, readFlags(p))
	assert.Equal(t, 2, p.ReadCount)
	assert.Equal(t, 3, p.ReadMinutes)

	p = planWith(1)
	selectReads(p, Options{Top: 5})
	assert.Equal(t, 1, p.ReadCount)

	p = &Plan{}
	selectReads(p, Options{Top: 3})
	assert.Equal(t, 0, p.ReadCount)
}

func TestSelectReadsEveryHighFile(t *testing.T) {
	// ranked: three high, one medium, one low
	p := planWith(1, 1, 1, 1, 1)
	for i, l := range []Level{LevelHigh, LevelHigh, LevelHigh, LevelMedium, LevelLow} {
		p.Files[i].Level = l
	}
	selectReads(p, Options{Top: 2})
	assert.Equal(t, []bool{true, true, true, false, false}, readFlags(p), "every high file is read, beyond Top")
	assert.Equal(t, 3, p.ReadCount)

	p = planWith(1, 1, 1, 1, 1)
	for i, l := range []Level{LevelHigh, LevelHigh, LevelHigh, LevelMedium, LevelLow} {
		p.Files[i].Level = l
	}
	selectReads(p, Options{Top: 2, Exact: true})
	assert.Equal(t, []bool{true, true, false, false, false}, readFlags(p), "Exact reads Top files only")

	p = planWith(1, 1, 1)
	p.Files[0].Level = LevelHigh
	selectReads(p, Options{Top: 3})
	assert.Equal(t, 3, p.ReadCount, "fewer high files than Top: Top files are read")

	p = planWith(1, 1, 1)
	p.Files[0].Level = LevelHigh
	selectReads(p, Options{Top: 1, Budget: time.Minute})
	assert.Equal(t, 1, p.ReadCount, "budget overrides the high rule")
}

func TestSelectReadsBudget(t *testing.T) {
	p := planWith(5, 5, 5)
	selectReads(p, Options{Top: 1, Budget: 10 * time.Minute})
	assert.Equal(t, []bool{true, true, false}, readFlags(p), "budget overrides Top")
	assert.Equal(t, 10, p.ReadMinutes)

	// a later, smaller file can still fit after a skipped one
	p = planWith(5, 8, 2)
	selectReads(p, Options{Budget: 10 * time.Minute})
	assert.Equal(t, []bool{true, false, true}, readFlags(p))
	assert.Equal(t, 7, p.ReadMinutes)
}

func TestSelectReadsBudgetSkipsUnchangedFiles(t *testing.T) {
	p := planWith(5, 1, 1)
	p.Files[1].Added = 2
	selectReads(p, Options{Budget: 2 * time.Hour})
	assert.Equal(t, []bool{true, false, true}, readFlags(p), "the plan without budget skips it too")
	assert.Equal(t, "no measurable change", skipReason(p.Files[1]))

	p = planWith(1)
	p.Files[0].Added = 2
	selectReads(p, Options{Budget: 2 * time.Hour})
	assert.Equal(t, 1, p.ReadCount, "still one file when it is the only one")
}

func TestSelectReadsBudgetTooSmallStillReadsOne(t *testing.T) {
	p := planWith(9, 4, 4)
	selectReads(p, Options{Budget: 2 * time.Minute})
	assert.Equal(t, []bool{true, false, false}, readFlags(p))
	assert.Equal(t, 1, p.ReadCount)
	assert.Equal(t, 9, p.ReadMinutes)
}

func TestSkipReasonMechanical(t *testing.T) {
	renames := FileReport{Hunks: []HunkReport{{Nature: NatureRename}, {Nature: NatureRename}}, ToRead: 2, Added: 40}
	renames.Natures = countNatures(renames.Hunks)
	assert.Equal(t, "renames only", skipReason(renames))
	assert.True(t, unchanged(renames))
	mixed := FileReport{Hunks: []HunkReport{{Nature: NatureMoved}, {Nature: NatureImports}}, Added: 40}
	mixed.Natures = countNatures(mixed.Hunks)
	assert.Equal(t, "mechanical changes only", skipReason(mixed))
	read := FileReport{Hunks: []HunkReport{{Nature: NatureBehavior, Added: 40}}, ToRead: 40, Added: 40}
	read.Natures = countNatures(read.Hunks)
	read.Values = map[Feature]float64{FeatureComplexityAdded: 3}
	assert.Equal(t, "ranked lower", skipReason(read))
}

func TestSkipReason(t *testing.T) {
	assert.Equal(t, "no measurable change", skipReason(FileReport{Added: 10, Values: map[Feature]float64{}}))
	assert.Equal(t, "ranked lower", skipReason(FileReport{Added: 11, Values: map[Feature]float64{}}))
	assert.Equal(t, "ranked lower", skipReason(FileReport{Added: 2, Values: map[Feature]float64{FeatureComplexityAdded: 1}}))
	assert.Equal(t, "ranked lower", skipReason(FileReport{Added: 2, Values: map[Feature]float64{}, Clone: &CloneMatch{}}))
}

func TestGate(t *testing.T) {
	facts := []Fact{{Kind: FactCycle, Path: "a.go", Text: "introduces a cycle: a.go → b.go → a.go"}, {Kind: FactSignature, Path: "c.go", Text: "F() changed, called by 2 files"}}

	p := &Plan{ChangeMinutes: 71, Facts: facts}
	p.gate(Options{})
	assert.Equal(t, "", p.Gate, "no gate set, no verdict")
	assert.Empty(t, p.GateReasons)

	p = &Plan{ChangeMinutes: 71, Facts: facts}
	p.gate(Options{FailOver: time.Hour})
	assert.Equal(t, "failed", p.Gate)
	assert.Equal(t, []string{"reading time 1 h 11, over the 1 h allowed"}, p.GateReasons)

	p = &Plan{ChangeMinutes: 48, Facts: facts}
	p.gate(Options{FailOver: time.Hour, FailOn: []string{FactCycle}})
	assert.Equal(t, "failed", p.Gate)
	assert.Equal(t, []string{"a.go: introduces a cycle: a.go → b.go → a.go"}, p.GateReasons)

	p = &Plan{ChangeMinutes: 48, Facts: facts}
	p.gate(Options{FailOver: time.Hour, FailOn: []string{FactDependency}})
	assert.Equal(t, "passed", p.Gate)
	assert.Equal(t, "reading time 48 min of 1 h allowed, no dependency", p.gateTerms())

	p = &Plan{}
	p.gate(Options{FailOn: []string{FactCycle, FactSignature}})
	assert.Equal(t, "passed", p.Gate, "nothing changed passes a gate that is set")
	assert.Equal(t, "no cycle, no signature", p.gateTerms())
}

func TestGateFacts(t *testing.T) {
	kinds, err := gateFacts([]string{"cycle", "never", "signature"})
	require.NoError(t, err)
	assert.Equal(t, []string{"cycle", "signature"}, kinds)

	kinds, err = gateFacts([]string{"never"})
	require.NoError(t, err)
	assert.Empty(t, kinds)

	_, err = gateFacts([]string{"high"})
	assert.ErrorContains(t, err, "score level")
	_, err = gateFacts([]string{"bogus"})
	assert.ErrorContains(t, err, `unknown fact "bogus"`)
}

func TestPlanCountHighest(t *testing.T) {
	p := &Plan{}
	assert.Equal(t, LevelLow, p.Highest())
	assert.Equal(t, 0, p.Count(LevelHigh))
	p.Files = []FileReport{{Level: LevelMedium}, {Level: LevelMedium}, {Level: LevelLow}}
	assert.Equal(t, LevelMedium, p.Highest())
	assert.Equal(t, 2, p.Count(LevelMedium))
	assert.Equal(t, 1, p.Count(LevelLow))
	assert.Equal(t, 0, p.Count(LevelHigh))
}

func TestFilterIgnored(t *testing.T) {
	changes := []Change{{Path: "docs/a.go"}, {Path: "main.go"}, {Path: "x.generated.ts"}}
	got := filterIgnored(append([]Change(nil), changes...), Config{Ignore: []string{"docs/**", "*.generated.ts"}})
	assert.Equal(t, []Change{{Path: "main.go"}}, got)
	assert.Equal(t, changes, filterIgnored(changes, Config{}))
}
