package requirement

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/analyzer/issue"
	"github.com/ast-metrics/ast-metrics/internal/analyzer/ruleset"
	"github.com/ast-metrics/ast-metrics/internal/configuration"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/sirupsen/logrus"
)

// Expose Severity and RequirementError in this package via alias to avoid import cycles
type Severity = issue.Severity
type RequirementError = issue.RequirementError

const (
	SeverityUnknown Severity = issue.SeverityUnknown
	SeverityLow     Severity = issue.SeverityLow
	SeverityMedium  Severity = issue.SeverityMedium
	SeverityHigh    Severity = issue.SeverityHigh
)

// RuleOutcome is a structured message produced by rules
// Message should not include severity prefix; Rule is the rule name; File is the concerned file path when applicable.
type RuleOutcome struct {
	Severity Severity
	Rule     string
	Message  string
	File     string
	// Line is the 1-based line in File where the violation occurs.
	// Zero means the violation is file-level (no specific line).
	Line int
}

type RequirementsEvaluator struct {
	Requirements configuration.ConfigurationRequirements
	// Baseline, when set, filters out already-known violations from Errors.
	// Use LoadBaseline (or ResolveBaselinePath) to build it from a file.
	Baseline *Baseline
	// scopes judges each file against the requirements of the analyzed source
	// it belongs to. Empty means a single project, judged by Requirements.
	scopes []scopedRequirements
}

// scopedRequirements are the requirements governing the files under one
// analyzed source.
type scopedRequirements struct {
	// root is the absolute path the governed files live under. Empty governs
	// every file.
	root         string
	requirements configuration.ConfigurationRequirements
}

type EvaluationResult struct {
	Files             []*pb.File
	ProjectAggregated ProjectAggregated
	Errors            []RuleOutcome
	Successes         []RuleOutcome
	Succeeded         bool
	// Baselined is the number of violations silenced by Baseline.
	Baselined int
}

// method to get number of errors by severity
func (er *EvaluationResult) CountErrorsBySeverity(sev Severity) int {
	count := 0
	for _, e := range er.Errors {
		if e.Severity == sev {
			count++
		}
	}
	return count
}

// methd to get the number of errors by severity (as string)
func (er *EvaluationResult) CountErrorsBySeverityString(sev string) int {
	sev = strings.ToLower(strings.TrimSpace(sev))
	var severity Severity
	switch sev {
	case "high":
		severity = SeverityHigh
	case "medium":
		severity = SeverityMedium
	case "low":
		severity = SeverityLow
	default:
		severity = SeverityUnknown
	}

	return er.CountErrorsBySeverity(severity)
}

// minimal view of ProjectAggregated to avoid import cycle; use original type via alias in caller
// We reuse the original analyzer.ProjectAggregated at call site; here we just keep it opaque
// Define a tiny interface type to hold reference without methods
type ProjectAggregated struct {
	ProjectCtx ruleset.ProjectContext
	// ByScope holds the project-level context of each analyzed source, keyed by
	// its absolute path. A scope without an entry is judged on ProjectCtx.
	ByScope map[string]ruleset.ProjectContext
}

func NewRequirementsEvaluator(requirements configuration.ConfigurationRequirements) *RequirementsEvaluator {
	return &RequirementsEvaluator{Requirements: requirements}
}

// NewScopedRequirementsEvaluator judges every file against the requirements of
// the analyzed source it belongs to. A source holding its own configuration
// file is judged by it alone, and a file belongs to the most specific source
// containing it.
func NewScopedRequirementsEvaluator(config *configuration.Configuration) *RequirementsEvaluator {
	evaluator := &RequirementsEvaluator{}
	if config.Requirements != nil {
		evaluator.Requirements = *config.Requirements
	}

	for _, scope := range config.Scopes {
		if scope.Configuration == nil || scope.Configuration.Requirements == nil {
			continue
		}
		evaluator.scopes = append(evaluator.scopes, scopedRequirements{
			root:         scope.Path,
			requirements: *scope.Configuration.Requirements,
		})
	}

	return evaluator
}

// Rules with boundaries, to evaluate
// Deprecated path-based rules retained for backward compatibility

type BoundariesRule struct {
	Name  string
	Rule  *configuration.ConfigurationDefaultRule
	Label string
}

// parseSeverityFromMessage extracts a leading [Label] severity and returns (severity, cleanedMessage)
func parseSeverityFromMessage(msg string) (Severity, string) {
	trim := strings.TrimSpace(msg)
	sev := SeverityUnknown
	// handle two optional bracketed prefixes (e.g., [Medium][rule])
	re := regexp.MustCompile(`^(\[[^\]]+\])+`)
	if loc := re.FindStringIndex(trim); loc != nil && loc[0] == 0 {
		prefix := trim[loc[0]:loc[1]]
		// Find a severity token inside the brackets
		lower := strings.ToLower(prefix)
		switch {
		case strings.Contains(lower, "[high]") || strings.Contains(lower, "[medium→high]") || strings.Contains(lower, "[medium->high]"):
			sev = SeverityHigh
		case strings.Contains(lower, "[medium]"):
			sev = SeverityMedium
		case strings.Contains(lower, "[low]") || strings.Contains(lower, "[low→medium]") || strings.Contains(lower, "[low->medium]"):
			sev = SeverityLow
		}
		trim = strings.TrimSpace(trim[loc[1]:])
	}
	return sev, trim
}

func (r *RequirementsEvaluator) Evaluate(files []*pb.File, projectAggregated ProjectAggregated) EvaluationResult {
	evaluation := EvaluationResult{
		Files:             files,
		ProjectAggregated: projectAggregated,
		Succeeded:         true,
		Successes:         []RuleOutcome{},
		Errors:            []RuleOutcome{},
	}

	scopes := r.evaluatedScopes()
	filesByScope := groupFilesByScope(scopes, files)

	for index, scope := range scopes {
		context := projectAggregated.ProjectCtx
		if scoped, ok := projectAggregated.ByScope[scope.root]; ok {
			context = scoped
		}
		scope.evaluate(filesByScope[index], context, &evaluation)
	}

	if r.Baseline != nil {
		evaluation.Errors, evaluation.Baselined = r.Baseline.Filter(evaluation.Errors)
	}

	if len(evaluation.Errors) > 0 {
		evaluation.Succeeded = false
	}

	return evaluation
}

// evaluatedScopes returns the requirements to apply. A project without
// sub-configuration is a single scope governing every file.
func (r *RequirementsEvaluator) evaluatedScopes() []scopedRequirements {
	if len(r.scopes) > 0 {
		return r.scopes
	}

	return []scopedRequirements{{requirements: r.Requirements}}
}

func (s scopedRequirements) evaluate(files []*pb.File, context ruleset.ProjectContext, evaluation *EvaluationResult) {
	// Compiled once for the whole scope: the patterns do not depend on the rule
	// being checked, and there are as many checks as rules times files.
	excluded := compileRequirementExcludes(s.requirements.Exclude)
	checked := make([]*pb.File, 0, len(files))
	for _, file := range files {
		if matchesAny(excluded, file.Path) {
			continue
		}
		checked = append(checked, file)
	}

	// Delegate to registry-based rulesets
	requirements := s.requirements
	reg := ruleset.Registry(&requirements)
	for _, rlset := range reg.EnabledRulesets() {
		// File-level rules
		for _, rule := range rlset.Enabled() {
			for _, file := range checked {
				rule := rule // capture
				file := file // capture
				rule.CheckFile(
					file,
					func(err RequirementError) {
						// Severity provided by rule; message should be clean already
						evaluation.Errors = append(evaluation.Errors, RuleOutcome{Severity: err.Severity, Rule: rule.Name(), Message: err.Message, File: file.Path, Line: err.Line})
					},
					func(ok string) {
						sev, msg := parseSeverityFromMessage(ok)
						evaluation.Successes = append(evaluation.Successes, RuleOutcome{Severity: sev, Rule: rule.Name(), Message: msg, File: file.Path})
					},
				)
			}
		}

		// Project-level rules
		if provider, ok := rlset.(ruleset.ProjectRuleProvider); ok {
			for _, rule := range provider.EnabledProjectRules() {
				rule := rule // capture
				rule.CheckProject(
					context,
					func(err RequirementError) {
						evaluation.Errors = append(evaluation.Errors, RuleOutcome{Severity: err.Severity, Rule: rule.Name(), Message: err.Message})
					},
					func(ok string) {
						sev, msg := parseSeverityFromMessage(ok)
						evaluation.Successes = append(evaluation.Successes, RuleOutcome{Severity: sev, Rule: rule.Name(), Message: msg})
					},
				)
			}
		}
	}
}

// compileRequirementExcludes compiles the patterns excluding files from the
// requirement checks. An unusable pattern is reported and dropped, rather than
// taking the whole analysis down.
func compileRequirementExcludes(patterns []string) []*regexp.Regexp {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		expression, err := regexp.Compile(pattern)
		if err != nil {
			logrus.Warnf("requirements exclude pattern %q is not a valid regular expression and is ignored: %v", pattern, err)
			continue
		}
		compiled = append(compiled, expression)
	}

	return compiled
}

func matchesAny(expressions []*regexp.Regexp, value string) bool {
	for _, expression := range expressions {
		if expression.MatchString(value) {
			return true
		}
	}

	return false
}

// groupFilesByScope hands every file to the scope that governs it: the most
// specific analyzed source containing it. The returned slice is indexed like
// scopes, so the evaluation order stays the declaration order whatever the
// nesting is.
func groupFilesByScope(scopes []scopedRequirements, files []*pb.File) [][]*pb.File {
	grouped := make([][]*pb.File, len(scopes))
	if len(scopes) == 1 && scopes[0].root == "" {
		grouped[0] = files
		return grouped
	}

	// From the most specific scope to the least specific one, so that a project
	// nested in another is the one judging its own files.
	order := make([]int, len(scopes))
	for index := range order {
		order[index] = index
	}
	sort.SliceStable(order, func(i, j int) bool {
		return len(scopes[order[i]].root) > len(scopes[order[j]].root)
	})

	workingDirectory, err := os.Getwd()
	if err != nil {
		workingDirectory = ""
	}

	for _, file := range files {
		path := absolutePath(workingDirectory, file.Path)
		for _, index := range order {
			if !containsPath(scopes[index].root, path) {
				continue
			}
			grouped[index] = append(grouped[index], file)
			break
		}
	}

	return grouped
}

// containsPath reports whether path lies inside root. An empty root contains
// every path.
func containsPath(root string, path string) bool {
	if root == "" {
		return true
	}

	return path == root || strings.HasPrefix(path, root+string(filepath.Separator))
}

func absolutePath(workingDirectory string, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}

	return filepath.Join(workingDirectory, path)
}
