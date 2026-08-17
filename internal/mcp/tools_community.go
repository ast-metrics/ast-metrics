package mcp

import (
	"context"
	"fmt"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/mark3labs/mcp-go/mcp"
)

func getCommunitiesTool() mcp.Tool {
	return mcp.NewTool("get_communities",
		mcp.WithDescription("Get the communities of the project: groups of classes (or packages) that depend on each other more than on the rest of the code, detected on the dependency graph regardless of folders. Returns each community with its name, members, the namespaces it draws from, what it uses and what uses it, its owners; the dependencies between communities; the cycles; and findings in plain words (cycles, namespaces split across communities, communities spread across namespaces, bridge classes)."),
		mcp.WithBoolean("force_refresh", mcp.Description("Force re-analysis ignoring cache")),
		mcp.WithBoolean("with_members", mcp.Description("List every member of every community (can be long); default lists the hubs only")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:        "Get Communities",
			ReadOnlyHint: mcp.ToBoolPtr(true),
		}),
	)
}

func handleGetCommunities(svc *AnalysisService) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		forceRefresh := false
		withMembers := false
		if args := request.GetArguments(); args != nil {
			if v, ok := args["force_refresh"].(bool); ok {
				forceRefresh = v
			}
			if v, ok := args["with_members"].(bool); ok {
				withMembers = v
			}
		}

		agg, _, err := svc.Analyze(forceRefresh)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Analysis failed: %v", err)), nil
		}

		cm := agg.Combined.Community
		if cm == nil {
			return mcp.NewToolResultError("No community analysis available (dependency graph may be too small)"), nil
		}
		return safeToolResultJSON(analyzer.ExportCommunities(cm, withMembers))
	}
}
