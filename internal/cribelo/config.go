package cribelo

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ConfigFile is the optional repository configuration, .cribelo.yaml at the root.
const ConfigFile = ".cribelo.yaml"

// WeightsFile is where calibrate stores the learned weights.
const WeightsFile = ".cribelo/weights.json"

// Config is what a repository may fix once instead of passing flags.
type Config struct {
	// Base is the reference to compare against, e.g. origin/develop.
	Base string `yaml:"base,omitempty"`
	// Top is how many files to read when no budget is given.
	Top int `yaml:"top,omitempty"`
	// FailOn lists the kinds of fact that fail the gate when the change
	// states one: cycle, dependency, library, signature, api, clone,
	// history, coverage. A list, or one comma-separated string.
	FailOn FactList `yaml:"fail_on,omitempty"`
	// FailOver is the reading time of the whole change above which the
	// gate fails, e.g. 1h or 45m.
	FailOver string `yaml:"fail_over,omitempty"`
	// Ignore lists glob patterns (path.Match on the relative path) of files
	// that are never ranked nor listed, e.g. "docs/**" or "*.generated.ts".
	Ignore []string `yaml:"ignore,omitempty"`
	// FixPattern is an extra regular expression that marks a commit as a
	// repair, besides the built-in words (fix, bug, revert, correction...).
	// Typically a tracker key: "BUG-\\d+" or "JIRA-[0-9]+ .*fix".
	FixPattern string `yaml:"fix_pattern,omitempty"`
	// Coverage is the coverage report to read, relative to the root
	// (lcov, Cobertura, Clover or a Go profile); "none" reads none. Empty
	// looks in the usual places.
	Coverage string `yaml:"coverage,omitempty"`
}

// FactList is a list of fact kinds, written in YAML as a list or as one
// comma-separated string.
type FactList []string

// UnmarshalYAML accepts "cycle, signature" as well as [cycle, signature].
func (l *FactList) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		*l = SplitList(node.Value)
		return nil
	}
	var items []string
	if err := node.Decode(&items); err != nil {
		return err
	}
	*l = items
	return nil
}

// SplitList splits a comma-separated list, blanks trimmed, empties dropped.
func SplitList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// LoadConfig reads .cribelo.yaml under root; a missing file is an empty config.
func LoadConfig(root string) (Config, error) {
	var c Config
	data, err := os.ReadFile(filepath.Join(root, ConfigFile))
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return c, err
	}
	if err := yaml.Unmarshal(data, &c); err != nil {
		return c, err
	}
	return c, nil
}

// Ignored reports whether a relative path matches one of the ignore globs.
// A pattern ending in "/**" matches everything below that directory.
func (c Config) Ignored(rel string) bool {
	for _, pattern := range c.Ignore {
		if len(pattern) > 3 && pattern[len(pattern)-3:] == "/**" {
			prefix := pattern[:len(pattern)-2]
			if len(rel) >= len(prefix) && rel[:len(prefix)] == prefix {
				return true
			}
			continue
		}
		if ok, _ := path.Match(pattern, rel); ok {
			return true
		}
		if ok, _ := path.Match(pattern, path.Base(rel)); ok {
			return true
		}
	}
	return false
}
