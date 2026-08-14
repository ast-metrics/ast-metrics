package requirement

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultBaselineFilename is looked up automatically in the working directory
// when no baseline path is configured, the same way .ast-metrics.yaml is
// auto-detected by ConfigurationLoader.
const DefaultBaselineFilename = ".ast-metrics-baseline.yaml"

// BaselineEntry is one unique (rule, file, message) violation together with
// the number of times it currently occurs. Line numbers are intentionally
// not part of the key so that unrelated edits which shift lines up or down
// do not invalidate the baseline.
type BaselineEntry struct {
	Rule    string `yaml:"rule"`
	File    string `yaml:"file,omitempty"`
	Message string `yaml:"message"`
	Count   int    `yaml:"count"`
}

type baselineFile struct {
	Ignored []BaselineEntry `yaml:"ignored"`
}

type baselineKey struct {
	Rule, File, Message string
}

// newBaselineKey builds a key from a violation, normalizing File to a path
// relative to the working directory. This keeps the key stable regardless of
// whether the caller resolved sources to absolute paths (e.g. `ast-metrics
// baseline .`) or left them relative (e.g. `ast-metrics lint` reusing a
// relative `sources:` entry from the configuration file), and keeps the
// generated baseline file portable across machines and CI runners.
func newBaselineKey(rule, file, message string) baselineKey {
	return baselineKey{Rule: rule, File: RelativeToCwd(file), Message: message}
}

// RelativeToCwd shortens an absolute path that lives under the working
// directory. Everything a baseline records has to go through it: a path is what
// makes an entry recognizable from one run to the next, and an absolute one ties
// the file to the machine that produced it, so a baseline generated in a
// container would match nothing on the host or in CI.
func RelativeToCwd(path string) string {
	if path == "" || !filepath.IsAbs(path) {
		return path
	}
	cwd, err := os.Getwd()
	if err != nil {
		return path
	}
	rel, err := filepath.Rel(cwd, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}
	return rel
}

// Baseline is a set of previously known violations to silence, similar to
// PHPStan's baseline: it lets a project adopt requirements without having to
// fix every pre-existing violation before turning lint on.
type Baseline struct {
	counts map[baselineKey]int
}

// NewBaselineFromOutcomes snapshots the given outcomes into a baseline.
func NewBaselineFromOutcomes(outcomes []RuleOutcome) *Baseline {
	b := &Baseline{counts: map[baselineKey]int{}}
	for _, o := range outcomes {
		b.counts[newBaselineKey(o.Rule, o.File, o.Message)]++
	}
	return b
}

// LoadBaseline reads a baseline file. A missing file is not an error: it
// yields an empty baseline so callers do not need to special-case "not
// generated yet".
func LoadBaseline(path string) (*Baseline, error) {
	b := &Baseline{counts: map[baselineKey]int{}}
	if path == "" {
		return b, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return b, nil
		}
		return nil, fmt.Errorf("cannot read baseline file %q: %w", path, err)
	}
	var parsed baselineFile
	if err := yaml.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("cannot parse baseline file %q: %w", path, err)
	}
	for _, e := range parsed.Ignored {
		b.counts[newBaselineKey(e.Rule, e.File, e.Message)] += e.Count
	}
	return b, nil
}

// ResolveBaselinePath returns the configured baseline path, falling back to
// DefaultBaselineFilename when it exists in the working directory.
func ResolveBaselinePath(configured string) string {
	if configured != "" {
		return configured
	}
	if _, err := os.Stat(DefaultBaselineFilename); err == nil {
		return DefaultBaselineFilename
	}
	return ""
}

// Filter removes outcomes already recorded in the baseline, decrementing an
// internal copy of its counters so that extra occurrences of the same
// message beyond what was baselined are still reported as new violations.
func (b *Baseline) Filter(outcomes []RuleOutcome) (kept []RuleOutcome, baselined int) {
	if b == nil || len(b.counts) == 0 {
		return outcomes, 0
	}
	remaining := make(map[baselineKey]int, len(b.counts))
	maps.Copy(remaining, b.counts)
	kept = make([]RuleOutcome, 0, len(outcomes))
	for _, o := range outcomes {
		key := newBaselineKey(o.Rule, o.File, o.Message)
		if remaining[key] > 0 {
			remaining[key]--
			baselined++
			continue
		}
		kept = append(kept, o)
	}
	return kept, baselined
}

// Len returns the number of unique ignored entries.
func (b *Baseline) Len() int {
	if b == nil {
		return 0
	}
	return len(b.counts)
}

// Save writes the baseline to path as YAML, sorted for deterministic diffs.
func (b *Baseline) Save(path string) error {
	entries := make([]BaselineEntry, 0, len(b.counts))
	for key, count := range b.counts {
		entries = append(entries, BaselineEntry{Rule: key.Rule, File: key.File, Message: key.Message, Count: count})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].File != entries[j].File {
			return entries[i].File < entries[j].File
		}
		if entries[i].Rule != entries[j].Rule {
			return entries[i].Rule < entries[j].Rule
		}
		return entries[i].Message < entries[j].Message
	})

	data, err := yaml.Marshal(baselineFile{Ignored: entries})
	if err != nil {
		return err
	}
	header := "# This file is generated by `ast-metrics baseline`. Do not edit it by hand.\n" +
		"# It lists lint violations that already existed when the baseline was\n" +
		"# generated, so they are ignored until fixed. Re-run `ast-metrics baseline`\n" +
		"# to refresh it after fixing some of them or accepting new ones.\n"
	return os.WriteFile(path, []byte(header+string(data)), 0644)
}
