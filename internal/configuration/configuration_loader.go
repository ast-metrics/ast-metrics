package configuration

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// configurationFilenames are the names a configuration file can take, from the
// most specific to the most generic one.
var configurationFilenames = []string{
	".ast-metrics.yaml",
	".ast-metrics.yml",
	".ast-metrics.dist.yaml",
	".ast-metrics.dist.yml",
}

type ConfigurationLoader struct {
	FilenameToChecks []string
}

func NewConfigurationLoader() *ConfigurationLoader {
	return &ConfigurationLoader{
		FilenameToChecks: append([]string{}, configurationFilenames...),
	}
}

func (c *ConfigurationLoader) Loads(cfg *Configuration) (*Configuration, error) {
	// Save default exclude patterns before loading config file
	defaultExcludePatterns := cfg.ExcludePatterns

	// Load configuration file
	for _, filename := range c.FilenameToChecks {

		if _, err := os.Stat(filename); err == nil {

			// Load configuration
			data, err := os.ReadFile(filename)
			if err != nil {
				return cfg, err
			}

			if err := decode(data, filename, cfg); err != nil {
				return cfg, err
			}

			// If YAML decode emptied the exclude patterns (e.g. `exclude: []` or
			// missing `exclude` key), restore the defaults so that vendor/,
			// node_modules/, etc. are still excluded.
			if len(cfg.ExcludePatterns) == 0 {
				cfg.ExcludePatterns = defaultExcludePatterns
			}

			cfg.IsComingFromConfigFile = true
			// Scope resolution compares against this path to recognize the
			// configuration it has already loaded.
			if absolute, err := filepath.Abs(filename); err == nil {
				cfg.ConfigurationFilePath = absolute
			}
			return cfg, nil
		}
	}

	return cfg, nil
}

// decode reads a configuration file into cfg. Unknown fields are reported but
// not fatal: a typo or an outdated field name must be surfaced, without
// stopping an analysis the rest of the file describes correctly.
func decode(data []byte, filename string, cfg *Configuration) error {
	strict := yaml.NewDecoder(bytes.NewReader(data))
	strict.KnownFields(true)
	var probe Configuration
	if err := strict.Decode(&probe); err != nil {
		logrus.Warnf("configuration file %s contains unexpected content and some settings may be ignored: %v", filename, err)
	}

	return yaml.Unmarshal(data, cfg)
}

// findConfigurationFile returns the configuration file held by directory, or an
// empty string when it holds none.
func findConfigurationFile(directory string) string {
	for _, filename := range configurationFilenames {
		path := filepath.Join(directory, filename)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return ""
}

// loadScopeConfiguration reads the configuration governing a single scope. It
// starts from the built-in defaults rather than from the root configuration:
// the closest configuration file wins whole, so that it describes the same
// analysis whether its project is analyzed inside the monorepository or alone.
func loadScopeConfiguration(filename string) (*Configuration, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	cfg := &Configuration{ExcludePatterns: defaultExcludePatterns()}
	if err := decode(data, filename, cfg); err != nil {
		return nil, err
	}

	if len(cfg.ExcludePatterns) == 0 {
		cfg.ExcludePatterns = defaultExcludePatterns()
	}

	cfg.IsComingFromConfigFile = true
	cfg.ConfigurationFilePath = filename
	cfg.scopeOnly()

	return cfg, nil
}

func (c *ConfigurationLoader) Import(yamlString string) (*Configuration, error) {
	// Load YAML string into configuration
	cfg := &Configuration{}
	err := yaml.Unmarshal([]byte(yamlString), cfg)
	if err != nil {
		return cfg, err
	}

	return cfg, nil
}

func (c *ConfigurationLoader) CreateDefaultFile() error {
	if len(c.FilenameToChecks) == 0 {
		return errors.New("No filename to check")
	}
	filename := c.FilenameToChecks[0]

	// Create default configuration file
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(`# AST Metrics configuration file
# This file is used to configure AST Metrics
# You can find more information at https://github.com/ast-metrics/ast-metrics/

# Sources to analyze. You can add multiple sources
sources:
  - ./

# Exclude patterns (list of regular expressions. When a file matches one of these patterns, it is not analyzed)
exclude:
  - /vendor/
  - /node_modules/

# Extra file extensions per language (added to built-in defaults)
# extensions:
#   php: [".inc", ".module", ".install", ".theme"]

# Reports to generate
reports:
  html: ./build/report
  markdown: ./build/report.md
  # The summary printed on the standard output. It is enabled by default, and
  # is what you get when no other report is asked for
  # summary: false

# Requirements. If a file does not meet these requirements, it will be reported
requirements:
  # Files matching these patterns are excluded from requirement checks
  # exclude:
  #   - /tests/
  # Path to a baseline file used to ignore pre-existing violations. Generate
  # it with "ast-metrics baseline". Defaults to .ast-metrics-baseline.yaml
  # when that file exists, so this is usually unnecessary.
  # baseline: .ast-metrics-baseline.yaml
  rules:
    architecture:
      # Coupling between components
      coupling:
        forbidden:
          # Fails if a Model is used in a Controller
          # Regular expressions are used
          - from: "Model"
            to: "Controller"
      # max_afferent_coupling: 10
      # max_efferent_coupling: 10
      # min_maintainability: 70
      # Communities: the groups of classes that depend on each other more
      # than on the rest of the code, read from the dependency graph.
      # Fail when two communities depend on each other, and when more than
      # 20% of the dependencies cross from one community to another.
      # no_community_cycles: true
      # max_community_cross_share: 20
      # Freeze the architecture: fail on every dependency crossing from one
      # community to another. Run "ast-metrics baseline" once to accept the
      # current ones, only new crossings fail afterwards.
      # no_cross_community_dependencies: true

    volume:
      # Maximum number of lines of code per file
      max_loc: 100
      # max_logical_loc: 60
      # max_loc_by_method: 30
      # max_logical_loc_by_method: 20

    complexity:
      # Maximum cyclomatic complexity
      max_cyclomatic: 10
`)

	if err != nil {
		return err
	}

	return nil
}
