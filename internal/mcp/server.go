package mcp

import (
	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/mark3labs/mcp-go/server"
)

// NewMCPServer creates and configures an MCP server with all ast-metrics tools.
func NewMCPServer(version string, config *configuration.Configuration, runners []engine.Engine) *server.MCPServer {
	s := server.NewMCPServer(
		"ast-metrics",
		version,
		server.WithToolCapabilities(true),
		server.WithInstructions("ast-metrics provides code analysis tools: complexity metrics, coupling analysis, community detection, risk scoring, test quality, and dependency graphs. Use analyze_project first to get an overview, then drill down with specific tools."),
	)

	svc := NewAnalysisService(config, runners)

	// Register all tools
	s.AddTool(analyzeProjectTool(), handleAnalyzeProject(svc))
	s.AddTool(getFileMetricsTool(), handleGetFileMetrics(svc))
	s.AddTool(findRiskyCodeTool(), handleFindRiskyCode(svc))
	s.AddTool(findComplexCodeTool(), handleFindComplexCode(svc))
	s.AddTool(getDependenciesTool(), handleGetDependencies(svc))
	s.AddTool(getCouplingTool(), handleGetCoupling(svc))
	s.AddTool(whoUsesTool(), handleWhoUses(svc))
	s.AddTool(getCommunitiesTool(), handleGetCommunities(svc))
	s.AddTool(getTestQualityTool(), handleGetTestQuality(svc))
	s.AddTool(listComponentsTool(), handleListComponents(svc))

	return s
}

// ServeStdio starts the MCP server on stdio transport.
func ServeStdio(s *server.MCPServer) error {
	return server.ServeStdio(s)
}
