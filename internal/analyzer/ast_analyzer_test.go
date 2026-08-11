package analyzer

import (
	"testing"

	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/stretchr/testify/assert"
)

func TestAnalyzeFiles(t *testing.T) {
	protoFile := &pb.File{
		ProgrammingLanguage: "Go",
		Stmts: &pb.Stmts{
			StmtFunction: []*pb.StmtFunction{
				{Stmts: &pb.Stmts{}},
			},
			StmtClass: []*pb.StmtClass{
				{Stmts: &pb.Stmts{}},
				{Stmts: &pb.Stmts{}},
				{Stmts: &pb.Stmts{Analyze: &pb.Analyze{}}},
				{Stmts: &pb.Stmts{Analyze: &pb.Analyze{}}},
			},
		},
	}

	// Analyze in-memory
	results := AnalyzeFiles([]*pb.File{protoFile}, nil)

	assert.Equal(t, 1, len(results))
	assert.Equal(t, "Go", results[0].ProgrammingLanguage)
	ccn := results[0].Stmts.Analyze.Complexity.Cyclomatic
	assert.NotNil(t, ccn)
	// four empty classes worth nothing, and one empty function worth its
	// entry path
	assert.Equal(t, 1, int(*ccn))
}

// The engines attach a class both to the namespace of the file and to the
// scope that declares it, and a method to its class. These fixtures reproduce
// that shape: the file total must follow the structure, not the flat lists,
// or a nested scope gets counted twice.

func TestRecomputeFileCyclomatic_SumsClassesAndFunctionsOutsideClasses(t *testing.T) {
	method := &pb.StmtFunction{
		Name:  &pb.Name{Short: "M", Qualified: "Acme\\C::M"},
		Stmts: &pb.Stmts{StmtDecisionIf: []*pb.StmtDecisionIf{{}, {}}},
	}
	class := &pb.StmtClass{
		Name:  &pb.Name{Short: "C", Qualified: "Acme\\C"},
		Stmts: &pb.Stmts{StmtFunction: []*pb.StmtFunction{method}},
	}
	outsideFn := &pb.StmtFunction{
		Name:  &pb.Name{Short: "F", Qualified: "Acme\\F"},
		Stmts: &pb.Stmts{StmtDecisionIf: []*pb.StmtDecisionIf{{}}},
	}

	file := &pb.File{
		Stmts: &pb.Stmts{
			StmtClass:    []*pb.StmtClass{class},
			StmtFunction: []*pb.StmtFunction{outsideFn},
			StmtNamespace: []*pb.StmtNamespace{
				{Stmts: &pb.Stmts{
					StmtClass:    []*pb.StmtClass{class},
					StmtFunction: []*pb.StmtFunction{method, outsideFn},
				}},
			},
		},
	}

	recomputeFileCyclomatic(file)

	if file.Stmts.Analyze.GetComplexity().Cyclomatic == nil {
		t.Fatalf("expected file cyclomatic complexity to be set")
	}
	// method (1 + 2 if) + outside function (1 + 1 if)
	assert.Equal(t, int32(5), *file.Stmts.Analyze.Complexity.Cyclomatic)
}

func TestRecomputeFileCyclomatic_CountsANestedClassOnlyOnce(t *testing.T) {
	inner := &pb.StmtClass{
		Name:  &pb.Name{Short: "Inner", Qualified: "Acme\\Outer\\Inner"},
		Stmts: &pb.Stmts{StmtFunction: []*pb.StmtFunction{{Stmts: &pb.Stmts{StmtLoop: []*pb.StmtLoop{{}}}}}},
	}
	outer := &pb.StmtClass{
		Name:  &pb.Name{Short: "Outer", Qualified: "Acme\\Outer"},
		Stmts: &pb.Stmts{StmtClass: []*pb.StmtClass{inner}},
	}
	file := &pb.File{
		Stmts: &pb.Stmts{
			StmtClass: []*pb.StmtClass{outer},
			// the namespace lists both classes side by side, as the engines do
			StmtNamespace: []*pb.StmtNamespace{
				{Stmts: &pb.Stmts{StmtClass: []*pb.StmtClass{outer, inner}}},
			},
		},
	}

	recomputeFileCyclomatic(file)

	// the single method of Inner: 1 + one loop
	assert.Equal(t, int32(2), *file.Stmts.Analyze.Complexity.Cyclomatic)
}

func TestRecomputeFileCyclomatic_UsesFunctionsWhenNoClasses(t *testing.T) {
	file := &pb.File{
		Stmts: &pb.Stmts{
			StmtFunction: []*pb.StmtFunction{
				{Name: &pb.Name{Short: "A"}, Stmts: &pb.Stmts{}},
				{Name: &pb.Name{Short: "B"}, Stmts: &pb.Stmts{StmtDecisionIf: []*pb.StmtDecisionIf{{}}}},
			},
		},
	}

	recomputeFileCyclomatic(file)

	assert.Equal(t, int32(3), *file.Stmts.Analyze.Complexity.Cyclomatic)
}

func TestRecomputeFileCyclomatic_CountsBranchesWrittenOutsideAnyScope(t *testing.T) {
	// a script branching at top level, as PHP and Python allow
	file := &pb.File{
		Stmts: &pb.Stmts{
			StmtDecisionIf: []*pb.StmtDecisionIf{{}, {}},
			StmtLoop:       []*pb.StmtLoop{{}},
		},
	}

	recomputeFileCyclomatic(file)

	assert.Equal(t, int32(3), *file.Stmts.Analyze.Complexity.Cyclomatic)
}
