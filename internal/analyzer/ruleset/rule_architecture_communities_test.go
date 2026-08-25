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

func TestNoCrossCommunityDependenciesRuleReportsEachCrossingInOrder(t *testing.T) {
	enabled := true
	rule := NewNoCrossCommunityDependenciesRule(&enabled)
	ctx := ProjectContext{Communities: &CommunitiesInfo{Count: 2, CrossDependencies: []CrossDependencyInfo{
		{File: "src/Billing/B.php", From: "B", To: "C"},
		{File: "src/Billing/A.php", From: "A", To: "D"},
	}}}
	errors, successes := collectProject(rule, ctx)
	if len(successes) != 0 || len(errors) != 2 {
		t.Fatalf("expected two errors and no success, got %d/%d", len(errors), len(successes))
	}
	if errors[0].File != "src/Billing/A.php" || errors[1].File != "src/Billing/B.php" {
		t.Errorf("errors should be sorted by file: %+v", errors)
	}
	if errors[0].Code != "no_cross_community_dependencies" || errors[0].Severity != issue.SeverityLow {
		t.Errorf("unexpected code or severity: %+v", errors[0])
	}
	if errors[0].Message != "A depends on D, which sits in another community" {
		t.Errorf("unexpected message: %s", errors[0].Message)
	}
}

func TestNoCrossCommunityDependenciesRuleIsQuietWithoutCrossingsOrWhenDisabled(t *testing.T) {
	enabled := true
	errors, successes := collectProject(NewNoCrossCommunityDependenciesRule(&enabled), ProjectContext{Communities: &CommunitiesInfo{Count: 3}})
	if len(errors) != 0 || len(successes) != 1 {
		t.Errorf("no crossing: expected one success, got %d errors, %d successes", len(errors), len(successes))
	}
	errors, successes = collectProject(NewNoCrossCommunityDependenciesRule(&enabled), ProjectContext{})
	if len(errors) != 0 || len(successes) != 1 {
		t.Errorf("no analysis: expected one success, got %d errors, %d successes", len(errors), len(successes))
	}
	crossing := []CrossDependencyInfo{{File: "a.php", From: "A", To: "B"}}
	errors, successes = collectProject(NewNoCrossCommunityDependenciesRule(nil), ProjectContext{Communities: &CommunitiesInfo{Count: 2, CrossDependencies: crossing}})
	if len(errors) != 0 || len(successes) != 0 {
		t.Errorf("disabled rule should stay silent, got %d errors, %d successes", len(errors), len(successes))
	}
}
