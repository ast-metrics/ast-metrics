package ruleset

import (
	"github.com/ast-metrics/ast-metrics/internal/analyzer/issue"
)

// ProjectContext carries aggregated project-level data for project rules.
// Defined here (not in analyzer) to avoid import cycles.
type ProjectContext struct {
	TraceabilityPct      float64
	GlobalIsolationScore float64
	GodTests             []GodTestInfo
	OrphanClasses        []OrphanClassInfo
	// Communities is what the community analysis found, nil when it did
	// not run.
	Communities *CommunitiesInfo
}

// GodTestInfo describes a test file with excessive fan-out.
type GodTestInfo struct {
	FilePath string
	FanOut   int
}

// OrphanClassInfo describes a production class with no test coverage.
type OrphanClassInfo struct {
	ClassName string
	FilePath  string
	Weight    float64
}

// ProjectRule checks project-level aggregated metrics (as opposed to per-file Rule).
type ProjectRule interface {
	Name() string
	Description() string
	CheckProject(ctx ProjectContext, addError func(issue.RequirementError), addSuccess func(string))
}

// ProjectRuleProvider is implemented by rulesets that provide project-level rules.
type ProjectRuleProvider interface {
	AllProjectRules() []ProjectRule
	EnabledProjectRules() []ProjectRule
}
