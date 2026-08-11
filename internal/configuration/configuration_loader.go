package configuration

import (
	"bytes"
	"errors"
	"os"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

type ConfigurationLoader struct {
	FilenameToChecks []string
}

func NewConfigurationLoader() *ConfigurationLoader {
	return &ConfigurationLoader{
		FilenameToChecks: []string{
			".ast-metrics.yaml",
			".ast-metrics.yml",
			".ast-metrics.dist.yaml",
			".ast-metrics.dist.yml",
		},
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

			// Strict pass: detect unknown fields and warn instead of silently
			// ignoring them, so typos or outdated field names are surfaced.
			strict := yaml.NewDecoder(bytes.NewReader(data))
			strict.KnownFields(true)
			var probe Configuration
			if derr := strict.Decode(&probe); derr != nil {
				logrus.Warnf("configuration file %s contains unexpected content and some settings may be ignored: %v", filename, derr)
			}

			// Authoritative pass: decode leniently into the running configuration.
			if err := yaml.Unmarshal(data, &cfg); err != nil {
				return cfg, err
			}

			// If YAML decode emptied the exclude patterns (e.g. `exclude: []` or
			// missing `exclude` key), restore the defaults so that vendor/,
			// node_modules/, etc. are still excluded.
			if len(cfg.ExcludePatterns) == 0 {
				cfg.ExcludePatterns = defaultExcludePatterns
			}

			cfg.IsComingFromConfigFile = true
			return cfg, nil
		}
	}

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
