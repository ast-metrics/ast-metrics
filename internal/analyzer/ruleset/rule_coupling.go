package ruleset

import (
	"fmt"
	"regexp"

	"github.com/ast-metrics/ast-metrics/internal/analyzer/issue"
	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

type couplingRule struct {
	cfg *configuration.ConfigurationCouplingRule
}

func NewCouplingRule(c *configuration.ConfigurationCouplingRule) Rule {
	return &couplingRule{cfg: c}
}

func (c *couplingRule) Name() string {
	return "coupling"
}

func (c *couplingRule) Description() string {
	return "Checks for forbidden coupling between packages"
}

func (c *couplingRule) CheckFile(file *pb.File, addError func(issue.RequirementError), addSuccess func(string)) {
	if c.cfg == nil || file.Stmts == nil {
		return
	}

	// Aggregate dependencies from all levels (file, namespace, class, function)
	dependencies := engine.GetDependenciesInFile(file)
	if len(dependencies) == 0 {
		return
	}

	// External dependencies carry no line of their own; anchor the violation
	// to the offending class when the file exposes one.
	line := 0
	if classes := engine.GetClassesInFile(file); len(classes) > 0 {
		line = lineOf(classes[0].GetLocation())
	}

	hasError := false
	for _, forbidden := range c.cfg.Forbidden {
		fromRegex := regexp.MustCompile("(?i)" + forbidden.From)
		if !fromRegex.MatchString(file.Path) {
			continue
		}
		toRegex := regexp.MustCompile("(?i)" + forbidden.To)
		for _, dependency := range dependencies {
			name, matched := forbiddenName(toRegex, dependency)
			if !matched {
				continue
			}
			addError(issue.RequirementError{
				Severity: issue.SeverityUnknown,
				Code:     c.Name(),
				Message:  fmt.Sprintf("Forbidden coupling between %s and %s", file.Path, name),
				Line:     line,
			})
			hasError = true
			break
		}
	}

	if !hasError {
		addSuccess("Coupling OK")
	}
}

// forbiddenName tells whether a dependency is one the rule forbids, and under
// which name. A rule may name a class ("UserRepository") or a package
// ("org\\.apache\\.logging"): an engine records the two apart, so the
// pattern is tried on the class name first, then on the namespace.
func forbiddenName(pattern *regexp.Regexp, dependency *pb.StmtExternalDependency) (string, bool) {
	if className := dependency.GetClassName(); className != "" && pattern.MatchString(className) {
		return className, true
	}
	if namespace := dependency.GetNamespace(); namespace != "" && pattern.MatchString(namespace) {
		return namespace, true
	}
	return "", false
}
