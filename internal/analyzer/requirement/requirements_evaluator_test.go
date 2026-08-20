package requirement

import (
	"fmt"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer/ruleset"
	"github.com/ast-metrics/ast-metrics/internal/configuration"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/stretchr/testify/assert"
)

func TestEvaluationResult(t *testing.T) {

	ccn5 := int32(5)
	ccn10 := int32(10)
	files := []*pb.File{
		{
			Path: "test1.go",
			Stmts: &pb.Stmts{
				Analyze: &pb.Analyze{
					Complexity: &pb.Complexity{
						Cyclomatic: &ccn10,
					},
				},
			},
		},
		{
			Path: "test2.go",
			Stmts: &pb.Stmts{
				Analyze: &pb.Analyze{
					Complexity: &pb.Complexity{
						Cyclomatic: &ccn5,
					},
				},
			},
		},
	}

	configInYaml := `
requirements:
  rules:
    complexity:
      max_cyclomatic: 5`

	loader := configuration.NewConfigurationLoader()
	config, err := loader.Import(configInYaml)
	assert.Nil(t, err)
	fmt.Println(config)

	evaluator := NewRequirementsEvaluator(*config.Requirements)
	evaluation := evaluator.Evaluate(files, ProjectAggregated{})

	assert.Equal(t, files, evaluation.Files)
	assert.Equal(t, false, evaluation.Succeeded)

	assert.Equal(t, 1, len(evaluation.Errors))
	assert.Equal(t, "Cyclomatic complexity too high: got 10 (max: 5)", evaluation.Errors[0].Message)
	assert.Equal(t, "cyclomatic_complexity", evaluation.Errors[0].Rule)
	assert.Equal(t, "test1.go", evaluation.Errors[0].File)
}

func TestEvaluationResultSuccess(t *testing.T) {

	ccn5 := int32(5)
	ccn10 := int32(10)
	files := []*pb.File{
		{
			Path: "test1.go",
			Stmts: &pb.Stmts{
				Analyze: &pb.Analyze{
					Complexity: &pb.Complexity{
						Cyclomatic: &ccn10,
					},
				},
			},
		},
		{
			Path: "test2.go",
			Stmts: &pb.Stmts{
				Analyze: &pb.Analyze{
					Complexity: &pb.Complexity{
						Cyclomatic: &ccn5,
					},
				},
			},
		},
	}

	configInYaml := `
requirements:
  rules:
    max_cyclomatic: 15
`

	loader := configuration.NewConfigurationLoader()
	configuration, err := loader.Import(configInYaml)
	assert.Nil(t, err)

	evaluator := NewRequirementsEvaluator(*configuration.Requirements)
	evaluation := evaluator.Evaluate(files, ProjectAggregated{})

	assert.Equal(t, files, evaluation.Files)
	assert.Equal(t, true, evaluation.Succeeded)

	assert.Equal(t, 0, len(evaluation.Errors))
}

// A project rule naming a file must reach the outcomes with that file, and
// the baseline must recognize it from one run to the next: freezing the
// crossings between communities relies on both.
func TestEvaluateProjectRuleCarriesTheFileAndSurvivesTheBaseline(t *testing.T) {
	enabled := true
	requirements := configuration.ConfigurationRequirements{
		Rules: &configuration.ConfigurationRequirementsRules{
			Architecture: &configuration.ConfigurationArchitectureRules{NoCrossCommunityDependencies: &enabled},
		},
	}
	crossings := func(deps ...ruleset.CrossDependencyInfo) ProjectAggregated {
		return ProjectAggregated{ProjectCtx: ruleset.ProjectContext{Communities: &ruleset.CommunitiesInfo{Count: 2, CrossDependencies: deps}}}
	}
	first := ruleset.CrossDependencyInfo{File: "src/Billing/A.php", From: "A", To: "D"}
	second := ruleset.CrossDependencyInfo{File: "src/Billing/B.php", From: "B", To: "C"}

	evaluator := NewRequirementsEvaluator(requirements)
	before := evaluator.Evaluate(nil, crossings(first))
	assert.Equal(t, 1, len(before.Errors))
	assert.Equal(t, "no_cross_community_dependencies", before.Errors[0].Rule)
	assert.Equal(t, "src/Billing/A.php", before.Errors[0].File)
	assert.Nil(t, before.BaselinedByRule)

	evaluator.Baseline = NewBaselineFromOutcomes(before.Errors)
	frozen := evaluator.Evaluate(nil, crossings(first))
	assert.Equal(t, 0, len(frozen.Errors))
	assert.True(t, frozen.Succeeded)
	assert.Equal(t, 1, frozen.Baselined)
	assert.Equal(t, map[string]int{"no_cross_community_dependencies": 1}, frozen.BaselinedByRule)

	after := evaluator.Evaluate(nil, crossings(first, second))
	assert.Equal(t, 1, len(after.Errors))
	assert.Equal(t, "src/Billing/B.php", after.Errors[0].File)
	assert.Equal(t, 1, after.BaselinedByRule["no_cross_community_dependencies"])
}
