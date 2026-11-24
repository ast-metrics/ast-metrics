package cribelo

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultWeightsCoverEveryFeature(t *testing.T) {
	w := DefaultWeights()
	assert.Equal(t, MethodologyVersion, w.Methodology)
	assert.False(t, w.Calibrated)
	assert.Len(t, w.Features, len(AllFeatures))
	for _, f := range AllFeatures {
		fw, ok := w.Features[f]
		require.True(t, ok, f)
		assert.Equal(t, 1.0, fw.Std)
		assert.Equal(t, 0.0, fw.Mean)
		assert.Greater(t, fw.Weight, 0.0)
		assert.NotEmpty(t, f.Describe())
	}
	assert.Equal(t, "weird", Feature("weird").Describe())
}

func TestFeatureTransform(t *testing.T) {
	assert.InDelta(t, math.Log1p(9), FeatureLinesAdded.transform(9), 1e-12)
	assert.Equal(t, 0.0, FeatureLinesAdded.transform(-5))
	assert.Equal(t, 1.0, FeatureNewFile.transform(1))
	assert.Equal(t, 0.83, FeatureClone.transform(0.83))
	assert.Equal(t, 1.0, FeatureUntested.transform(1))
}

func TestScoreEmptyValues(t *testing.T) {
	w := DefaultWeights()
	score, contributions := w.Score(map[Feature]float64{})
	assert.InDelta(t, sigmoid(w.Intercept), score, 1e-12)
	assert.Len(t, contributions, len(AllFeatures))
	for _, c := range contributions {
		assert.Equal(t, 0.0, c.Logit)
	}
}

func TestScoreMonotonicInLinesAdded(t *testing.T) {
	w := DefaultWeights()
	prev := -1.0
	for _, added := range []float64{0, 1, 5, 20, 100, 1000} {
		score, _ := w.Score(map[Feature]float64{FeatureLinesAdded: added, FeatureAfferent: 2})
		assert.Greater(t, score, prev)
		assert.Greater(t, score, 0.0)
		assert.Less(t, score, 1.0)
		prev = score
	}
}

func TestScoreContributionsSortedDescending(t *testing.T) {
	w := DefaultWeights()
	w.Features[FeatureLinesDeleted] = FeatureWeight{Weight: -1, Std: 1}
	_, contributions := w.Score(map[Feature]float64{
		FeatureLinesAdded: 50, FeatureLinesDeleted: 50, FeatureFixes90d: 4, FeatureUntested: 1, FeatureClone: 1,
	})
	for i := 1; i < len(contributions); i++ {
		assert.GreaterOrEqual(t, contributions[i-1].Logit, contributions[i].Logit)
	}
	assert.Equal(t, FeatureClone, contributions[0].Feature, "1.5*1 is the largest")
	assert.Equal(t, FeatureLinesDeleted, contributions[len(contributions)-1].Feature)
	assert.Less(t, contributions[len(contributions)-1].Logit, 0.0)
	// value is the raw one, not the transformed one
	for _, c := range contributions {
		if c.Feature == FeatureLinesAdded {
			assert.Equal(t, 50.0, c.Value)
		}
	}
}

func TestScoreUsesMeanAndStd(t *testing.T) {
	w := Weights{Intercept: 0, Features: map[Feature]FeatureWeight{
		FeatureNewFile: {Weight: 2, Mean: 0.5, Std: 0.25},
	}}
	score, contributions := w.Score(map[Feature]float64{FeatureNewFile: 1})
	require.Len(t, contributions, 1)
	assert.InDelta(t, 4.0, contributions[0].Logit, 1e-12) // 2 * (1-0.5)/0.25
	assert.InDelta(t, sigmoid(4), score, 1e-12)
}

func TestLevelOf(t *testing.T) {
	w := DefaultWeights()
	assert.Equal(t, LevelLow, w.Level(0))
	assert.Equal(t, LevelLow, w.Level(0.3999))
	assert.Equal(t, LevelMedium, w.Level(0.40))
	assert.Equal(t, LevelMedium, w.Level(0.6999))
	assert.Equal(t, LevelHigh, w.Level(0.70))
	assert.Equal(t, LevelHigh, w.Level(1))

	w.High, w.Medium = 0.20, 0.05
	assert.Equal(t, LevelHigh, w.Level(0.25))
	assert.Equal(t, LevelMedium, w.Level(0.10))
	assert.Equal(t, LevelLow, w.Level(0.01))
}

func TestLoadWeightsRepairsThresholds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.json")
	for _, body := range []string{
		`{"methodology":"` + MethodologyVersion + `"}`,
		`{"methodology":"` + MethodologyVersion + `","high":0.3,"medium":0.5}`,
	} {
		require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
		w, err := LoadWeights(path)
		require.NoError(t, err)
		assert.Equal(t, DefaultHigh, w.High, body)
		assert.Equal(t, DefaultMedium, w.Medium, body)
	}
	require.NoError(t, os.WriteFile(path, []byte(`{"methodology":"`+MethodologyVersion+`","high":0.2,"medium":0.05}`), 0o644))
	w, err := LoadWeights(path)
	require.NoError(t, err)
	assert.Equal(t, 0.2, w.High)
	assert.Equal(t, 0.05, w.Medium)
}

func TestLoadWeightsMissingFile(t *testing.T) {
	w, err := LoadWeights(filepath.Join(t.TempDir(), "nope.json"))
	require.NoError(t, err)
	assert.Equal(t, DefaultWeights(), w)
}

func TestLoadWeightsInvalidJSON(t *testing.T) {
	p := filepath.Join(t.TempDir(), "w.json")
	require.NoError(t, os.WriteFile(p, []byte("{not json"), 0o644))
	_, err := LoadWeights(p)
	assert.Error(t, err)
}

func TestLoadWeightsOtherMethodologyFallsBack(t *testing.T) {
	p := filepath.Join(t.TempDir(), "w.json")
	data, _ := json.Marshal(Weights{Methodology: "0.1", Calibrated: true, Intercept: 9, Features: map[Feature]FeatureWeight{}})
	require.NoError(t, os.WriteFile(p, data, 0o644))
	w, err := LoadWeights(p)
	require.NoError(t, err)
	assert.Equal(t, DefaultWeights(), w)
	assert.False(t, w.Calibrated)
}

func TestLoadWeightsFillsMissingAndFixesStd(t *testing.T) {
	p := filepath.Join(t.TempDir(), "w.json")
	partial := Weights{
		Methodology: MethodologyVersion, Calibrated: true, Intercept: -1,
		Features: map[Feature]FeatureWeight{
			FeatureLinesAdded: {Weight: 0.9, Mean: 2, Std: 0},
			FeatureFixes90d:   {Weight: 0.1, Mean: 1, Std: -3},
			FeatureAfferent:   {Weight: 0.7, Mean: 1, Std: 2},
		},
	}
	data, _ := json.Marshal(partial)
	require.NoError(t, os.WriteFile(p, data, 0o644))
	w, err := LoadWeights(p)
	require.NoError(t, err)
	assert.True(t, w.Calibrated)
	assert.Equal(t, -1.0, w.Intercept)
	assert.Len(t, w.Features, len(AllFeatures))
	assert.Equal(t, FeatureWeight{Weight: 0.9, Mean: 2, Std: 1}, w.Features[FeatureLinesAdded])
	assert.Equal(t, FeatureWeight{Weight: 0.1, Mean: 1, Std: 1}, w.Features[FeatureFixes90d])
	assert.Equal(t, FeatureWeight{Weight: 0.7, Mean: 1, Std: 2}, w.Features[FeatureAfferent])
	assert.Equal(t, DefaultWeights().Features[FeatureClone], w.Features[FeatureClone])
}

func TestSaveLoadRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "w.json")
	w := DefaultWeights()
	w.Calibrated = true
	w.TrainedOn = 42
	w.TrainedAt = "2026-01-02"
	w.Intercept = -2.5
	w.Features[FeatureChurn90d] = FeatureWeight{Weight: 0.77, Mean: 1.5, Std: 0.5}
	require.NoError(t, w.Save(p))
	data, err := os.ReadFile(p)
	require.NoError(t, err)
	assert.Equal(t, byte('\n'), data[len(data)-1])
	got, err := LoadWeights(p)
	require.NoError(t, err)
	assert.Equal(t, w, got)
}
