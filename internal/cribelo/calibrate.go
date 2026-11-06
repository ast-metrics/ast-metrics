package cribelo

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Calibration is the outcome of learning weights from a backtest.
type Calibration struct {
	Weights     Weights `json:"weights"`
	TrainCount  int     `json:"train_changes"`
	TestCount   int     `json:"test_changes"`
	Positives   int     `json:"positive_files"`
	Samples     int     `json:"files"`
	BeforeTest  Metrics `json:"generic_on_test"`
	AfterTest   Metrics `json:"calibrated_on_test"`
	Improved    bool    `json:"improved"`
	Explanation string  `json:"explanation"`
}

// learnable excludes the clone signal, which the backtest does not replay.
var learnable = []Feature{
	FeatureLinesAdded, FeatureLinesDeleted, FeatureComplexityAdded, FeatureMaxComplexity,
	FeatureAfferent, FeatureFixes90d, FeatureChurn90d, FeatureNewFile, FeatureUntested,
}

const (
	minCalibrationChanges   = 30
	minCalibrationPositives = 10
	trainShare              = 0.7
)

// Calibrate fits a logistic regression on the oldest 70% of the replayed
// changes and checks it on the newest 30%. The learned weights are kept only
// when they cover at least as many later-fixed files as the generic ones on
// those held-out changes; a repository whose history disagrees with the
// model keeps the generic model, and says so.
func Calibrate(b *Backtest, now time.Time) (*Calibration, error) {
	if len(b.Changes) < minCalibrationChanges {
		return nil, fmt.Errorf("only %d changes qualified, %d are needed to calibrate", len(b.Changes), minCalibrationChanges)
	}
	split := int(float64(len(b.Changes)) * trainShare)
	cal := &Calibration{TrainCount: split, TestCount: len(b.Changes) - split}

	// Gather and transform the training rows.
	var rows [][]float64
	var labels []float64
	for _, change := range b.Changes[:split] {
		for _, s := range change.Files {
			row := make([]float64, len(learnable))
			for i, f := range learnable {
				row[i] = f.transform(s.Values[f])
			}
			rows = append(rows, row)
			if b.positive(s) {
				labels = append(labels, 1)
				cal.Positives++
			} else {
				labels = append(labels, 0)
			}
		}
	}
	cal.Samples = len(rows)
	if cal.Positives < minCalibrationPositives {
		return nil, fmt.Errorf("only %d files were fixed later in the training changes, %d are needed", cal.Positives, minCalibrationPositives)
	}

	means, stds := standardize(rows)
	weights, intercept := logisticRegression(rows, labels)

	learned := DefaultWeights()
	learned.Calibrated = true
	learned.TrainedOn = len(b.Changes)
	learned.TrainedAt = now.UTC().Format("2006-01-02")
	learned.Intercept = intercept
	for i, f := range learnable {
		learned.Features[f] = FeatureWeight{Weight: weights[i], Mean: means[i], Std: stds[i]}
	}
	learned.High, learned.Medium = matchedThresholds(b.Changes[:split], DefaultWeights(), learned)
	cal.Weights = learned

	cal.BeforeTest = b.Evaluate(DefaultWeights(), split)
	cal.AfterTest = b.Evaluate(learned, split)
	cal.Improved = cal.AfterTest.Covered >= cal.BeforeTest.Covered && cal.AfterTest.RuleCovered >= cal.BeforeTest.RuleCovered
	if cal.Improved {
		cal.Explanation = fmt.Sprintf("on the %d most recent changes, calibrated weights cover %s of the later-fixed files in the top-%d and %s with every high file (generic: %s and %s)", cal.TestCount, pct(cal.AfterTest.Coverage), b.Top, pct(cal.AfterTest.RuleCoverage), pct(cal.BeforeTest.Coverage), pct(cal.BeforeTest.RuleCoverage))
	} else {
		cal.Explanation = fmt.Sprintf("on the %d most recent changes, calibrated weights cover %s of the later-fixed files in the top-%d and %s with every high file, generic ones %s and %s: the generic model is kept", cal.TestCount, pct(cal.AfterTest.Coverage), b.Top, pct(cal.AfterTest.RuleCoverage), pct(cal.BeforeTest.Coverage), pct(cal.BeforeTest.RuleCoverage))
	}
	return cal, nil
}

// matchedThresholds picks the high and medium thresholds of the learned model
// so that, on the training files, it flags the same share of files high and
// medium as the generic model does. A learned model speaks in probabilities
// of a later fix, which are low when fixes are rare; the levels keep their
// meaning of "the riskiest files of the change" regardless.
func matchedThresholds(changes []ChangeEval, generic, learned Weights) (high, medium float64) {
	var genericScores, learnedScores []float64
	for _, change := range changes {
		for _, s := range change.Files {
			g, _ := generic.Score(s.Values)
			l, _ := learned.Score(s.Values)
			genericScores = append(genericScores, g)
			learnedScores = append(learnedScores, l)
		}
	}
	if len(learnedScores) == 0 {
		return generic.High, generic.Medium
	}
	highs, mediums := 0, 0
	for _, g := range genericScores {
		switch generic.Level(g) {
		case LevelHigh:
			highs++
		case LevelMedium:
			mediums++
		}
	}
	sort.Sort(sort.Reverse(sort.Float64Slice(learnedScores)))
	// above is a score no training file reaches: the threshold of an empty
	// level. Scores are sigmoids, so it stays below 1.01.
	above := learnedScores[0] + 1e-6
	threshold := func(count int) float64 {
		if count == 0 {
			return above
		}
		return learnedScores[count-1]
	}
	high = threshold(highs)
	medium = threshold(highs + mediums)
	if medium >= high {
		if highs+mediums == 0 {
			high = above + 1e-6
		} else {
			medium = math.Nextafter(high, 0)
		}
	}
	return high, medium
}

// standardize centers and scales every column in place and returns the
// statistics used. A constant column keeps a standard deviation of 1.
func standardize(rows [][]float64) (means, stds []float64) {
	if len(rows) == 0 {
		return nil, nil
	}
	n := len(rows[0])
	means = make([]float64, n)
	stds = make([]float64, n)
	for _, row := range rows {
		for j, v := range row {
			means[j] += v
		}
	}
	for j := range means {
		means[j] /= float64(len(rows))
	}
	for _, row := range rows {
		for j, v := range row {
			stds[j] += (v - means[j]) * (v - means[j])
		}
	}
	for j := range stds {
		stds[j] = math.Sqrt(stds[j] / float64(len(rows)))
		if stds[j] < 1e-9 {
			stds[j] = 1
		}
	}
	for _, row := range rows {
		for j := range row {
			row[j] = (row[j] - means[j]) / stds[j]
		}
	}
	return means, stds
}

// logisticRegression fits weights by full-batch gradient descent with a
// small L2 penalty. Deterministic: zero initialization, fixed schedule.
func logisticRegression(rows [][]float64, labels []float64) (weights []float64, intercept float64) {
	const (
		iterations = 3000
		rate       = 0.1
		l2         = 0.01
	)
	n := len(rows[0])
	weights = make([]float64, n)
	grad := make([]float64, n)
	m := float64(len(rows))
	for it := 0; it < iterations; it++ {
		for j := range grad {
			grad[j] = 0
		}
		gradB := 0.0
		for i, row := range rows {
			z := intercept
			for j, v := range row {
				z += weights[j] * v
			}
			diff := sigmoid(z) - labels[i]
			gradB += diff
			for j, v := range row {
				grad[j] += diff * v
			}
		}
		intercept -= rate * gradB / m
		for j := range weights {
			weights[j] -= rate * (grad[j]/m + l2*weights[j])
		}
	}
	return weights, intercept
}
