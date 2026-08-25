package report

import (
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/ast-metrics/ast-metrics/internal/analyzer/requirement"
)

func TestCommunityFreezeReadsTheRuleOffTheEvaluation(t *testing.T) {
	if got := communityFreezeOf(nil); got.Enabled {
		t.Errorf("no evaluation, no freeze: %+v", got)
	}
	silent := &requirement.EvaluationResult{Successes: []requirement.RuleOutcome{{Rule: "max_lloc"}}}
	if got := communityFreezeOf(silent); got.Enabled {
		t.Errorf("the rule did not run: %+v", got)
	}
	frozen := &requirement.EvaluationResult{
		Errors:          []requirement.RuleOutcome{{Rule: crossCommunityRule, Message: "A depends on B"}, {Rule: "max_lloc"}},
		BaselinedByRule: map[string]int{crossCommunityRule: 12},
	}
	got := communityFreezeOf(frozen)
	if !got.Enabled || got.Baselined != 12 || got.Fresh != 1 {
		t.Errorf("expected 12 frozen and 1 new, got %+v", got)
	}
	clean := &requirement.EvaluationResult{Successes: []requirement.RuleOutcome{{Rule: crossCommunityRule}}}
	if got := communityFreezeOf(clean); !got.Enabled || got.Fresh != 0 || got.Baselined != 0 {
		t.Errorf("a success enables the rule with nothing to report: %+v", got)
	}
}

func TestUnitLabelsUsesShortLabels(t *testing.T) {
	cm := &analyzer.CommunityMetrics{Labels: map[string]string{`App\A`: "A", `App\B`: "B"}}
	if got := unitLabels(cm, []string{`App\A`, `App\B`, `App\C`}, 2); got != "A, B" {
		t.Errorf("got %q", got)
	}
	if got := unitLabels(cm, []string{`App\C`}, 5); got != `App\C` {
		t.Errorf("an unknown unit keeps its id, got %q", got)
	}
}
