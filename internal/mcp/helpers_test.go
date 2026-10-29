package mcp

import (
	"math"
	"testing"

	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"
)

func TestSanitizeJSON_NaN(t *testing.T) {
	input := map[string]any{
		"valid":   42.0,
		"nan":     math.NaN(),
		"inf":     math.Inf(1),
		"neg_inf": math.Inf(-1),
		"nested": map[string]any{
			"nan": math.NaN(),
			"ok":  "hello",
		},
	}

	result := sanitizeJSON(input).(map[string]any)

	assert.Equal(t, 42.0, result["valid"])
	assert.Equal(t, 0.0, result["nan"])
	assert.Equal(t, 0.0, result["inf"])
	assert.Equal(t, 0.0, result["neg_inf"])

	nested := result["nested"].(map[string]any)
	assert.Equal(t, 0.0, nested["nan"])
	assert.Equal(t, "hello", nested["ok"])
}

func TestSanitizeJSON_NoNaN(t *testing.T) {
	input := map[string]any{
		"a": 1.0,
		"b": "text",
		"c": 42,
	}

	result := sanitizeJSON(input).(map[string]any)
	assert.Equal(t, 1.0, result["a"])
	assert.Equal(t, "text", result["b"])
	assert.Equal(t, 42, result["c"])
}

func TestBuildFileMetrics(t *testing.T) {
	f := &pb.File{
		Path:                "src/main.go",
		ProgrammingLanguage: "Go",
		Stmts: &pb.Stmts{
			Analyze: &pb.Analyze{
				Complexity: &pb.Complexity{
					Cyclomatic: proto.Int32(15),
				},
				Volume: &pb.Volume{
					Loc:  proto.Int32(200),
					Lloc: proto.Int32(150),
					Cloc: proto.Int32(30),
				},
				Maintainability: &pb.Maintainability{
					MaintainabilityIndex: proto.Float64(85.5),
				},
				Risk: &pb.Risk{
					Score: 0.4,
				},
				Coupling: &pb.Coupling{
					Afferent:    3,
					Efferent:    5,
					Instability: 0.625,
				},
			},
		},
	}

	result := buildFileMetrics(f)

	assert.Equal(t, "src/main.go", result["path"])
	assert.Equal(t, "Go", result["language"])
	assert.Equal(t, int32(15), result["cyclomatic_complexity"])
	assert.Equal(t, 0.4, result["risk_score"])

	coupling := result["coupling"].(map[string]any)
	assert.Equal(t, int32(3), coupling["afferent"])
	assert.Equal(t, int32(5), coupling["efferent"])
	assert.Equal(t, 0.625, coupling["instability"])

	vol := result["volume"].(map[string]any)
	assert.Equal(t, int32(200), vol["loc"])

	maint := result["maintainability"].(map[string]any)
	assert.Equal(t, 85.5, maint["maintainability_index"])
}

func TestBuildFileMetrics_Minimal(t *testing.T) {
	f := &pb.File{
		Path:                "empty.py",
		ProgrammingLanguage: "Python",
	}

	result := buildFileMetrics(f)
	assert.Equal(t, "empty.py", result["path"])
	assert.Equal(t, "Python", result["language"])
	// No crash on nil Stmts
	_, hasCyclomatic := result["cyclomatic_complexity"]
	assert.False(t, hasCyclomatic)
}

func TestGetClassName(t *testing.T) {
	assert.Equal(t, "pkg.MyClass", getClassName(&pb.StmtClass{
		Name: &pb.Name{Short: "MyClass", Qualified: "pkg.MyClass"},
	}))

	assert.Equal(t, "MyClass", getClassName(&pb.StmtClass{
		Name: &pb.Name{Short: "MyClass"},
	}))

	assert.Equal(t, "(anonymous)", getClassName(&pb.StmtClass{}))
}

func TestGetFuncName(t *testing.T) {
	assert.Equal(t, "pkg.DoStuff", getFuncName(&pb.StmtFunction{
		Name: &pb.Name{Short: "DoStuff", Qualified: "pkg.DoStuff"},
	}))

	assert.Equal(t, "(anonymous)", getFuncName(&pb.StmtFunction{}))
}

func TestMatchesPath(t *testing.T) {
	assert.True(t, matchesPath("/home/user/project/src/main.go", "src/main.go"))
	assert.True(t, matchesPath("src/main.go", "src/main.go"))
	assert.True(t, matchesPath("/home/user/project/src/main.go", "main.go"))
	assert.False(t, matchesPath("src/main.go", "other.go"))
}

func TestFillAnalyzeMetrics_CognitiveComplexity(t *testing.T) {
	m := map[string]any{}
	fillAnalyzeMetrics(m, &pb.Analyze{Complexity: &pb.Complexity{
		Cyclomatic: proto.Int32(4),
		Cognitive:  proto.Int32(7),
	}})

	assert.Equal(t, int32(7), m["cognitive_complexity"])
}
