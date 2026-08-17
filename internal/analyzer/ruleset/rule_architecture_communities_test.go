package ruleset

import (
	"strings"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer/issue"
)

func collectProject(rule ProjectRule, ctx ProjectContext) (errors []issue.RequirementError, successes []string) {
	rule.CheckProject(ctx,
		func(e issue.RequirementError) { errors = append(errors, e) },
		func(s string) { successes = append(successes, s) })
	return
}

func TestNoCommunityCyclesRuleReportsEachCycleWithTheCuts(t *testing.T) {
	enabled := true
	rule := NewNoCommunityCyclesRule(&enabled)
	ctx := ProjectContext{Communities: &CommunitiesInfo{
		Count:     4,
		Cycles:    [][]string{{"Billing", "Catalog"}},
		BackEdges: []string{"Catalog → Billing (1)"},
	}}
	errors, _ := collectProject(rule, ctx)
	if len(errors) != 1 {
		t.Fatalf("expected one error, got %d", len(errors))
	}
	if !strings.Contains(errors[0].Message, "Billing, Catalog") || !strings.Contains(errors[0].Message, "Catalog → Billing (1)") {
		t.Errorf("message should name the cycle and the cut: %s", errors[0].Message)
	}
	if errors[0].Code != "no_community_cycles" {
		t.Errorf("code = %s", errors[0].Code)
	}
}

func TestNoCommunityCyclesRuleIsQuietWithoutCyclesOrWhenDisabled(t *testing.T) {
	enabled := true
	errors, successes := collectProject(NewNoCommunityCyclesRule(&enabled), ProjectContext{Communities: &CommunitiesInfo{Count: 3}})
	if len(errors) != 0 || len(successes) != 1 {
		t.Errorf("no cycle should be a success, got %v / %v", errors, successes)
	}
	errors, successes = collectProject(NewNoCommunityCyclesRule(nil), ProjectContext{Communities: &CommunitiesInfo{Count: 3, Cycles: [][]string{{"A", "B"}}}})
	if len(errors) != 0 || len(successes) != 0 {
		t.Errorf("a disabled rule says nothing, got %v / %v", errors, successes)
	}
	errors, _ = collectProject(NewNoCommunityCyclesRule(&enabled), ProjectContext{})
	if len(errors) != 0 {
		t.Errorf("no community analysis, no error")
	}
}

func TestMaxCommunityCrossShareRule(t *testing.T) {
	max := 20
	rule := NewMaxCommunityCrossShareRule(&max)
	errors, _ := collectProject(rule, ProjectContext{Communities: &CommunitiesInfo{Count: 5, CrossSharePct: 31.4}})
	if len(errors) != 1 || !strings.Contains(errors[0].Message, "31% (max: 20%)") {
		t.Errorf("expected a failure at 31%%, got %v", errors)
	}
	errors, successes := collectProject(rule, ProjectContext{Communities: &CommunitiesInfo{Count: 5, CrossSharePct: 12}})
	if len(errors) != 0 || len(successes) != 1 {
		t.Errorf("12%% is under the maximum, got %v / %v", errors, successes)
	}
	errors, _ = collectProject(NewMaxCommunityCrossShareRule(nil), ProjectContext{Communities: &CommunitiesInfo{Count: 5, CrossSharePct: 90}})
	if len(errors) != 0 {
		t.Errorf("no threshold, no error")
	}
}
