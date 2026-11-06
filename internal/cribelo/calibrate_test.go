package cribelo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStandardize(t *testing.T) {
	rows := [][]float64{{1, 5}, {3, 5}, {5, 5}}
	means, stds := standardize(rows)
	assert.InDelta(t, 3.0, means[0], 1e-12)
	assert.InDelta(t, 5.0, means[1], 1e-12)
	assert.InDelta(t, 1.6329931618554521, stds[0], 1e-9) // sqrt(8/3)
	assert.Equal(t, 1.0, stds[1], "constant column keeps std 1")
	// rows are standardized in place
	assert.InDelta(t, -1.2247448713915892, rows[0][0], 1e-9)
	assert.InDelta(t, 0.0, rows[1][0], 1e-12)
	assert.Equal(t, 0.0, rows[0][1])

	m, s := standardize(nil)
	assert.Nil(t, m)
	assert.Nil(t, s)
}

func TestLogisticRegressionSeparable(t *testing.T) {
	var rows [][]float64
	var labels []float64
	for i := 0; i < 40; i++ {
		x := float64(i%20) - 9.5 // symmetric around 0
		noise := float64((i*7)%5) - 2
		rows = append(rows, []float64{x, noise})
		if x > 0 {
			labels = append(labels, 1)
		} else {
			labels = append(labels, 0)
		}
	}
	standardize(rows)
	w, b := logisticRegression(rows, labels)
	require.Len(t, w, 2)
	assert.Greater(t, w[0], 0.5, "informative column learns a clear positive weight")
	assert.Greater(t, w[0], 5*absF(w[1]), "noise column stays small")
	for i, row := range rows {
		z := b + w[0]*row[0] + w[1]*row[1]
		assert.Equal(t, labels[i] == 1, sigmoid(z) > 0.5, "row %d", i)
	}
	// deterministic
	w2, b2 := logisticRegression(rows, labels)
	assert.Equal(t, w, w2)
	assert.Equal(t, b, b2)
}

func absF(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// syntheticBacktest has n changes of four files each; the two files with the
// most past fixes and complexity are the ones fixed later.
func syntheticBacktest(n int) *Backtest {
	b := &Backtest{Methodology: MethodologyVersion, Ref: "main", Top: 3, LeftOut: map[string]int{}}
	for i := 0; i < n; i++ {
		change := ChangeEval{Sha: "sha", Time: int64(1000 + i), Subject: "s"}
		for j := 0; j < 4; j++ {
			added := 10 + (i*7+j*3)%25
			change.Files = append(change.Files, Sample{
				Path: "f" + string(rune('0'+j)) + ".go", Added: added, Deleted: j,
				Values: map[Feature]float64{
					FeatureLinesAdded:      float64(added),
					FeatureLinesDeleted:    float64(j),
					FeatureFixes90d:        float64(j),
					FeatureComplexityAdded: float64(2 * j),
					FeatureUntested:        1,
				},
				Fixed: j >= 2,
			})
		}
		b.Changes = append(b.Changes, change)
	}
	return b
}

func TestCalibrateNotEnoughChanges(t *testing.T) {
	_, err := Calibrate(syntheticBacktest(minCalibrationChanges-1), time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "29 changes")
}

func TestCalibrateNotEnoughPositives(t *testing.T) {
	b := syntheticBacktest(40)
	for i := range b.Changes {
		for j := range b.Changes[i].Files {
			b.Changes[i].Files[j].Fixed = false
		}
	}
	_, err := Calibrate(b, time.Now())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fixed later")
}

func TestCalibrateSynthetic(t *testing.T) {
	b := syntheticBacktest(40)
	now := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	cal, err := Calibrate(b, now)
	require.NoError(t, err)
	assert.Equal(t, 28, cal.TrainCount)
	assert.Equal(t, 12, cal.TestCount)
	assert.Equal(t, 28*4, cal.Samples)
	assert.Equal(t, 28*2, cal.Positives)
	assert.True(t, cal.Improved)
	assert.NotEmpty(t, cal.Explanation)
	assert.NotContains(t, cal.Explanation, "generic model is kept")

	w := cal.Weights
	assert.True(t, w.Calibrated)
	assert.Equal(t, MethodologyVersion, w.Methodology)
	assert.Equal(t, 40, w.TrainedOn)
	assert.Equal(t, "2026-05-06", w.TrainedAt)
	assert.Greater(t, w.Features[FeatureFixes90d].Weight+w.Features[FeatureComplexityAdded].Weight, 0.0)
	// the clone signal is not learned and keeps its default
	assert.Equal(t, DefaultWeights().Features[FeatureClone], w.Features[FeatureClone])
	assert.Len(t, w.Features, len(AllFeatures))
	for _, f := range AllFeatures {
		assert.Greater(t, w.Features[f].Std, 0.0)
	}
	assert.GreaterOrEqual(t, cal.AfterTest.Covered, cal.BeforeTest.Covered)
	assert.Equal(t, 12, cal.AfterTest.Changes)

	// deterministic
	cal2, err := Calibrate(b, now)
	require.NoError(t, err)
	assert.Equal(t, cal.Weights, cal2.Weights)
}

func TestCalibrateImprovedMirrorsComparison(t *testing.T) {
	b := syntheticBacktest(40)
	// training half: signals predict the fix; held-out changes: the opposite
	for i := 28; i < 40; i++ {
		for j := range b.Changes[i].Files {
			b.Changes[i].Files[j].Fixed = j < 2
		}
	}
	cal, err := Calibrate(b, time.Now())
	require.NoError(t, err)
	// with Top = 3 of 4 files, both models read the same three; force a difference
	// only by checking the invariant: Improved mirrors the comparison.
	assert.Equal(t, cal.AfterTest.Covered >= cal.BeforeTest.Covered && cal.AfterTest.RuleCovered >= cal.BeforeTest.RuleCovered, cal.Improved)
}

func TestMatchedThresholdsKeepTheShareOfLevels(t *testing.T) {
	// Six files: with the generic model two are high and one is medium. The
	// learned model scores them all low; its thresholds must still flag the
	// two best as high and the third as medium.
	generic := DefaultWeights()
	learned := DefaultWeights()
	learned.Intercept = -8 // every score is far below 0.40
	var files []Sample
	for _, lines := range []float64{3000, 2000, 300, 10, 5, 1} {
		files = append(files, Sample{Values: map[Feature]float64{FeatureLinesAdded: lines, FeatureComplexityAdded: lines / 10}})
	}
	changes := []ChangeEval{{Files: files}}
	highs, mediums := 0, 0
	for _, s := range files {
		g, _ := generic.Score(s.Values)
		switch generic.Level(g) {
		case LevelHigh:
			highs++
		case LevelMedium:
			mediums++
		}
	}
	require.Equal(t, 2, highs)
	require.Equal(t, 1, mediums)

	high, medium := matchedThresholds(changes, generic, learned)
	assert.Less(t, high, 0.40)
	assert.Less(t, medium, high)
	learned.High, learned.Medium = high, medium
	var levels []Level
	for _, s := range files {
		l, _ := learned.Score(s.Values)
		levels = append(levels, learned.Level(l))
	}
	assert.Equal(t, []Level{LevelHigh, LevelHigh, LevelMedium, LevelLow, LevelLow, LevelLow}, levels)
}

func TestMatchedThresholdsWithoutHighFiles(t *testing.T) {
	generic := DefaultWeights()
	learned := DefaultWeights()
	files := []Sample{{Values: map[Feature]float64{FeatureLinesAdded: 1}}, {Values: map[Feature]float64{FeatureLinesAdded: 2}}}
	high, medium := matchedThresholds([]ChangeEval{{Files: files}}, generic, learned)
	assert.Less(t, medium, high)
	for _, s := range files {
		l, _ := learned.Score(s.Values)
		assert.Equal(t, LevelLow, Weights{High: high, Medium: medium}.Level(l), "no file may become high or medium")
	}
}
