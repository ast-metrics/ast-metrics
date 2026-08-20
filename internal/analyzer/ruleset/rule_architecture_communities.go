package ruleset

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/analyzer/issue"
)

// CommunitiesInfo carries what the community analysis found, for the project
// rules that read it. Defined here, not in analyzer, to avoid import cycles.
type CommunitiesInfo struct {
	// Count is the number of communities, the shared kernel left out.
	Count int
	// Cycles lists the groups of communities depending on each other, each
	// as the names of its members.
	Cycles [][]string
	// CrossSharePct is the share, in percent, of the dependencies crossing
	// from one community to another, the shared kernel left aside.
	CrossSharePct float64
	// BackEdges name the dependencies to cut to break the cycles, as
	// "A → B (n references)", lightest first.
	BackEdges []string
	// CrossDependencies lists every reference from a unit of one community
	// to a unit of another, the shared kernel left aside.
	CrossDependencies []CrossDependencyInfo
}

// CrossDependencyInfo is one dependency crossing from a community to another:
// From, in File, depends on To, which sits in another community. Names are
// labels (a class name or a namespace), not ids, and File is relative to the
// working directory: the baseline recognizes an entry by rule, file and
// message, so only stable text may reach the message.
type CrossDependencyInfo struct {
	File string
	From string
	To   string
}

// noCommunityCyclesRule fails when communities depend on each other.
type noCommunityCyclesRule struct {
	enabled bool
}

// NewNoCommunityCyclesRule builds the rule; nil or false leaves it disabled.
func NewNoCommunityCyclesRule(enabled *bool) ProjectRule {
	return &noCommunityCyclesRule{enabled: enabled != nil && *enabled}
}

func (r *noCommunityCyclesRule) Name() string { return "no_community_cycles" }

func (r *noCommunityCyclesRule) Description() string {
	return "Fails when communities (groups of classes detected on the dependency graph) depend on each other in a cycle"
}

func (r *noCommunityCyclesRule) CheckProject(ctx ProjectContext, addError func(issue.RequirementError), addSuccess func(string)) {
	if !r.enabled {
		return
	}
	if ctx.Communities == nil || ctx.Communities.Count < 2 {
		addSuccess("No community cycle: fewer than two communities")
		return
	}
	if len(ctx.Communities.Cycles) == 0 {
		addSuccess("No cycle between communities")
		return
	}
	for _, cycle := range ctx.Communities.Cycles {
		cuts := ""
		if len(ctx.Communities.BackEdges) > 0 {
			shown := ctx.Communities.BackEdges
			if len(shown) > 3 {
				shown = shown[:3]
			}
			cuts = "; to cut: " + strings.Join(shown, ", ")
		}
		addError(issue.RequirementError{
			Severity: issue.SeverityMedium,
			Message:  fmt.Sprintf("Communities depend on each other: %s%s", strings.Join(cycle, ", "), cuts),
			Code:     r.Name(),
		})
	}
}

// maxCommunityCrossShareRule fails when too many dependencies cross from one
// community to another.
type maxCommunityCrossShareRule struct {
	max *int
}

// NewMaxCommunityCrossShareRule builds the rule with a threshold in percent.
func NewMaxCommunityCrossShareRule(max *int) ProjectRule {
	return &maxCommunityCrossShareRule{max: max}
}

func (r *maxCommunityCrossShareRule) Name() string { return "max_community_cross_share" }

func (r *maxCommunityCrossShareRule) Description() string {
	return "Checks that the share of dependencies crossing from one community to another (in percent, the shared kernel left aside) stays under a maximum"
}

func (r *maxCommunityCrossShareRule) CheckProject(ctx ProjectContext, addError func(issue.RequirementError), addSuccess func(string)) {
	if r.max == nil {
		return
	}
	if ctx.Communities == nil || ctx.Communities.Count < 2 {
		addSuccess("Community cross share: fewer than two communities")
		return
	}
	if ctx.Communities.CrossSharePct > float64(*r.max) {
		addError(issue.RequirementError{
			Severity: issue.SeverityMedium,
			Message:  fmt.Sprintf("Too many dependencies cross between communities: %.0f%% (max: %d%%)", ctx.Communities.CrossSharePct, *r.max),
			Code:     r.Name(),
		})
		return
	}
	addSuccess(fmt.Sprintf("Community cross share OK: %.0f%%", ctx.Communities.CrossSharePct))
}

// noCrossCommunityDependenciesRule fails on every dependency crossing from one
// community to another. The intent is to freeze the architecture the community
// analysis found: the team runs `ast-metrics baseline` once to accept the
// current crossings, and only a new one fails afterwards.
type noCrossCommunityDependenciesRule struct {
	enabled bool
}

// NewNoCrossCommunityDependenciesRule builds the rule; nil or false leaves it disabled.
func NewNoCrossCommunityDependenciesRule(enabled *bool) ProjectRule {
	return &noCrossCommunityDependenciesRule{enabled: enabled != nil && *enabled}
}

func (r *noCrossCommunityDependenciesRule) Name() string {
	return "no_cross_community_dependencies"
}

func (r *noCrossCommunityDependenciesRule) Description() string {
	return "Fails on every dependency crossing from one community to another (the shared kernel left aside); freeze the current ones with `ast-metrics baseline` and only new crossings fail afterwards"
}

func (r *noCrossCommunityDependenciesRule) CheckProject(ctx ProjectContext, addError func(issue.RequirementError), addSuccess func(string)) {
	if !r.enabled {
		return
	}
	if ctx.Communities == nil || ctx.Communities.Count < 2 {
		addSuccess("No cross-community dependency: fewer than two communities")
		return
	}
	if len(ctx.Communities.CrossDependencies) == 0 {
		addSuccess("No dependency crosses from one community to another")
		return
	}
	// The order decides the order of the baseline entries and of the lint
	// output, so it has to be the same from one run to the next.
	deps := make([]CrossDependencyInfo, len(ctx.Communities.CrossDependencies))
	copy(deps, ctx.Communities.CrossDependencies)
	sort.SliceStable(deps, func(i, j int) bool {
		if deps[i].File != deps[j].File {
			return deps[i].File < deps[j].File
		}
		return crossDependencyMessage(deps[i]) < crossDependencyMessage(deps[j])
	})
	for _, dep := range deps {
		addError(issue.RequirementError{
			Severity: issue.SeverityLow,
			Message:  crossDependencyMessage(dep),
			Code:     r.Name(),
			File:     dep.File,
		})
	}
}

// crossDependencyMessage words one crossing. Community ids, weights and even
// the name of the community stay out of it on purpose: they change from one
// run to the next (a community is renamed as soon as its hub moves) and
// would break the baseline. The two class names are what identifies a
// crossing; the community is only told to the reader, in the file column.
func crossDependencyMessage(dep CrossDependencyInfo) string {
	return fmt.Sprintf("%s depends on %s, which sits in another community", dep.From, dep.To)
}
