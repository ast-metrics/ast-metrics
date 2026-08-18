package report

import (
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/ast-metrics/ast-metrics/internal/analyzer/requirement"
)

// crossCommunityRule is the name of the project rule freezing the boundaries
// between communities: `ast-metrics baseline` accepts the crossings of the
// day, `ast-metrics lint` fails on the ones added since.
const crossCommunityRule = "no_cross_community_dependencies"

// communityFreeze is what the communities page says of that rule.
type communityFreeze struct {
	// Enabled is true when the rule ran: it left an outcome, or the baseline
	// silenced some of its errors.
	Enabled bool
	// Baselined counts the crossings frozen in the baseline, Fresh the ones
	// found since.
	Baselined int
	Fresh     int
}

// communityFreezeOf reads the state of the rule off the evaluation of the
// project, nil when no requirement was evaluated.
func communityFreezeOf(eval *requirement.EvaluationResult) communityFreeze {
	out := communityFreeze{}
	if eval == nil {
		return out
	}
	for _, e := range eval.Errors {
		if e.Rule == crossCommunityRule {
			out.Enabled = true
			out.Fresh++
		}
	}
	if !out.Enabled {
		for _, s := range eval.Successes {
			if s.Rule == crossCommunityRule {
				out.Enabled = true
				break
			}
		}
	}
	if n := eval.BaselinedByRule[crossCommunityRule]; n > 0 {
		out.Enabled = true
		out.Baselined = n
	}
	return out
}

// unitLabels turns unit ids into their short labels, the first n of them,
// joined for a tooltip. Ids without a label are shown as they are.
func unitLabels(cm *analyzer.CommunityMetrics, units []string, n int) string {
	if len(units) > n {
		units = units[:n]
	}
	labels := make([]string, 0, len(units))
	for _, u := range units {
		if l := cm.Labels[u]; l != "" {
			labels = append(labels, l)
		} else {
			labels = append(labels, u)
		}
	}
	return strings.Join(labels, ", ")
}
