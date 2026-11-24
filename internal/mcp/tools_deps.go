package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

func getDependenciesTool() mcp.Tool {
	return mcp.NewTool("get_dependencies",
		mcp.WithDescription("Get the dependency graph for a package or component. Shows which packages depend on it (afferent) and which it depends on (efferent)."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Package or component name to look up in the dependency graph")),
		mcp.WithNumber("depth", mcp.Description("How many levels of dependencies to traverse (default: 2)")),
		mcp.WithBoolean("force_refresh", mcp.Description("Force re-analysis ignoring cache")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:        "Get Dependencies",
			ReadOnlyHint: mcp.ToBoolPtr(true),
		}),
	)
}

func handleGetDependencies(svc *AnalysisService) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, err := request.RequireString("name")
		if err != nil {
			return mcp.NewToolResultError("Missing required parameter: name"), nil
		}

		args := request.GetArguments()
		depth := 2
		forceRefresh := false
		if args != nil {
			if v, ok := args["depth"].(float64); ok {
				depth = int(v)
			}
			if v, ok := args["force_refresh"].(bool); ok {
				forceRefresh = v
			}
		}

		agg, _, err := svc.Analyze(forceRefresh)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Analysis failed: %v", err)), nil
		}

		graph := agg.Combined.Graph
		if graph == nil || graph.Nodes == nil {
			return mcp.NewToolResultError("No dependency graph available"), nil
		}

		// Find matching node(s)
		var matchedNodeID string
		for id := range graph.Nodes {
			if strings.Contains(strings.ToLower(id), strings.ToLower(name)) {
				matchedNodeID = id
				break
			}
		}

		if matchedNodeID == "" {
			// Try package relations
			available := make([]string, 0)
			for id := range graph.Nodes {
				available = append(available, id)
			}
			return safeToolResultJSON(map[string]any{
				"error":           fmt.Sprintf("Node '%s' not found in dependency graph", name),
				"available_nodes": available,
			})
		}

		// BFS to collect subgraph up to depth
		type edge struct {
			From string `json:"from"`
			To   string `json:"to"`
		}

		visited := map[string]bool{matchedNodeID: true}
		queue := []struct {
			id    string
			level int
		}{{matchedNodeID, 0}}

		var edges []edge
		nodes := map[string]map[string]any{}

		for len(queue) > 0 {
			curr := queue[0]
			queue = queue[1:]

			node := graph.Nodes[curr.id]
			if node == nil {
				continue
			}

			nodeName := curr.id
			if node.Name != nil && node.Name.Short != "" {
				nodeName = node.Name.Short
			}
			nodes[curr.id] = map[string]any{
				"id":   curr.id,
				"name": nodeName,
			}

			if curr.level < depth {
				for _, targetID := range node.Edges {
					edges = append(edges, edge{From: curr.id, To: targetID})
					if !visited[targetID] {
						visited[targetID] = true
						queue = append(queue, struct {
							id    string
							level int
						}{targetID, curr.level + 1})
					}
				}
			}
		}

		// Also find afferent edges (who points to matched node)
		for id, node := range graph.Nodes {
			if visited[id] {
				continue
			}
			for _, targetID := range node.Edges {
				if targetID == matchedNodeID {
					edges = append(edges, edge{From: id, To: targetID})
					nodeName := id
					if node.Name != nil && node.Name.Short != "" {
						nodeName = node.Name.Short
					}
					nodes[id] = map[string]any{
						"id":   id,
						"name": nodeName,
					}
				}
			}
		}

		// Package relations if available
		var packageDeps []map[string]any
		if agg.Combined.PackageRelations != nil {
			for from, targets := range agg.Combined.PackageRelations {
				if strings.Contains(strings.ToLower(from), strings.ToLower(name)) {
					for to, count := range targets {
						packageDeps = append(packageDeps, map[string]any{
							"from":  from,
							"to":    to,
							"count": count,
						})
					}
				}
			}
		}

		result := map[string]any{
			"queried_node": matchedNodeID,
			"nodes":        nodes,
			"edges":        edges,
		}
		if len(packageDeps) > 0 {
			result["package_relations"] = packageDeps
		}

		return safeToolResultJSON(result)
	}
}

func getCouplingTool() mcp.Tool {
	return mcp.NewTool("get_coupling",
		mcp.WithDescription("Get coupling analysis for a specific component: afferent coupling (who depends on me), efferent coupling (who I depend on), and instability metric."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Class or package name to analyze coupling for")),
		mcp.WithBoolean("force_refresh", mcp.Description("Force re-analysis ignoring cache")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:        "Get Coupling",
			ReadOnlyHint: mcp.ToBoolPtr(true),
		}),
	)
}

func handleGetCoupling(svc *AnalysisService) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, err := request.RequireString("name")
		if err != nil {
			return mcp.NewToolResultError("Missing required parameter: name"), nil
		}

		args := request.GetArguments()
		forceRefresh := false
		if args != nil {
			if v, ok := args["force_refresh"].(bool); ok {
				forceRefresh = v
			}
		}

		agg, _, err := svc.Analyze(forceRefresh)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Analysis failed: %v", err)), nil
		}

		nameLower := strings.ToLower(name)

		// Search in files
		for _, f := range agg.Combined.ConcernedFiles {
			if !strings.Contains(strings.ToLower(f.Path), nameLower) {
				continue
			}
			if f.Stmts == nil || f.Stmts.Analyze == nil || f.Stmts.Analyze.Coupling == nil {
				continue
			}

			c := f.Stmts.Analyze.Coupling
			result := map[string]any{
				"path":        f.Path,
				"afferent":    c.Afferent,
				"efferent":    c.Efferent,
				"instability": c.Instability,
			}

			// Find what this file depends on via graph edges
			if agg.Combined.Graph != nil {
				var dependsOn []string
				var dependedBy []string
				for id, node := range agg.Combined.Graph.Nodes {
					if strings.Contains(strings.ToLower(id), nameLower) {
						for _, edge := range node.Edges {
							dependsOn = append(dependsOn, edge)
						}
					} else {
						for _, edge := range node.Edges {
							if strings.Contains(strings.ToLower(edge), nameLower) {
								dependedBy = append(dependedBy, id)
							}
						}
					}
				}
				if len(dependsOn) > 0 {
					result["depends_on"] = dependsOn
				}
				if len(dependedBy) > 0 {
					result["depended_by"] = dependedBy
				}
			}

			return safeToolResultJSON(result)
		}

		// Search in class-level afferent coupling map
		if agg.Combined.ClassesAfferentCoupling != nil {
			for className, count := range agg.Combined.ClassesAfferentCoupling {
				if strings.Contains(strings.ToLower(className), nameLower) {
					return safeToolResultJSON(map[string]any{
						"class":             className,
						"afferent_coupling": count,
					})
				}
			}
		}

		return mcp.NewToolResultError(fmt.Sprintf("Component '%s' not found", name)), nil
	}
}

func whoUsesTool() mcp.Tool {
	return mcp.NewTool("who_uses",
		mcp.WithDescription("Find which files depend on a library, directly (they import it) or through other files, level by level, and which communities they belong to. Answers 'what is exposed if this library has a vulnerability?' or 'how much of the code stands on this framework?'. The name is matched anywhere in the imported module: 'log4j' finds org.apache.logging.log4j."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Library or module name to look for, as the imports spell it (e.g. 'log4j', 'react', 'github.com/sirupsen/logrus', 'Monolog')")),
		mcp.WithNumber("max_depth", mcp.Description("How many levels past the importers to follow (default: unbounded)")),
		mcp.WithBoolean("force_refresh", mcp.Description("Force re-analysis ignoring cache")),
		mcp.WithToolAnnotation(mcp.ToolAnnotation{
			Title:        "Who Uses a Library",
			ReadOnlyHint: mcp.ToBoolPtr(true),
		}),
	)
}

func handleWhoUses(svc *AnalysisService) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name, err := request.RequireString("name")
		if err != nil || strings.TrimSpace(name) == "" {
			return mcp.NewToolResultError("Missing required parameter: name"), nil
		}

		args := request.GetArguments()
		maxDepth := 0
		forceRefresh := false
		if args != nil {
			if v, ok := args["max_depth"].(float64); ok {
				maxDepth = int(v)
			}
			if v, ok := args["force_refresh"].(bool); ok {
				forceRefresh = v
			}
		}

		agg, _, err := svc.Analyze(forceRefresh)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Analysis failed: %v", err)), nil
		}

		reach := agg.Combined.WhoUses(name, maxDepth)
		if len(reach.Modules) == 0 {
			return safeToolResultJSON(map[string]any{
				"error": fmt.Sprintf("No imported module matches '%s'. Names are the ones the imports use.", name),
				"hint":  "Call get_dependencies or read the 'libraries' section of the JSON report to list the imported modules.",
			})
		}
		return safeToolResultJSON(map[string]any{
			"query":       reach.Query,
			"modules":     reach.Modules,
			"files":       reach.Files(),
			"scope":       reach.Scope,
			"levels":      reach.Levels,
			"communities": reach.Communities,
		})
	}
}
