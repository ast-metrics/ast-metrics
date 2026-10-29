package analyzer

import (
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// CognitiveComplexityVisitor copies the cognitive complexity scored by the
// engine onto the Complexity of each function.
//
// The score needs the syntax tree, for nesting and for the operators around a
// boolean expression, so the engine computes it while parsing (see
// internal/engine/treesitter/cognitive.go). Only functions are scored.
type CognitiveComplexityVisitor struct{}

func (v *CognitiveComplexityVisitor) Visit(stmts *pb.Stmts, parents *pb.Stmts) {
	for _, scope := range []*pb.Stmts{parents, stmts} {
		for _, fn := range scope.GetStmtFunction() {
			publishCognitive(fn)
		}
	}
}

func (v *CognitiveComplexityVisitor) LeaveNode(stmts *pb.Stmts) {
}

func publishCognitive(fn *pb.StmtFunction) {
	if fn.GetStmts() == nil || fn.CognitiveComplexity == nil {
		return
	}
	if fn.Stmts.Analyze == nil {
		fn.Stmts.Analyze = &pb.Analyze{}
	}
	if fn.Stmts.Analyze.Complexity == nil {
		fn.Stmts.Analyze.Complexity = &pb.Complexity{}
	}
	score := fn.GetCognitiveComplexity()
	fn.Stmts.Analyze.Complexity.Cognitive = &score
}
