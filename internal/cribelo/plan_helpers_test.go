package cribelo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
		p.Files = append(p.Files, FileReport{Path: string(rune('a' + i)), Minutes: m})
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

func TestSelectReadsBudgetTooSmallStillReadsOne(t *testing.T) {
	p := planWith(9, 4, 4)
	selectReads(p, Options{Budget: 2 * time.Minute})
	assert.Equal(t, []bool{true, false, false}, readFlags(p))
	assert.Equal(t, 1, p.ReadCount)
	assert.Equal(t, 9, p.ReadMinutes)
}

func TestSkipReason(t *testing.T) {
	assert.Equal(t, "no measurable change", skipReason(FileReport{Added: 10, Values: map[Feature]float64{}}))
	assert.Equal(t, "ranked lower", skipReason(FileReport{Added: 11, Values: map[Feature]float64{}}))
	assert.Equal(t, "ranked lower", skipReason(FileReport{Added: 2, Values: map[Feature]float64{FeatureComplexityAdded: 1}}))
	assert.Equal(t, "ranked lower", skipReason(FileReport{Added: 2, Values: map[Feature]float64{}, Clone: &CloneMatch{}}))
}

func TestGate(t *testing.T) {
	high := []FileReport{{Level: LevelLow}, {Level: LevelHigh}}
	medium := []FileReport{{Level: LevelMedium}, {Level: LevelLow}}
	low := []FileReport{{Level: LevelLow}}

	assert.Equal(t, "failed", gate(high, "high"))
	assert.Equal(t, "passed", gate(medium, "high"))
	assert.Equal(t, "passed", gate(low, "high"))

	assert.Equal(t, "failed", gate(high, "medium"))
	assert.Equal(t, "failed", gate(medium, "medium"))
	assert.Equal(t, "failed", gate(medium, "any"))
	assert.Equal(t, "passed", gate(low, "medium"))

	assert.Equal(t, "passed", gate(high, "never"))
	assert.Equal(t, "passed", gate(high, ""))
	assert.Equal(t, "passed", gate(high, "bogus"))
	assert.Equal(t, "passed", gate(nil, "high"))
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
