package mcp

import (
	"context"
	"fmt"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/mark3labs/mcp-go/mcp"
)

func getTestQualityTool() mcp.Tool {
	return mcp.NewTool("get_test_quality",
		mcp.WithDescription("Get test quality metrics: global isolation score, traceability percentage, god tests (over-coupled tests), orphan classes (untested production classes), and isolation histogram."),
		mcp.WithBoolean("force_refresh", mcp.Description("Force re-analysis ignoring cache")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:        "Get Test Quality",
			ReadOnlyHint: mcp.ToBoolPtr(true),
		}),
	)
}

func handleGetTestQuality(svc *AnalysisService) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		forceRefresh := false
		if args := request.GetArguments(); args != nil {
			if v, ok := args["force_refresh"].(bool); ok {
				forceRefresh = v
			}
		}

		agg, _, err := svc.Analyze(forceRefresh)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Analysis failed: %v", err)), nil
		}

		tq := agg.Combined.TestQuality
		if tq == nil {
			return mcp.NewToolResultError("No test quality data available (no test files detected)"), nil
		}

		// God tests
		type godTest struct {
			FilePath       string  `json:"file_path"`
			FanOut         int     `json:"fan_out"`
			IsolationScore float64 `json:"isolation_score"`
			IsolationLabel string  `json:"isolation_label"`
		}
		// Only the head of the lists: the caller is a language model, and a large
		// project has hundreds of these.
		var godTests []godTest
		for _, gt := range analyzer.Top(tq.GodTests) {
			godTests = append(godTests, godTest{
				FilePath:       gt.FilePath,
				FanOut:         gt.SUTFanOut,
				IsolationScore: gt.IsolationScore,
				IsolationLabel: gt.IsolationLabel,
			})
		}

		// Orphan classes
		type orphan struct {
			ClassName  string  `json:"class_name"`
			FilePath   string  `json:"file_path"`
			Complexity int32   `json:"complexity"`
			Weight     float64 `json:"weight"`
		}
		var orphans []orphan
		for _, oc := range analyzer.Top(tq.OrphanClasses) {
			orphans = append(orphans, orphan{
				ClassName:  oc.ClassName,
				FilePath:   oc.FilePath,
				Complexity: oc.Complexity,
				Weight:     oc.Weight,
			})
		}

		result := map[string]any{
			"global_isolation_score": tq.GlobalIsolationScore,
			"isolation_label":        tq.IsolationLabel,
			"traceability_pct":       tq.TraceabilityPct,
			"nb_test_files":          tq.NbTestFiles,
			"nb_prod_files":          tq.NbProdFiles,
			"nb_prod_classes":        tq.NbProdClasses,
			"nb_tested_classes":      tq.NbTestedClasses,
			"god_tests":              godTests,
			"orphan_classes":         orphans,
			"isolation_histogram": map[string]int{
				"0-19":   tq.IsolationHistogram[0],
				"20-39":  tq.IsolationHistogram[1],
				"40-59":  tq.IsolationHistogram[2],
				"60-79":  tq.IsolationHistogram[3],
				"80-100": tq.IsolationHistogram[4],
			},
		}

		return safeToolResultJSON(result)
	}
}
