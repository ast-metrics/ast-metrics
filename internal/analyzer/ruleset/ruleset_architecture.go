package ruleset

import (
	"github.com/ast-metrics/ast-metrics/internal/configuration"
)

// Architecture ruleset
type architectureRuleset struct {
	cfg *configuration.ConfigurationRequirements
}

func (a *architectureRuleset) Category() string {
	return "architecture"
}
func (a *architectureRuleset) Description() string {
	return "Architecture-related constraints (e.g., coupling)"
}
func (a *architectureRuleset) Enabled() []Rule {
	rules := []Rule{}
	if a == nil || a.cfg == nil || a.cfg.Rules == nil || a.cfg.Rules.Architecture == nil {
		return rules
	}
	arch := a.cfg.Rules.Architecture
	if arch.Coupling != nil {
		rules = append(rules, NewCouplingRule(arch.Coupling))
	}
	if arch.AfferentCoupling != nil {
		rules = append(rules, NewAfferentCouplingRule(arch.AfferentCoupling))
	}
	if arch.EfferentCoupling != nil {
		rules = append(rules, NewEfferentCouplingRule(arch.EfferentCoupling))
	}
	if arch.Maintainability != nil {
		rules = append(rules, NewMaintainabilityRule(arch.Maintainability))
	}
	if arch.NoCircularDependencies != nil {
		rules = append(rules, NewNoCircularDependenciesRule(arch.NoCircularDependencies))
	}
	if arch.MaxResponsibilities != nil {
		rules = append(rules, NewMaxResponsibilitiesRule(arch.MaxResponsibilities))
	}
	if arch.NoGodClass != nil {
		rules = append(rules, NewNoGodClassRule(arch.NoGodClass))
	}
	return rules
}

func (a *architectureRuleset) All() []Rule {
	var coupling *configuration.ConfigurationCouplingRule
	var afferent *int
	var efferent *int
	var maintainability *int
	var noCircularDependencies *bool
	var maxResponsibilities *int
	var noGodClass *bool
	if a != nil && a.cfg != nil && a.cfg.Rules != nil && a.cfg.Rules.Architecture != nil {
		arch := a.cfg.Rules.Architecture
		coupling = arch.Coupling
		afferent = arch.AfferentCoupling
		efferent = arch.EfferentCoupling
		maintainability = arch.Maintainability
		noCircularDependencies = arch.NoCircularDependencies
		maxResponsibilities = arch.MaxResponsibilities
		noGodClass = arch.NoGodClass
	}
	return []Rule{
		NewCouplingRule(coupling),
		NewAfferentCouplingRule(afferent),
		NewEfferentCouplingRule(efferent),
		NewMaintainabilityRule(maintainability),
		NewNoCircularDependenciesRule(noCircularDependencies),
		NewMaxResponsibilitiesRule(maxResponsibilities),
		NewNoGodClassRule(noGodClass),
	}
}

func (a *architectureRuleset) IsEnabled() bool {
	return len(a.Enabled()) > 0 || len(a.EnabledProjectRules()) > 0
}

// AllProjectRules returns the project-level rules of the ruleset, whatever
// the configuration: the ones reading the communities.
func (a *architectureRuleset) AllProjectRules() []ProjectRule {
	return []ProjectRule{
		NewNoCommunityCyclesRule(nil),
		NewMaxCommunityCrossShareRule(nil),
		NewNoCrossCommunityDependenciesRule(nil),
	}
}

// EnabledProjectRules returns the project-level rules the configuration turns on.
func (a *architectureRuleset) EnabledProjectRules() []ProjectRule {
	rules := []ProjectRule{}
	if a == nil || a.cfg == nil || a.cfg.Rules == nil || a.cfg.Rules.Architecture == nil {
		return rules
	}
	arch := a.cfg.Rules.Architecture
	if arch.NoCommunityCycles != nil && *arch.NoCommunityCycles {
		rules = append(rules, NewNoCommunityCyclesRule(arch.NoCommunityCycles))
	}
	if arch.MaxCommunityCrossShare != nil {
		rules = append(rules, NewMaxCommunityCrossShareRule(arch.MaxCommunityCrossShare))
	}
	if arch.NoCrossCommunityDependencies != nil && *arch.NoCrossCommunityDependencies {
		rules = append(rules, NewNoCrossCommunityDependenciesRule(arch.NoCrossCommunityDependencies))
	}
	return rules
}
