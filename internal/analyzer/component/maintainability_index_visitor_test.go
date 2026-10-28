package analyzer

import (
	"testing"

	pb "github.com/ast-metrics/ast-metrics/pb"
)

func TestMaintainabilityIndexVisitor_Visit_NilStmts(t *testing.T) {
	visitor := &MaintainabilityIndexVisitor{}
	visitor.Visit(nil, nil)
	// Should not panic
}

func TestMaintainabilityIndexVisitor_Visit_InClass(t *testing.T) {
	stmts := &pb.Stmts{}
	parents := &pb.Stmts{
		StmtClass: []*pb.StmtClass{
			{Stmts: stmts},
		},
	}

	visitor := &MaintainabilityIndexVisitor{}
	visitor.Visit(stmts, parents)

	if !visitor.inClass {
		t.Error("expected inClass to be true")
	}
}

func TestMaintainabilityIndexVisitor_Visit_NotInClass(t *testing.T) {
	stmts := &pb.Stmts{}
	parents := &pb.Stmts{}

	visitor := &MaintainabilityIndexVisitor{}
	visitor.Visit(stmts, parents)

	if visitor.inClass {
		t.Error("expected inClass to be false")
	}
}

func TestMaintainabilityIndexVisitor_LeaveNode(t *testing.T) {
	visitor := &MaintainabilityIndexVisitor{}
	stmts := &pb.Stmts{}

	visitor.LeaveNode(stmts)
	// Should not panic
}

func TestMaintainabilityIndexUndefinedForZeroLogicalLines(t *testing.T) {
	loc := int32(4)
	lloc := int32(0)
	cloc := int32(0)
	cyclomatic := int32(1)
	halsteadVolume := float64(2)
	stmts := &pb.Stmts{Analyze: &pb.Analyze{
		Volume: &pb.Volume{
			Loc:            &loc,
			Lloc:           &lloc,
			Cloc:           &cloc,
			HalsteadVolume: &halsteadVolume,
		},
		Complexity: &pb.Complexity{Cyclomatic: &cyclomatic},
	}}

	(&MaintainabilityIndexVisitor{}).Calculate(stmts)

	if metrics := stmts.Analyze.Maintainability; metrics != nil && metrics.MaintainabilityIndex != nil {
		t.Fatalf("zero executable logical lines should leave maintainability undefined, got %g", *metrics.MaintainabilityIndex)
	}
}

func TestItCalculateMaintainabilityIndex(t *testing.T) {

	visitor := MaintainabilityIndexVisitor{}

	stmts := pb.Stmts{}
	class1 := pb.StmtClass{}
	class1.Stmts = &pb.Stmts{}
	stmts.StmtClass = append(stmts.StmtClass, &class1)

	stmts.Analyze = &pb.Analyze{}
	stmts.Analyze.Volume = &pb.Volume{}

	loc := int32(10)
	lloc := int32(8)
	cloc := int32(2)
	cyclomatic := int32(3)
	halsteadVolume := float64(10)

	stmts.Analyze.Volume.Loc = &loc
	stmts.Analyze.Volume.Lloc = &lloc
	stmts.Analyze.Volume.Cloc = &cloc
	stmts.Analyze.Complexity = &pb.Complexity{}
	stmts.Analyze.Complexity.Cyclomatic = &cyclomatic
	stmts.Analyze.Volume.HalsteadVolume = &halsteadVolume

	visitor.Calculate(&stmts)

	MI := int(*stmts.Analyze.Maintainability.MaintainabilityIndex)
	MIwoc := int(*stmts.Analyze.Maintainability.MaintainabilityIndexWithoutComments)
	commentWeight := int(*stmts.Analyze.Maintainability.CommentWeight)

	if MI != 104 {
		t.Error("Expected 104, got ", MI)
	}

	if MIwoc != 72 {
		t.Error("Expected 72, got ", MIwoc)
	}

	if commentWeight != 31 {
		t.Error("Expected 31, got ", commentWeight)
	}
}
