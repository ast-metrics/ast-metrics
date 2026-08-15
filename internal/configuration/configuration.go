package configuration

import (
	"os"
	"path/filepath"
	"strings"

	storage "github.com/ast-metrics/ast-metrics/internal/storage"
)

// FileDiscoveryCache is an opaque type to avoid import cycles.
// It holds a pointer to file.FileDiscovery at runtime.
type FileDiscoveryCache interface{}

type Configuration struct {
	// The path to the sources to analyze
	SourcesToAnalyzePath []string `yaml:"sources"`

	// Exclude patterns (list of regular expressions. When a file matches one of these patterns, it is not analyzed)
	ExcludePatterns []string `yaml:"exclude"`

	// Reports
	Reports ConfigurationReport `yaml:"reports,omitempty"`

	// Requirements
	Requirements *ConfigurationRequirements `yaml:"requirements,omitempty"`

	Watching bool `yaml:"watching,omitempty"`

	// if not empty, compare the current analysis with the one in this branch / commit
	CompareWith string `yaml:"comparewith,omitempty"`

	// Architecture enables the optional AI-based architecture classification.
	// It is intentionally CLI-only because loading the classifier is expensive.
	Architecture bool `yaml:"-"`

	// Extra file extensions per language (e.g. {"php": [".inc", ".module"]})
	Extensions map[string][]string `yaml:"extensions,omitempty"`

	// Location of cache files
	Storage *storage.Workdir `yaml:"-"`

	IsComingFromConfigFile bool `yaml:"-"`

	// ConfigurationFilePath is the absolute path of the file this configuration
	// was read from. Empty when no configuration file was found.
	ConfigurationFilePath string `yaml:"-"`

	// Scopes binds every analyzed source to the configuration that governs it.
	// Built by ResolveScopes, which SetSourcesToAnalyzePath calls.
	Scopes []Scope `yaml:"-"`

	// FileDiscovery holds a pre-computed file discovery cache (type *file.FileDiscovery).
	// Stored as interface{} to avoid import cycles.
	FileDiscovery FileDiscoveryCache `yaml:"-"`

	ModelClassifierDirectory string
}

type ConfigurationReport struct {
	Html        string `yaml:"html,omitempty"`
	Markdown    string `yaml:"markdown,omitempty"`
	Json        string `yaml:"json,omitempty"`
	OpenMetrics string `yaml:"openmetrics,omitempty"`
	Sarif       string `yaml:"sarif,omitempty"`
	// SarifMaxLevel caps the level of the SARIF results (error, warning or note).
	// Empty keeps the level derived from the severity of the finding.
	SarifMaxLevel string `yaml:"sarif_max_level,omitempty"`
	OpenHtml      bool   `yaml:"open_html,omitempty"`
	// Summary is the plain-text report written on stdout. It is the report you
	// get when you ask for no other one, so that a run always produces something
	// readable instead of analyzing a whole project for nothing. It is a pointer
	// to tell "not configured" (defaults to enabled) from an explicit disabling.
	Summary *bool `yaml:"summary,omitempty"`
}

// HasReports reports whether at least one report file has to be written. The
// stdout summary is deliberately not counted here: it produces no file, and the
// views listing the generated reports only deal with files.
func (c *ConfigurationReport) HasReports() bool {
	return c.Html != "" || c.Markdown != "" || c.Json != "" || c.OpenMetrics != "" || c.Sarif != ""
}

// HasSummary reports whether the plain-text summary has to be printed. It is
// enabled unless it has been explicitly turned off.
func (c *ConfigurationReport) HasSummary() bool {
	return c.Summary == nil || *c.Summary
}

type ConfigurationRequirements struct {
	Rules   *ConfigurationRequirementsRules `yaml:"rules"`
	Exclude []string                        `yaml:"exclude,omitempty"`
	// Baseline is the path to a baseline file listing violations to ignore,
	// similar to PHPStan's baseline. It lets an existing project start
	// enforcing requirements without having to fix every pre-existing
	// violation first. When empty, DefaultBaselineFilename is used if that
	// file exists in the working directory.
	Baseline string `yaml:"baseline,omitempty"`
}

type ConfigurationCouplingRule struct {
	Forbidden []struct {
		From string `yaml:"from"`
		To   string `yaml:"to"`
	} `yaml:"forbidden,omitempty"`
}

type ConfigurationArchitectureRules struct {
	Coupling               *ConfigurationCouplingRule `yaml:"coupling,omitempty"`
	AfferentCoupling       *int                       `yaml:"max_afferent_coupling,omitempty"`
	EfferentCoupling       *int                       `yaml:"max_efferent_coupling,omitempty"`
	Maintainability        *int                       `yaml:"min_maintainability,omitempty"`
	NoCircularDependencies *bool                      `yaml:"no_circular_dependencies,omitempty"`
	MaxResponsibilities    *int                       `yaml:"max_responsibilities,omitempty"`
	NoGodClass             *bool                      `yaml:"no_god_class,omitempty"`
}

type ConfigurationVolumeRules struct {
	Loc                    *int `yaml:"max_loc,omitempty"`
	Lloc                   *int `yaml:"max_logical_loc,omitempty"`
	LocByMethod            *int `yaml:"max_loc_by_method,omitempty"`
	LlocByMethod           *int `yaml:"max_logical_loc_by_method,omitempty"`
	MaxMethodsPerClass     *int `yaml:"max_methods_per_class,omitempty"`
	MaxSwitchCases         *int `yaml:"max_switch_cases,omitempty"`
	MaxParametersPerMethod *int `yaml:"max_parameters_per_method,omitempty"`
	MaxNestedBlocks        *int `yaml:"max_nested_blocks,omitempty"`
	MaxPublicMethods       *int `yaml:"max_public_methods,omitempty"`
}

type ConfigurationComplexityRules struct {
	Cyclomatic *int `yaml:"max_cyclomatic,omitempty"`
}

type ConfigurationOOPRules struct {
	Maintainability *int `yaml:"min_maintainability,omitempty"`
}

type ConfigurationRequirementsRules struct {
	// New nested rulesets
	Architecture              *ConfigurationArchitectureRules `yaml:"architecture,omitempty"`
	Volume                    *ConfigurationVolumeRules       `yaml:"volume,omitempty"`
	Complexity                *ConfigurationComplexityRules   `yaml:"complexity,omitempty"`
	ObjectOrientedProgramming *ConfigurationOOPRules          `yaml:"object-oriented-programming,omitempty"`
	Golang                    *ConfigurationGolangRuleset     `yaml:"golang,omitempty"`
	Testing                   *ConfigurationTestingRules      `yaml:"testing,omitempty"`

	// Legacy flat rules support for backward compatibility
	CyclomaticLegacy *ConfigurationDefaultRule `yaml:"cyclomatic_complexity,omitempty"`
}

type ConfigurationTestingRules struct {
	MinTraceability   *int     `yaml:"min_traceability,omitempty"`
	MinIsolationScore *int     `yaml:"min_isolation_score,omitempty"`
	MaxGodTestFanOut  *int     `yaml:"max_god_test_fan_out,omitempty"`
	MaxOrphanWeight   *float64 `yaml:"max_orphan_weight,omitempty"`
}

// ConfigurationGolangRuleset toggles for Golang-specific best-practice rules (per-rule)
// If a field is set to true, the corresponding rule is enabled. Omitting or false disables it.
type ConfigurationGolangRuleset struct {
	NoPackageNameInMethod *bool `yaml:"no_package_name_in_method,omitempty"`
	MaxNesting            *int  `yaml:"max_nesting,omitempty"`
	MaxFileSize           *int  `yaml:"max_file_size,omitempty"`
	MaxFilesPerPackage    *int  `yaml:"max_files_per_package,omitempty"`
	SlicePrealloc         *bool `yaml:"slice_prealloc,omitempty"`
	ContextMissing        *bool `yaml:"context_missing,omitempty"`
	ContextIgnored        *bool `yaml:"context_ignored,omitempty"`
}

type ConfigurationDefaultRule struct {
	Max             int      `yaml:"max"`
	Min             int      `yaml:"min"`
	ExcludePatterns []string `yaml:"exclude"`
}

// defaultExcludePatterns returns the directories left out of every analysis
// unless a configuration file says otherwise. A fresh slice is returned, so
// that a configuration keeping the defaults cannot alter another one.
func defaultExcludePatterns() []string {
	return []string{"/vendor/", "/node_modules/", "/.git/", "/.idea/", "/_ide_helper/", "/var/", "/.claude/", "/.venv/", "/__pycache__/"}
}

func NewConfiguration() *Configuration {
	return &Configuration{
		SourcesToAnalyzePath:     []string{},
		ExcludePatterns:          defaultExcludePatterns(),
		Watching:                 false,
		CompareWith:              "",
		Architecture:             false,
		Storage:                  storage.Default(),
		IsComingFromConfigFile:   false,
		ModelClassifierDirectory: "ai/training/classifier/build",
	}
}

func NewConfigurationRequirements() *ConfigurationRequirements {
	return &ConfigurationRequirements{
		Rules: &ConfigurationRequirementsRules{
			Architecture:              &ConfigurationArchitectureRules{},
			Volume:                    &ConfigurationVolumeRules{},
			Complexity:                &ConfigurationComplexityRules{},
			ObjectOrientedProgramming: &ConfigurationOOPRules{},
		},
	}
}

func (c *Configuration) SetSourcesToAnalyzePath(paths []string) error {

	(*c).SourcesToAnalyzePath = []string{}

	// foreach path, make it absolute
	for i := range paths {

		// ensure path exists
		if _, err := os.Stat(paths[i]); err != nil {
			return err
		}

		// make path absolute
		if !filepath.IsAbs(paths[i]) {
			var err error
			paths[i], err = filepath.Abs(paths[i])
			if err != nil {
				return err
			}
		}

		// ensure path exists
		if _, err := os.Stat(paths[i]); err != nil {
			return err
		}

		// Remove trailing slash
		paths[i] = filepath.Clean(paths[i])
	}

	(*c).SourcesToAnalyzePath = paths

	// Sources are also the scopes: a source directory holding its own
	// configuration file is governed by it. Resolving here keeps the two in
	// step, since this is the one place sources are set.
	return c.ResolveScopes()
}

func (c *Configuration) SetExcludePatterns(patterns []string) {
	// Ensure patterns are valid regular expressions
	// @todo
	(*c).ExcludePatterns = patterns
}

var defaultExtensions = map[string]string{
	"php": ".php", "go": ".go", "python": ".py", "rust": ".rs", "typescript": ".ts",
	"java": ".java", "csharp": ".cs",
}

// GetExtensionsForLanguage returns every extension a file of lang can carry:
// the built-in one, plus the extra ones declared by this configuration and by
// any scope. The union is what has to be looked for during the discovery;
// which files a scope actually owns is decided there, against the extensions
// that scope itself declares.
func (c *Configuration) GetExtensionsForLanguage(lang string) []string {
	base := []string{defaultExtensions[lang]}
	base = append(base, normalizeExtensions(c.Extensions[lang])...)
	for _, scope := range c.Scopes {
		if scope.Configuration == nil || scope.Configuration == c {
			continue
		}
		base = append(base, normalizeExtensions(scope.Configuration.Extensions[lang])...)
	}

	return uniqueStrings(base)
}

// DeclaredExtensions returns the extra extensions this configuration maps to a
// language, on top of the built-in ones.
func (c *Configuration) DeclaredExtensions() []string {
	var declared []string
	for _, extensions := range c.Extensions {
		declared = append(declared, normalizeExtensions(extensions)...)
	}

	return declared
}

// normalizeExtensions makes every extension start with a dot, so that a
// configuration can write "inc" as well as ".inc".
func normalizeExtensions(extensions []string) []string {
	normalized := make([]string, 0, len(extensions))
	for _, ext := range extensions {
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		normalized = append(normalized, ext)
	}

	return normalized
}

func uniqueStrings(s []string) []string {
	seen := make(map[string]bool, len(s))
	result := make([]string, 0, len(s))
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}
