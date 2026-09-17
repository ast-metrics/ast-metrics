package ruleset

import (
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer/issue"
	"github.com/ast-metrics/ast-metrics/internal/configuration"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

func TestCouplingRule_Name(t *testing.T) {
	rule := NewCouplingRule(nil)
	if rule.Name() != "coupling" {
		t.Errorf("expected 'coupling', got %s", rule.Name())
	}
}

func TestCouplingRule_Description(t *testing.T) {
	rule := NewCouplingRule(nil)
	expected := "Checks for forbidden coupling between packages"
	if rule.Description() != expected {
		t.Errorf("expected '%s', got %s", expected, rule.Description())
	}
}

func TestCouplingRule_CheckFile_NilConfig(t *testing.T) {
	rule := NewCouplingRule(nil)
	file := &pb.File{
		Path: "/src/controller/UserController.go",
		Stmts: &pb.Stmts{
			StmtExternalDependencies: []*pb.StmtExternalDependency{
				{ClassName: "repository.UserRepository"},
			},
		},
	}

	errors := []issue.RequirementError{}
	successes := []string{}

	rule.CheckFile(file,
		func(e issue.RequirementError) { errors = append(errors, e) },
		func(s string) { successes = append(successes, s) })

	if len(errors) != 0 || len(successes) != 0 {
		t.Error("expected no errors or successes with nil config")
	}
}

func TestCouplingRule_CheckFile_ForbiddenCoupling(t *testing.T) {
	cfg := &configuration.ConfigurationCouplingRule{
		Forbidden: []struct {
			From string `yaml:"from"`
			To   string `yaml:"to"`
		}{
			{From: "Controller", To: "Repository"},
		},
	}
	rule := NewCouplingRule(cfg)

	// Dependencies at class level (as tree-sitter produces them)
	file := &pb.File{
		Path: "/src/controller/UserController.go",
		Stmts: &pb.Stmts{
			StmtClass: []*pb.StmtClass{
				{
					Name: &pb.Name{Short: "UserController"},
					Stmts: &pb.Stmts{
						StmtExternalDependencies: []*pb.StmtExternalDependency{
							{ClassName: "repository.UserRepository"},
						},
					},
				},
			},
		},
	}

	errors := []issue.RequirementError{}
	successes := []string{}

	rule.CheckFile(file,
		func(e issue.RequirementError) { errors = append(errors, e) },
		func(s string) { successes = append(successes, s) })

	if len(errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errors))
	}

	err := errors[0]
	if err.Code != "coupling" {
		t.Errorf("expected 'coupling' code, got %s", err.Code)
	}
	if err.Severity != issue.SeverityUnknown {
		t.Errorf("expected unknown severity, got %s", err.Severity)
	}
}

func TestCouplingRule_CheckFile_ForbiddenCoupling_CaseInsensitive(t *testing.T) {
	cfg := &configuration.ConfigurationCouplingRule{
		Forbidden: []struct {
			From string `yaml:"from"`
			To   string `yaml:"to"`
		}{
			{From: "Service", To: "Controller"},
		},
	}
	rule := NewCouplingRule(cfg)

	// lowercase path and dependency — should still match
	file := &pb.File{
		Path: "/src/service/userservice.go",
		Stmts: &pb.Stmts{
			StmtClass: []*pb.StmtClass{
				{
					Name: &pb.Name{Short: "UserService"},
					Stmts: &pb.Stmts{
						StmtExternalDependencies: []*pb.StmtExternalDependency{
							{ClassName: "controller.UserController"},
						},
					},
				},
			},
		},
	}

	errors := []issue.RequirementError{}
	successes := []string{}

	rule.CheckFile(file,
		func(e issue.RequirementError) { errors = append(errors, e) },
		func(s string) { successes = append(successes, s) })

	if len(errors) != 1 {
		t.Fatalf("expected 1 error (case-insensitive match), got %d", len(errors))
	}
}

func TestCouplingRule_CheckFile_AllowedCoupling(t *testing.T) {
	cfg := &configuration.ConfigurationCouplingRule{
		Forbidden: []struct {
			From string `yaml:"from"`
			To   string `yaml:"to"`
		}{
			{From: "Controller", To: "Repository"},
		},
	}
	rule := NewCouplingRule(cfg)

	file := &pb.File{
		Path: "/src/controller/UserController.go",
		Stmts: &pb.Stmts{
			StmtClass: []*pb.StmtClass{
				{
					Name: &pb.Name{Short: "UserController"},
					Stmts: &pb.Stmts{
						StmtExternalDependencies: []*pb.StmtExternalDependency{
							{ClassName: "service.UserService"},
						},
					},
				},
			},
		},
	}

	errors := []issue.RequirementError{}
	successes := []string{}

	rule.CheckFile(file,
		func(e issue.RequirementError) { errors = append(errors, e) },
		func(s string) { successes = append(successes, s) })

	if len(errors) != 0 {
		t.Errorf("expected no errors, got %d", len(errors))
	}
	if len(successes) != 1 {
		t.Fatalf("expected 1 success, got %d", len(successes))
	}
	if successes[0] != "Coupling OK" {
		t.Errorf("unexpected success message: %s", successes[0])
	}
}

func TestCouplingRule_CheckFile_NoMatchingFromPattern(t *testing.T) {
	cfg := &configuration.ConfigurationCouplingRule{
		Forbidden: []struct {
			From string `yaml:"from"`
			To   string `yaml:"to"`
		}{
			{From: "Controller", To: "Repository"},
		},
	}
	rule := NewCouplingRule(cfg)

	file := &pb.File{
		Path: "/src/service/UserService.go",
		Stmts: &pb.Stmts{
			StmtClass: []*pb.StmtClass{
				{
					Name: &pb.Name{Short: "UserService"},
					Stmts: &pb.Stmts{
						StmtExternalDependencies: []*pb.StmtExternalDependency{
							{ClassName: "repository.UserRepository"},
						},
					},
				},
			},
		},
	}

	errors := []issue.RequirementError{}
	successes := []string{}

	rule.CheckFile(file,
		func(e issue.RequirementError) { errors = append(errors, e) },
		func(s string) { successes = append(successes, s) })

	if len(errors) != 0 {
		t.Errorf("expected no errors when from pattern doesn't match, got %d", len(errors))
	}
	if len(successes) != 1 {
		t.Fatalf("expected 1 success, got %d", len(successes))
	}
}

func TestCouplingRule_CheckFile_MultipleForbiddenRules(t *testing.T) {
	cfg := &configuration.ConfigurationCouplingRule{
		Forbidden: []struct {
			From string `yaml:"from"`
			To   string `yaml:"to"`
		}{
			{From: "Controller", To: "Repository"},
			{From: "Controller", To: "Database"},
		},
	}
	rule := NewCouplingRule(cfg)

	file := &pb.File{
		Path: "/src/controller/UserController.go",
		Stmts: &pb.Stmts{
			StmtClass: []*pb.StmtClass{
				{
					Name: &pb.Name{Short: "UserController"},
					Stmts: &pb.Stmts{
						StmtExternalDependencies: []*pb.StmtExternalDependency{
							{ClassName: "repository.UserRepository"},
							{ClassName: "database.Connection"},
						},
					},
				},
			},
		},
	}

	errors := []issue.RequirementError{}
	successes := []string{}

	rule.CheckFile(file,
		func(e issue.RequirementError) { errors = append(errors, e) },
		func(s string) { successes = append(successes, s) })

	// Both forbidden rules match: Repository and Database
	if len(errors) != 2 {
		t.Fatalf("expected 2 errors (one per forbidden rule), got %d", len(errors))
	}
	if len(successes) != 0 {
		t.Errorf("expected no successes when violations found, got %d", len(successes))
	}
}

func TestCouplingRule_CheckFile_DepsAtNamespaceLevel(t *testing.T) {
	cfg := &configuration.ConfigurationCouplingRule{
		Forbidden: []struct {
			From string `yaml:"from"`
			To   string `yaml:"to"`
		}{
			{From: "Controller", To: "Repository"},
		},
	}
	rule := NewCouplingRule(cfg)

	// Dependencies at namespace level (as Go/Python parsers produce them)
	file := &pb.File{
		Path: "/src/controller/UserController.go",
		Stmts: &pb.Stmts{
			StmtNamespace: []*pb.StmtNamespace{
				{
					Name: &pb.Name{Short: "controller"},
					Stmts: &pb.Stmts{
						StmtExternalDependencies: []*pb.StmtExternalDependency{
							{ClassName: "UserRepository", Namespace: "repository"},
						},
					},
				},
			},
		},
	}

	errors := []issue.RequirementError{}
	successes := []string{}

	rule.CheckFile(file,
		func(e issue.RequirementError) { errors = append(errors, e) },
		func(s string) { successes = append(successes, s) })

	if len(errors) != 1 {
		t.Fatalf("expected 1 error for namespace-level dependency, got %d", len(errors))
	}
}

func TestCouplingRule_CheckFile_ForbidsAPackage(t *testing.T) {
	cfg := &configuration.ConfigurationCouplingRule{
		Forbidden: []struct {
			From string `yaml:"from"`
			To   string `yaml:"to"`
		}{
			{From: "Controller", To: `org\.apache\.logging`},
		},
	}
	rule := NewCouplingRule(cfg)

	// An import is recorded as a package and a simple name, apart: the rule
	// has to see the package.
	file := &pb.File{
		Path: "/src/controller/UserController.java",
		Stmts: &pb.Stmts{
			StmtExternalDependencies: []*pb.StmtExternalDependency{
				{ClassName: "Logger", Namespace: "org.apache.logging.log4j"},
			},
		},
	}

	errors := []issue.RequirementError{}
	rule.CheckFile(file,
		func(e issue.RequirementError) { errors = append(errors, e) },
		func(string) {})

	if len(errors) != 1 {
		t.Fatalf("expected 1 error for a forbidden package, got %d", len(errors))
	}
	if want := "Forbidden coupling between /src/controller/UserController.java and org.apache.logging.log4j"; errors[0].Message != want {
		t.Errorf("message: got %q, want %q", errors[0].Message, want)
	}
}
