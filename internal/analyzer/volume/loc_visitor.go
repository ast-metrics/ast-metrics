package analyzer

import (
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// LocVisitor copies the line counts measured by the parser onto the Volume of
// each scope.
//
// It measures nothing itself: LOC, LLOC and CLOC are computed once, on the
// tree, by the engine (see internal/engine/comments.go and
// internal/engine/treesitter/statement.go). A scope already knows its own size,
// so there is nothing to add up here, and adding it up would double count every
// method of a class.
type LocVisitor struct{}

func (v *LocVisitor) Visit(stmts *pb.Stmts, parents *pb.Stmts) {
	for _, scope := range []*pb.Stmts{parents, stmts} {
		if scope == nil {
			continue
		}
		for _, fn := range scope.StmtFunction {
			publish(fn.GetStmts(), fn.GetLinesOfCode())
		}
		for _, class := range scope.StmtClass {
			publish(class.GetStmts(), class.GetLinesOfCode())
		}
	}
}

func (v *LocVisitor) LeaveNode(stmts *pb.Stmts) {
}

// publish exposes the line counts of a scope as its Volume metrics.
func publish(stmts *pb.Stmts, loc *pb.LinesOfCode) {
	if stmts == nil || loc == nil {
		return
	}
	if stmts.Analyze == nil {
		stmts.Analyze = &pb.Analyze{}
	}
	if stmts.Analyze.Volume == nil {
		stmts.Analyze.Volume = &pb.Volume{}
	}
	stmts.Analyze.Volume.Loc = &loc.LinesOfCode
	stmts.Analyze.Volume.Lloc = &loc.LogicalLinesOfCode
	stmts.Analyze.Volume.Cloc = &loc.CommentLinesOfCode
}
