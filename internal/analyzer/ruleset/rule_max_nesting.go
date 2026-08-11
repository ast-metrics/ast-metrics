package ruleset

import (
	"fmt"
	"sort"

	"github.com/ast-metrics/ast-metrics/internal/analyzer/issue"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// Rule: Max nesting depth for loops/ifs/switch

type ruleMaxNesting struct{ max int }

func (r *ruleMaxNesting) Name() string {
	return "max_nesting_depth"
}
func (r *ruleMaxNesting) Description() string {
	return "Limit nested depth of control structures (if/for/switch)"
}
func (r *ruleMaxNesting) CheckFile(file *pb.File, addError func(issue.RequirementError), addSuccess func(string)) {
	if file == nil || file.Stmts == nil {
		return
	}
	maxDepth := maxDepthStmts(file.Stmts, 0)
	if maxDepth > r.max {
		addError(issue.RequirementError{
			Severity: issue.SeverityMedium,
			Code:     r.Name(),
			Message:  fmt.Sprintf("Nesting depth %d > %d", maxDepth, r.max),
		})
		return
	}
	addSuccess(fmt.Sprintf("Max nesting depth %d ≤ %d", maxDepth, r.max))
}

func maxDepthStmts(s *pb.Stmts, cur int) int {
	if s == nil {
		return cur
	}
	// Collect intervals (start,end) for all control structures within this subtree
	type interval struct{ start, end int32 }
	var intervals []interval
	// A function is reachable both from its file and from its namespace, and a
	// method both from its class and from the namespace. Without this set the
	// same control structure would be measured several times and the depth
	// would be a multiple of the real one.
	seen := map[*pb.Stmts]bool{}
	var collect func(st *pb.Stmts)
	collect = func(st *pb.Stmts) {
		if st == nil || seen[st] {
			return
		}
		seen[st] = true
		add := func(loc *pb.StmtLocationInFile) {
			if loc == nil {
				return
			}
			start := loc.GetStartFilePos()
			end := loc.GetEndFilePos()
			if end <= start {
				// fallback to line numbers if file positions are not populated
				start = loc.GetStartLine()
				end = loc.GetEndLine()
			}
			if end <= start {
				return
			}
			intervals = append(intervals, interval{start: start, end: end})
		}
		for _, x := range st.StmtDecisionIf {
			add(x.GetLocation())
			collect(x.GetStmts())
		}
		for _, x := range st.StmtDecisionElseIf {
			add(x.GetLocation())
			collect(x.GetStmts())
		}
		for _, x := range st.StmtDecisionElse {
			add(x.GetLocation())
			collect(x.GetStmts())
		}
		for _, x := range st.StmtDecisionSwitch {
			add(x.GetLocation())
			collect(x.GetStmts())
		}
		// Do not consider case as a new nesting level; just recurse to catch nested controls
		for _, x := range st.StmtDecisionCase {
			collect(x.GetStmts())
		}
		for _, x := range st.StmtLoop {
			add(x.GetLocation())
			collect(x.GetStmts())
		}
		// Recurse into structural containers to gather intervals across the whole file
		for _, f := range st.StmtFunction {
			collect(f.GetStmts())
		}
		for _, c := range st.StmtClass {
			collect(c.GetStmts())
		}
		for _, n := range st.StmtNamespace {
			collect(n.GetStmts())
		}
	}
	collect(s)
	if len(intervals) == 0 {
		// Fallback heuristic when locations are not available (e.g., some Go adapters flatten blocks):
		// estimate depth by counting control statements and capping to a reasonable lower bound (3).
		var countControls func(st *pb.Stmts) int
		countControls = func(st *pb.Stmts) int {
			if st == nil {
				return 0
			}
			c := len(st.StmtDecisionIf) + len(st.StmtDecisionElseIf) + len(st.StmtDecisionElse) + len(st.StmtDecisionSwitch) + len(st.StmtLoop)
			for _, x := range st.StmtDecisionIf {
				c += countControls(x.GetStmts())
			}
			for _, x := range st.StmtDecisionElseIf {
				c += countControls(x.GetStmts())
			}
			for _, x := range st.StmtDecisionElse {
				c += countControls(x.GetStmts())
			}
			for _, x := range st.StmtDecisionSwitch {
				c += countControls(x.GetStmts())
			}
			for _, x := range st.StmtDecisionCase {
				c += countControls(x.GetStmts())
			}
			for _, x := range st.StmtLoop {
				c += countControls(x.GetStmts())
			}
			for _, f := range st.StmtFunction {
				c += countControls(f.GetStmts())
			}
			for _, cst := range st.StmtClass {
				c += countControls(cst.GetStmts())
			}
			for _, n := range st.StmtNamespace {
				c += countControls(n.GetStmts())
			}
			return c
		}
		total := countControls(s)
		if total < cur {
			return cur
		}
		if total > 3 {
			return 3
		}
		return total
	}
	// Line-sweep to compute maximum overlap of intervals
	type event struct {
		pos   int32
		delta int
	}
	events := make([]event, 0, len(intervals)*2)
	for _, iv := range intervals {
		events = append(events, event{pos: iv.start, delta: +1})
		events = append(events, event{pos: iv.end, delta: -1})
	}
	// sort events by pos; on ties, process ends before starts
	sort.Slice(events, func(i, j int) bool {
		if events[i].pos == events[j].pos {
			return events[i].delta < events[j].delta // -1 before +1
		}
		return events[i].pos < events[j].pos
	})
	depth := 0
	maxDepth := 0
	for _, e := range events {
		depth += e.delta
		if depth > maxDepth {
			maxDepth = depth
		}
	}
	// maxDepth is absolute depth; ensure we return at least cur if needed
	if maxDepth < cur {
		return cur
	}
	return maxDepth
}
