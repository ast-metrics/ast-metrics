package cribelo

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func handBuiltBacktest() *Backtest {
	lines := func(n float64) map[Feature]float64 { return map[Feature]float64{FeatureLinesAdded: n} }
	return &Backtest{
		Top: 1,
		Changes: []ChangeEval{
			{Files: []Sample{
				{Path: "a", Added: 10, Values: map[Feature]float64{FeatureLinesAdded: 10, FeatureFixes90d: 3}, Fixed: true},
				{Path: "b", Added: 50, Values: lines(50)},
				{Path: "c", Added: 5, Values: lines(5)},
			}},
			{Files: []Sample{
				{Path: "d", Added: 4, Deleted: 6, Values: map[Feature]float64{FeatureLinesAdded: 4, FeatureLinesDeleted: 6}},
				{Path: "e", Added: 20, Values: lines(20), Fixed: true},
			}},
		},
	}
}

func TestEvaluateByHand(t *testing.T) {
	m := handBuiltBacktest().Evaluate(DefaultWeights(), 0)
	assert.Equal(t, 2, m.Changes)
	assert.Equal(t, 5, m.Files)
	assert.Equal(t, 2, m.FixedFiles)
	assert.Equal(t, 2, m.Covered, "top-1 is a in the first change, e in the second")
	assert.InDelta(t, 1.0, m.Coverage, 1e-12)
	assert.Equal(t, 95, m.LinesTotal) // 10+50+5 + 4+6+20
	assert.Equal(t, 30, m.LinesRead)  // a (10) + e (20)
	assert.InDelta(t, 30.0/95.0, m.LinesShare, 1e-12)
	// baseline reads the largest file: b (50, not fixed) then e (20, fixed)
	assert.InDelta(t, 0.5, m.BaselineCover, 1e-12)
	assert.InDelta(t, 70.0/95.0, m.BaselineShare, 1e-12)
}

func TestEvaluateFromIndex(t *testing.T) {
	m := handBuiltBacktest().Evaluate(DefaultWeights(), 1)
	assert.Equal(t, 1, m.Changes)
	assert.Equal(t, 2, m.Files)
	assert.Equal(t, 1, m.FixedFiles)
	assert.Equal(t, 1, m.Covered)
	assert.Equal(t, 30, m.LinesTotal)
	assert.Equal(t, 20, m.LinesRead)
	assert.InDelta(t, 20.0/30.0, m.LinesShare, 1e-12)
}

func TestEvaluateEmpty(t *testing.T) {
	m := (&Backtest{Top: 3}).Evaluate(DefaultWeights(), 0)
	assert.Equal(t, Metrics{}, m)
	m = handBuiltBacktest().Evaluate(DefaultWeights(), 2)
	assert.Equal(t, Metrics{}, m)
}

func TestEvaluateTopLargerThanFiles(t *testing.T) {
	b := handBuiltBacktest()
	b.Top = 10
	m := b.Evaluate(DefaultWeights(), 0)
	assert.Equal(t, 2, m.Covered)
	assert.Equal(t, m.LinesTotal, m.LinesRead)
	assert.InDelta(t, 1.0, m.LinesShare, 1e-12)
	assert.InDelta(t, 1.0, m.BaselineCover, 1e-12)
}

func TestCountsLine(t *testing.T) {
	assert.Equal(t, "a 1 · b 22 · c 3", countsLine(map[string]int{"c": 3, "a": 1, "b": 22}))
	assert.Equal(t, "", countsLine(nil))
}

func TestPct(t *testing.T) {
	assert.Equal(t, "0%", pct(0))
	assert.Equal(t, "50%", pct(0.5))
	assert.Equal(t, "33%", pct(1.0/3))
	assert.Equal(t, "67%", pct(2.0/3))
	assert.Equal(t, "13%", pct(0.125), "0.5 rounds up")
	assert.Equal(t, "100%", pct(1))
}

func TestDayAndMinInt(t *testing.T) {
	assert.Equal(t, "2024-01-01", day(1704067200))
	assert.Equal(t, 2, minInt(2, 3))
	assert.Equal(t, 2, minInt(3, 2))
	assert.Equal(t, 3, maxInt(3, 2))
}

func TestBacktestTextAndJSONOffline(t *testing.T) {
	b := handBuiltBacktest()
	b.Ref, b.Landings = "main", 4
	b.LeftOut = map[string]int{"too recent to know if a fix followed": 2}
	b.Generic = b.Evaluate(DefaultWeights(), 0)
	text := b.Text()
	assert.Contains(t, text, "cribelo backtest")
	assert.Contains(t, text, "4 landings on main replayed")
	assert.Contains(t, text, "Left out: too recent to know if a fix followed 2")
	assert.Contains(t, text, "generic weights")
	assert.NotContains(t, text, "calibrated weights")

	m := b.Generic
	b.Calibrated = &m
	b.GenericHeldOut = &m
	assert.Contains(t, b.Text(), "held-out check")
	assert.Contains(t, b.Text(), "calibrated weights")

	out, err := b.JSON()
	require.NoError(t, err)
	assert.True(t, json.Valid([]byte(out)))

	empty := &Backtest{Ref: "main"}
	assert.Contains(t, empty.Text(), "No landing qualified.")
}
