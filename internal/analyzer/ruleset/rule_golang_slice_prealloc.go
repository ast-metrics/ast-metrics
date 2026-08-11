package ruleset

import (
	"fmt"
	"os"
	"regexp"

	"github.com/ast-metrics/ast-metrics/internal/analyzer/issue"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// Rule: Slice preallocation heuristic

type ruleSlicePrealloc struct{}

func (r *ruleSlicePrealloc) Name() string { return "slice_prealloc" }
func (r *ruleSlicePrealloc) Description() string {
	return "Suggest preallocating slice capacity when appending in a bounded loop"
}
func (r *ruleSlicePrealloc) CheckFile(file *pb.File, addError func(issue.RequirementError), addSuccess func(string)) {
	b, err := os.ReadFile(file.Path)
	if err != nil {
		return
	}
	src := string(b)
	// Find patterns like: for i := 0; i < len(x) or i < n; i++ { ... append(s, ...) }
	reFor := regexp.MustCompile(`for\s*\([^)]*;\s*[a-zA-Z0-9_]+\s*<\s*(len\s*\([^)]*\)|[a-zA-Z0-9_]+);[^)]*\)\s*{[\s\S]*?}`)
	reAppend := regexp.MustCompile(`append\s*\(\s*([a-zA-Z0-9_]+)\s*,`)
	matches := reFor.FindAllString(src, -1)
	count := 0
	for _, block := range matches {
		if reAppend.MatchString(block) {
			// Heuristic: if slice s is appended inside bounded loop, suggest make with capacity
			count++
		}
	}
	if count > 0 {
		addError(issue.RequirementError{
			Severity: issue.SeverityLow,
			Message:  fmt.Sprintf("Found %d loop(s) appending to a slice with known bound; consider preallocating capacity with make(T, 0, n)", count),
			Code:     r.Name(),
		})
		return
	}
	addSuccess("No slice preallocation opportunities OK")
}
