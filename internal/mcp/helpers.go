package mcp

import (
	"math"

	pb "github.com/ast-metrics/ast-metrics/pb"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

// buildFileMetrics extracts metrics from a pb.File into a map.
func buildFileMetrics(f *pb.File) map[string]any {
	result := map[string]any{
		"path":     f.Path,
		"language": f.ProgrammingLanguage,
	}

	if f.Stmts != nil && f.Stmts.Analyze != nil {
		fillAnalyzeMetrics(result, f.Stmts.Analyze)
	}

	if f.LinesOfCode != nil {
		result["lines_of_code"] = map[string]any{
			"lines_of_code":    f.LinesOfCode.LinesOfCode,
			"comment_lines":    f.LinesOfCode.CommentLinesOfCode,
			"logical_lines":    f.LinesOfCode.LogicalLinesOfCode,
		}
	}

	return result
}

// fillAnalyzeMetrics fills a map with metrics from a pb.Analyze struct.
func fillAnalyzeMetrics(m map[string]any, a *pb.Analyze) {
	if a.Complexity != nil && a.Complexity.Cyclomatic != nil {
		m["cyclomatic_complexity"] = *a.Complexity.Cyclomatic
	}
	if a.Complexity != nil && a.Complexity.Cognitive != nil {
		m["cognitive_complexity"] = *a.Complexity.Cognitive
	}

	if a.Volume != nil {
		vol := map[string]any{}
		if a.Volume.Loc != nil {
			vol["loc"] = *a.Volume.Loc
		}
		if a.Volume.Lloc != nil {
			vol["lloc"] = *a.Volume.Lloc
		}
		if a.Volume.Cloc != nil {
			vol["cloc"] = *a.Volume.Cloc
		}
		if a.Volume.HalsteadVolume != nil {
			vol["halstead_volume"] = *a.Volume.HalsteadVolume
		}
		if a.Volume.HalsteadDifficulty != nil {
			vol["halstead_difficulty"] = *a.Volume.HalsteadDifficulty
		}
		if a.Volume.HalsteadEffort != nil {
			vol["halstead_effort"] = *a.Volume.HalsteadEffort
		}
		if a.Volume.HalsteadBugs != nil {
			vol["halstead_bugs"] = *a.Volume.HalsteadBugs
		}
		if len(vol) > 0 {
			m["volume"] = vol
		}
	}

	if a.Maintainability != nil {
		maint := map[string]any{}
		if a.Maintainability.MaintainabilityIndex != nil {
			maint["maintainability_index"] = *a.Maintainability.MaintainabilityIndex
		}
		if a.Maintainability.MaintainabilityIndexWithoutComments != nil {
			maint["maintainability_index_without_comments"] = *a.Maintainability.MaintainabilityIndexWithoutComments
		}
		if a.Maintainability.CommentWeight != nil {
			maint["comment_weight"] = *a.Maintainability.CommentWeight
		}
		if len(maint) > 0 {
			m["maintainability"] = maint
		}
	}

	if a.Risk != nil {
		m["risk_score"] = a.Risk.Score
	}

	if a.Coupling != nil {
		m["coupling"] = map[string]any{
			"afferent":    a.Coupling.Afferent,
			"efferent":    a.Coupling.Efferent,
			"instability": a.Coupling.Instability,
		}
	}

	if a.ClassCohesion != nil {
		coh := map[string]any{}
		if a.ClassCohesion.Lcom4 != nil {
			coh["lcom4"] = *a.ClassCohesion.Lcom4
		}
		if len(coh) > 0 {
			m["cohesion"] = coh
		}
	}
}

// safeToolResultJSON is like mcp.NewToolResultJSON but sanitizes NaN/Inf first.
func safeToolResultJSON(data any) (*mcplib.CallToolResult, error) {
	return mcplib.NewToolResultJSON(sanitizeJSON(data))
}

// sanitizeJSON recursively replaces NaN/Inf float64 values with 0 in maps,
// slices, and structs so that json.Marshal does not fail.
func sanitizeJSON(v any) any {
	switch val := v.(type) {
	case float64:
		if math.IsNaN(val) || math.IsInf(val, 0) {
			return 0.0
		}
		return val
	case map[string]any:
		for k, item := range val {
			val[k] = sanitizeJSON(item)
		}
		return val
	case []any:
		for i, item := range val {
			val[i] = sanitizeJSON(item)
		}
		return val
	case []map[string]any:
		for i, item := range val {
			val[i] = sanitizeJSON(item).(map[string]any)
		}
		return val
	default:
		return v
	}
}

// getClassName returns the best available name for a class.
func getClassName(c *pb.StmtClass) string {
	if c.Name != nil {
		if c.Name.Qualified != "" {
			return c.Name.Qualified
		}
		return c.Name.Short
	}
	return "(anonymous)"
}

// getFuncName returns the best available name for a function.
func getFuncName(f *pb.StmtFunction) string {
	if f.Name != nil {
		if f.Name.Qualified != "" {
			return f.Name.Qualified
		}
		return f.Name.Short
	}
	return "(anonymous)"
}
