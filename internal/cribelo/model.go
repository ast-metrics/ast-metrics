package cribelo

import (
	"encoding/json"
	"math"
	"os"
	"sort"
)

// MethodologyVersion names the set of features, transforms and default
// weights. Bump it whenever any of them changes, so that a different plan
// can be told apart from a different codebase.
const MethodologyVersion = "1.1"

// Feature is one measured property of a changed file.
type Feature string

const (
	FeatureLinesAdded      Feature = "lines_added"
	FeatureLinesDeleted    Feature = "lines_deleted"
	FeatureComplexityAdded Feature = "complexity_added"
	FeatureMaxComplexity   Feature = "max_complexity"
	FeatureAfferent        Feature = "afferent"
	FeatureFixes90d        Feature = "fixes_90d"
	FeatureChurn90d        Feature = "churn_90d"
	FeatureNewFile         Feature = "new_file"
	FeatureUntested        Feature = "untested"
	FeatureClone           Feature = "clone_similarity"
)

// AllFeatures lists the features in their canonical order.
var AllFeatures = []Feature{
	FeatureLinesAdded, FeatureLinesDeleted, FeatureComplexityAdded, FeatureMaxComplexity,
	FeatureAfferent, FeatureFixes90d, FeatureChurn90d, FeatureNewFile, FeatureUntested, FeatureClone,
}

// Describe is the human wording of a feature, used by explain.
func (f Feature) Describe() string {
	switch f {
	case FeatureLinesAdded:
		return "lines added"
	case FeatureLinesDeleted:
		return "lines deleted"
	case FeatureComplexityAdded:
		return "decision points added (new or grown functions)"
	case FeatureMaxComplexity:
		return "highest cyclomatic complexity among touched functions"
	case FeatureAfferent:
		return "files outside its directory depending on this file"
	case FeatureFixes90d:
		return "fix commits on this file in the last 90 days"
	case FeatureChurn90d:
		return "commits on this file in the last 90 days"
	case FeatureNewFile:
		return "file is new"
	case FeatureUntested:
		return "no related test touched"
	case FeatureClone:
		return "similarity of a touched function with another one in the project"
	}
	return string(f)
}

// counted features are log-scaled, so that the 10th line matters more than
// the 1000th; binary and ratio features are used as they are.
func (f Feature) transform(x float64) float64 {
	switch f {
	case FeatureNewFile, FeatureUntested, FeatureClone:
		return x
	}
	if x < 0 {
		x = 0
	}
	return math.Log1p(x)
}

// FeatureWeight is the contribution of one feature: its transformed value is
// standardized with Mean and Std, then multiplied by Weight.
type FeatureWeight struct {
	Weight float64 `json:"weight"`
	Mean   float64 `json:"mean"`
	Std    float64 `json:"std"`
}

// Weights is the scoring model. The default is a hand-set heuristic; calibrate
// replaces it with weights learned from the repository's own history.
type Weights struct {
	Methodology string                    `json:"methodology"`
	Calibrated  bool                      `json:"calibrated"`
	TrainedOn   int                       `json:"trained_on,omitempty"`
	TrainedAt   string                    `json:"trained_at,omitempty"`
	Intercept   float64                   `json:"intercept"`
	Features    map[Feature]FeatureWeight `json:"features"`
	// High and Medium are the scores from which a file is high or medium.
	// Calibration moves them so that the same share of files stays high and
	// medium as with the generic model.
	High   float64 `json:"high"`
	Medium float64 `json:"medium"`
}

// Default thresholds of the generic model.
const (
	DefaultHigh   = 0.70
	DefaultMedium = 0.40
)

// DefaultWeights is the generic model, used until a repository is calibrated.
// Order of magnitude: a +300-line file with new branching, a fix history and
// no test touched scores around 0.75; a 5-line edit of a stable file, 0.03.
func DefaultWeights() Weights {
	unit := func(w float64) FeatureWeight { return FeatureWeight{Weight: w, Mean: 0, Std: 1} }
	return Weights{
		Methodology: MethodologyVersion,
		Intercept:   -4.0,
		High:        DefaultHigh,
		Medium:      DefaultMedium,
		Features: map[Feature]FeatureWeight{
			FeatureLinesAdded:      unit(0.35),
			FeatureLinesDeleted:    unit(0.10),
			FeatureComplexityAdded: unit(0.50),
			FeatureMaxComplexity:   unit(0.30),
			FeatureAfferent:        unit(0.40),
			FeatureFixes90d:        unit(0.60),
			FeatureChurn90d:        unit(0.20),
			FeatureNewFile:         unit(0.30),
			FeatureUntested:        unit(0.50),
			FeatureClone:           unit(1.50),
		},
	}
}

// LoadWeights reads a weights file; a missing file yields the defaults.
func LoadWeights(path string) (Weights, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultWeights(), nil
		}
		return Weights{}, err
	}
	var w Weights
	if err := json.Unmarshal(data, &w); err != nil {
		return Weights{}, err
	}
	if w.Methodology != MethodologyVersion {
		// The features changed since calibration: the learned weights no
		// longer mean what they meant. Fall back to the generic model.
		return DefaultWeights(), nil
	}
	defaults := DefaultWeights()
	if w.High <= 0 || w.Medium <= 0 || w.Medium >= w.High {
		w.High, w.Medium = defaults.High, defaults.Medium
	}
	if w.Features == nil {
		w.Features = map[Feature]FeatureWeight{}
	}
	for _, f := range AllFeatures {
		if _, ok := w.Features[f]; !ok {
			w.Features[f] = defaults.Features[f]
		}
		if w.Features[f].Std <= 0 {
			fw := w.Features[f]
			fw.Std = 1
			w.Features[f] = fw
		}
	}
	return w, nil
}

// Save writes the weights as indented JSON.
func (w Weights) Save(path string) error {
	data, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Contribution is the signed part of the logit owed to one feature.
type Contribution struct {
	Feature Feature `json:"feature"`
	Value   float64 `json:"value"`
	Logit   float64 `json:"logit"`
}

// Score turns raw feature values into a probability-like score in (0,1) and
// returns the contributions sorted by decreasing weight in the decision.
func (w Weights) Score(values map[Feature]float64) (float64, []Contribution) {
	logit := w.Intercept
	contributions := make([]Contribution, 0, len(AllFeatures))
	for _, f := range AllFeatures {
		fw, ok := w.Features[f]
		if !ok {
			continue
		}
		z := (f.transform(values[f]) - fw.Mean) / fw.Std
		c := fw.Weight * z
		logit += c
		contributions = append(contributions, Contribution{Feature: f, Value: values[f], Logit: c})
	}
	sort.SliceStable(contributions, func(i, j int) bool { return contributions[i].Logit > contributions[j].Logit })
	return sigmoid(logit), contributions
}

func sigmoid(x float64) float64 { return 1 / (1 + math.Exp(-x)) }

// Level buckets a score for gates and colors.
type Level string

const (
	LevelHigh   Level = "high"
	LevelMedium Level = "medium"
	LevelLow    Level = "low"
)

// Level buckets a score with the thresholds of this model.
func (w Weights) Level(score float64) Level {
	switch {
	case score >= w.High:
		return LevelHigh
	case score >= w.Medium:
		return LevelMedium
	}
	return LevelLow
}
