package analyzer

import (
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// CyclomaticComplexityVisitor computes the extended McCabe cyclomatic
// complexity, the measure implemented by lizard, gocyclo, phploc, PMD and
// SonarQube: a scope starts at 1 and every construct that adds a branch to the
// control flow graph adds 1.
//
// The engines fill the decision statements of a scope using a single shared
// model, documented in internal/engine/treesitter/decision.go, so that the same
// program written in two languages gets the same number. This visitor only
// applies the weights.
type CyclomaticComplexityVisitor struct {
	complexity int
}

func (v *CyclomaticComplexityVisitor) Visit(stmts *pb.Stmts, parents *pb.Stmts) {
	if stmts == nil {
		return
	}

	// The complexity of a function is its own: 1 for the entry path, plus the
	// branches it opens itself. A function declared inside it is a function of
	// its own, reported separately, exactly as lizard and gocyclo do; counting
	// it here would report it twice in any aggregate.
	//
	// A container (class, namespace, file) is the opposite: its complexity is
	// what everything it declares adds up to.
	var ccn int32
	if isFunctionBody(stmts, parents) {
		ccn = 1 + v.DecisionPoints(stmts)
	} else {
		ccn = v.Calculate(stmts)
	}

	if stmts.Analyze == nil {
		stmts.Analyze = &pb.Analyze{}
	}
	if stmts.Analyze.Complexity == nil {
		stmts.Analyze.Complexity = &pb.Complexity{}
	}

	stmts.Analyze.Complexity.Cyclomatic = &ccn
}

func (v *CyclomaticComplexityVisitor) LeaveNode(stmts *pb.Stmts) {
	if stmts == nil {
		return
	}
	ccn := v.Calculate(stmts)
	if stmts.Analyze == nil {
		stmts.Analyze = &pb.Analyze{}
	}
	if stmts.Analyze.Complexity == nil {
		stmts.Analyze.Complexity = &pb.Complexity{}
	}
	stmts.Analyze.Complexity.Cyclomatic = &ccn
}

// Calculate returns the complexity of a container scope: the branches it opens
// itself, plus the complexity of everything it declares. Each function it holds
// contributes its own baseline of 1, at any depth.
//
// Three kinds of statement are deliberately worth nothing:
//   - else, because the if it belongs to already paid for both outcomes;
//   - switch and match, which only hold their branches;
//   - default and the catch-all arm, which are the fallback path rather than a
//     new one.
func (v *CyclomaticComplexityVisitor) Calculate(stmts *pb.Stmts) int32 {

	if stmts == nil {
		return 0
	}

	ccn := v.DecisionPoints(stmts)

	// Each function/method contributes a baseline cyclomatic complexity of 1.
	for _, stmt := range stmts.StmtFunction {
		if stmt == nil {
			continue
		}
		ccn += 1 + v.Calculate(stmt.Stmts)
	}

	// A class is not a decision point, but it holds its methods.
	for _, stmt := range stmts.StmtClass {
		ccn += v.Calculate(stmt.Stmts)
	}
	for _, stmt := range stmts.StmtInterface {
		ccn += v.Calculate(stmt.Stmts)
	}

	return ccn
}

// DecisionPoints returns the number of branches a scope opens directly,
// ignoring the scopes nested inside it. Callers that aggregate already computed
// per-scope complexities use it to add what belongs to the scope itself, such
// as the branches a script writes outside of any function.
func (v *CyclomaticComplexityVisitor) DecisionPoints(stmts *pb.Stmts) int32 {
	if stmts == nil {
		return 0
	}
	return int32(len(stmts.StmtDecisionIf) +
		len(stmts.StmtDecisionElseIf) +
		len(stmts.StmtLoop) +
		len(stmts.StmtDecisionCase) +
		len(stmts.StmtDecisionCatch) +
		len(stmts.StmtDecisionTernary) +
		len(stmts.StmtDecisionLogical))
}

func isFunctionBody(stmts *pb.Stmts, parents *pb.Stmts) bool {
	if stmts == nil || parents == nil {
		return false
	}

	for _, fn := range parents.StmtFunction {
		if fn != nil && fn.Stmts == stmts {
			return true
		}
	}

	return false
}
