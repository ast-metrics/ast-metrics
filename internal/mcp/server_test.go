package mcp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"
)

// mockAnalysisService returns a pre-built AnalysisService with fake data
// so tool handlers can be tested without running real parsers.
func newTestAggregated() *analyzer.ProjectAggregated {
	return &analyzer.ProjectAggregated{
		Combined: analyzer.Aggregated{
			NbFiles:     3,
			NbClasses:   2,
			NbFunctions: 5,
			NbMethods:   4,
			ProgrammingLanguages: map[string]int{
				"Go":     2,
				"Python": 1,
			},
			CyclomaticComplexity: analyzer.AggregateResult{Avg: 5.0, Max: 20, Sum: 50},
			MaintainabilityIndex: analyzer.AggregateResult{Avg: 80.0, Min: 40.0},
			AfferentCoupling:     analyzer.AggregateResult{Avg: 2.0},
			EfferentCoupling:     analyzer.AggregateResult{Avg: 3.0},
			Instability:          analyzer.AggregateResult{Avg: 0.6},
			Loc:                  analyzer.AggregateResult{Sum: 1000, Avg: 333},
			BusFactor:            2,
			ConcernedFiles: []*pb.File{
				{
					Path:                "/project/cmd/main.go",
					ProgrammingLanguage: "Go",
					Stmts: &pb.Stmts{
						Analyze: &pb.Analyze{
							Complexity:      &pb.Complexity{Cyclomatic: proto.Int32(20)},
							Volume:          &pb.Volume{Loc: proto.Int32(300)},
							Maintainability: &pb.Maintainability{MaintainabilityIndex: proto.Float64(45.0)},
							Risk:            &pb.Risk{Score: 0.8},
							Coupling:        &pb.Coupling{Afferent: 1, Efferent: 5, Instability: 0.83},
						},
						StmtClass: []*pb.StmtClass{
							{
								Name: &pb.Name{Short: "App", Qualified: "cmd.App"},
								Stmts: &pb.Stmts{
									Analyze: &pb.Analyze{
										Complexity:      &pb.Complexity{Cyclomatic: proto.Int32(15)},
										Maintainability: &pb.Maintainability{MaintainabilityIndex: proto.Float64(50.0)},
										Risk:            &pb.Risk{Score: 0.7},
									},
								},
							},
						},
						StmtFunction: []*pb.StmtFunction{
							{
								Name: &pb.Name{Short: "main", Qualified: "cmd.main"},
								Stmts: &pb.Stmts{
									Analyze: &pb.Analyze{
										Complexity: &pb.Complexity{Cyclomatic: proto.Int32(12)},
										Volume:     &pb.Volume{Loc: proto.Int32(80)},
									},
								},
							},
						},
					},
				},
				{
					Path:                "/project/internal/util.go",
					ProgrammingLanguage: "Go",
					Stmts: &pb.Stmts{
						Analyze: &pb.Analyze{
							Complexity:      &pb.Complexity{Cyclomatic: proto.Int32(3)},
							Volume:          &pb.Volume{Loc: proto.Int32(50)},
							Maintainability: &pb.Maintainability{MaintainabilityIndex: proto.Float64(95.0)},
							Risk:            &pb.Risk{Score: 0.05},
							Coupling:        &pb.Coupling{Afferent: 4, Efferent: 1, Instability: 0.2},
						},
					},
				},
				{
					Path:                "/project/scripts/deploy.py",
					ProgrammingLanguage: "Python",
					Stmts: &pb.Stmts{
						Analyze: &pb.Analyze{
							Complexity: &pb.Complexity{Cyclomatic: proto.Int32(5)},
							Risk:       &pb.Risk{Score: 0.2},
						},
					},
				},
			},
			Graph: &pb.Graph{
				Nodes: map[string]*pb.Node{
					"cmd": {
						Id:    "cmd",
						Name:  &pb.Name{Short: "cmd"},
						Edges: []string{"internal/util"},
					},
					"internal/util": {
						Id:   "internal/util",
						Name: &pb.Name{Short: "util"},
					},
				},
			},
			PackageRelations: map[string]map[string]int{
				"cmd": {"internal/util": 3},
			},
			ClassesAfferentCoupling: map[string]int{
				"cmd.App": 2,
			},
			Community: &analyzer.CommunityMetrics{
				Granularity:      analyzer.GranularityNamespace,
				CommunitiesCount: 2,
				MaxSize:          1,
				UnitCount:        2,
				InternalShare:    0,
				Communities: []*analyzer.Community{
					{ID: "0", Name: "cmd", ShortName: "cmd", Units: []string{"cmd"}, Size: 1, Cohesive: true,
						Namespaces: []analyzer.NamespaceShare{{Namespace: "cmd", Count: 1, Share: 1}},
						Uses:       []analyzer.CommunityLink{{ID: "1", Name: "internal/util", Weight: 3}}},
					{ID: "1", Name: "internal/util", ShortName: "internal/util", Units: []string{"internal/util"}, Size: 1, Cohesive: true,
						Namespaces: []analyzer.NamespaceShare{{Namespace: "internal/util", Count: 1, Share: 1}},
						UsedBy:     []analyzer.CommunityLink{{ID: "0", Name: "cmd", Weight: 3}}},
				},
				NodeToCommunity: map[string]string{"cmd": "0", "internal/util": "1"},
				Edges:           []analyzer.CommunityEdge{{From: "0", To: "1", Weight: 3}},
				Verdict:         "Your code forms 2 communities.",
				VerdictNote:     "Each one stays inside a single namespace: the folders match the dependencies.",
			},
			TestQuality: &analyzer.TestQualityMetrics{
				GlobalIsolationScore: 75.0,
				IsolationLabel:       "Semi-isolated",
				TraceabilityPct:      60.0,
				NbTestFiles:          2,
				NbProdFiles:          3,
				NbProdClasses:        2,
				NbTestedClasses:      1,
				IsolationHistogram:   [5]int{0, 0, 1, 1, 0},
				GodTests: []analyzer.TestFileMetrics{
					{FilePath: "test_all.go", SUTFanOut: 12, IsolationScore: 30, IsolationLabel: "Coupled"},
				},
				OrphanClasses: []analyzer.OrphanClass{
					{ClassName: "Util", FilePath: "/project/internal/util.go", Complexity: 3, Weight: 0.5},
				},
			},
			Suggestions: []analyzer.Suggestion{
				{Summary: "Reduce coupling in cmd", Location: "cmd", Why: "High efferent coupling"},
			},
		},
	}
}

// parseToolResult extracts the JSON content from a CallToolResult
func parseToolResult(t *testing.T, result *mcp.CallToolResult) map[string]any {
	t.Helper()
	assert.NotNil(t, result)
	assert.False(t, result.IsError, "tool returned error: %v", result.Content)
	assert.NotEmpty(t, result.Content)

	// The result text is JSON
	textContent, ok := result.Content[0].(mcp.TextContent)
	assert.True(t, ok, "expected TextContent, got %T", result.Content[0])

	var data map[string]any
	err := json.Unmarshal([]byte(textContent.Text), &data)
	assert.NoError(t, err)
	return data
}

// prefillCache sets up the service cache with test data, bypassing real analysis.
func prefillCache(svc *AnalysisService, agg *analyzer.ProjectAggregated) {
	svc.cache.Set(agg.Combined.ConcernedFiles, *agg)
}

func TestHandleAnalyzeProject(t *testing.T) {
	svc := NewAnalysisService(nil, nil)
	agg := newTestAggregated()
	prefillCache(svc, agg)

	handler := handleAnalyzeProject(svc)
	result, err := handler(context.Background(), mcp.CallToolRequest{})
	assert.NoError(t, err)

	data := parseToolResult(t, result)

	assert.Equal(t, float64(3), data["files"])
	assert.Equal(t, float64(2), data["classes"])
	assert.Equal(t, float64(2), data["bus_factor"])

	langs := data["languages"].(map[string]any)
	assert.Equal(t, float64(2), langs["Go"])
	assert.Equal(t, float64(1), langs["Python"])
}

func TestHandleGetFileMetrics(t *testing.T) {
	svc := NewAnalysisService(nil, nil)
	agg := newTestAggregated()
	prefillCache(svc, agg)

	handler := handleGetFileMetrics(svc)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"path": "cmd/main.go"}

	result, err := handler(context.Background(), req)
	assert.NoError(t, err)

	data := parseToolResult(t, result)
	assert.Equal(t, "/project/cmd/main.go", data["path"])
	assert.Equal(t, float64(20), data["cyclomatic_complexity"])
	assert.Equal(t, 0.8, data["risk_score"])
}

func TestHandleGetFileMetrics_NotFound(t *testing.T) {
	svc := NewAnalysisService(nil, nil)
	prefillCache(svc, newTestAggregated())

	handler := handleGetFileMetrics(svc)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"path": "nonexistent.go"}

	result, err := handler(context.Background(), req)
	assert.NoError(t, err)
	assert.True(t, result.IsError)
}

func TestHandleFindRiskyCode(t *testing.T) {
	svc := NewAnalysisService(nil, nil)
	prefillCache(svc, newTestAggregated())

	handler := handleFindRiskyCode(svc)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"min_risk": 0.5}

	result, err := handler(context.Background(), req)
	assert.NoError(t, err)

	data := parseToolResult(t, result)
	items := data["risky_code"].([]any)
	assert.GreaterOrEqual(t, len(items), 1)

	// First item should be the riskiest (0.8)
	first := items[0].(map[string]any)
	assert.Equal(t, 0.8, first["risk_score"])
}

func TestHandleFindComplexCode(t *testing.T) {
	svc := NewAnalysisService(nil, nil)
	prefillCache(svc, newTestAggregated())

	handler := handleFindComplexCode(svc)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"max_cyclomatic": 10.0}

	result, err := handler(context.Background(), req)
	assert.NoError(t, err)

	data := parseToolResult(t, result)
	items := data["complex_code"].([]any)
	assert.GreaterOrEqual(t, len(items), 1)

	// All returned items should have cyclomatic >= 10
	for _, item := range items {
		m := item.(map[string]any)
		assert.GreaterOrEqual(t, m["cyclomatic_complexity"].(float64), 10.0)
	}
}

func TestHandleGetDependencies(t *testing.T) {
	svc := NewAnalysisService(nil, nil)
	prefillCache(svc, newTestAggregated())

	handler := handleGetDependencies(svc)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "cmd"}

	result, err := handler(context.Background(), req)
	assert.NoError(t, err)

	data := parseToolResult(t, result)
	assert.Equal(t, "cmd", data["queried_node"])
	assert.NotEmpty(t, data["nodes"])
	assert.NotEmpty(t, data["edges"])
}

func TestHandleGetDependencies_NotFound(t *testing.T) {
	svc := NewAnalysisService(nil, nil)
	prefillCache(svc, newTestAggregated())

	handler := handleGetDependencies(svc)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "nonexistent"}

	result, err := handler(context.Background(), req)
	assert.NoError(t, err)

	// Should return available nodes, not an error
	data := parseToolResult(t, result)
	assert.Contains(t, data, "available_nodes")
}

func TestHandleGetCoupling(t *testing.T) {
	svc := NewAnalysisService(nil, nil)
	prefillCache(svc, newTestAggregated())

	handler := handleGetCoupling(svc)

	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"name": "main.go"}

	result, err := handler(context.Background(), req)
	assert.NoError(t, err)

	data := parseToolResult(t, result)
	assert.Equal(t, "/project/cmd/main.go", data["path"])
	assert.Equal(t, float64(1), data["afferent"])
	assert.Equal(t, float64(5), data["efferent"])
}

func TestHandleGetCommunities(t *testing.T) {
	svc := NewAnalysisService(nil, nil)
	prefillCache(svc, newTestAggregated())

	handler := handleGetCommunities(svc)

	result, err := handler(context.Background(), mcp.CallToolRequest{})
	assert.NoError(t, err)

	data := parseToolResult(t, result)
	assert.Equal(t, float64(2), data["communitiesCount"])
	assert.Equal(t, "namespace", data["granularity"])
	edges := data["edges"].([]any)
	assert.Equal(t, 1, len(edges))

	communities := data["communities"].([]any)
	assert.Equal(t, 2, len(communities))
}

func TestHandleGetTestQuality(t *testing.T) {
	svc := NewAnalysisService(nil, nil)
	prefillCache(svc, newTestAggregated())

	handler := handleGetTestQuality(svc)

	result, err := handler(context.Background(), mcp.CallToolRequest{})
	assert.NoError(t, err)

	data := parseToolResult(t, result)
	assert.Equal(t, 75.0, data["global_isolation_score"])
	assert.Equal(t, "Semi-isolated", data["isolation_label"])
	assert.Equal(t, 60.0, data["traceability_pct"])

	godTests := data["god_tests"].([]any)
	assert.Equal(t, 1, len(godTests))

	orphans := data["orphan_classes"].([]any)
	assert.Equal(t, 1, len(orphans))
}

func TestHandleListComponents(t *testing.T) {
	svc := NewAnalysisService(nil, nil)
	prefillCache(svc, newTestAggregated())

	handler := handleListComponents(svc)

	result, err := handler(context.Background(), mcp.CallToolRequest{})
	assert.NoError(t, err)

	data := parseToolResult(t, result)

	graphNodes := data["graph_nodes"].([]any)
	assert.Equal(t, 2, len(graphNodes))

	files := data["files"].([]any)
	assert.Equal(t, 3, len(files))
}
