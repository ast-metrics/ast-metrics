package analyzer

import (
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/golang"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/stretchr/testify/assert"
)

// The visitor publishes the counts the parser measured; it computes none of
// them. These tests check that each scope ends up exposing its own measure,
// unchanged.
func TestLocVisitorPublishesTheMeasureOfEachScope(t *testing.T) {
	visitor := LocVisitor{}

	stmts := pb.Stmts{
		StmtFunction: []*pb.StmtFunction{
			{
				LinesOfCode: &pb.LinesOfCode{
					LinesOfCode:        10,
					LogicalLinesOfCode: 20,
					CommentLinesOfCode: 30,
				},
				Stmts: &pb.Stmts{Analyze: &pb.Analyze{Volume: &pb.Volume{}}},
			},
		},
		StmtClass: []*pb.StmtClass{
			{
				LinesOfCode: &pb.LinesOfCode{
					LinesOfCode:        40,
					LogicalLinesOfCode: 50,
					CommentLinesOfCode: 60,
				},
				Stmts: &pb.Stmts{},
			},
		},
	}

	visitor.Visit(&stmts, nil)

	fn := stmts.StmtFunction[0].Stmts.Analyze.Volume
	assert.Equal(t, int32(10), fn.GetLoc())
	assert.Equal(t, int32(20), fn.GetLloc())
	assert.Equal(t, int32(30), fn.GetCloc())

	// a class exposes its own measure too, and the Volume is created for it
	class := stmts.StmtClass[0].Stmts.Analyze.Volume
	assert.Equal(t, int32(40), class.GetLoc())
	assert.Equal(t, int32(50), class.GetLloc())
	assert.Equal(t, int32(60), class.GetCloc())
}

// TestLocVisitorDoesNotSumTheScopesItVisits is the regression that matters: the
// visitor used to add up the functions of a scope, so a class came out as the
// total of its methods and the file as the total of its functions. A scope
// already knows its own size.
func TestLocVisitorDoesNotSumTheScopesItVisits(t *testing.T) {
	visitor := LocVisitor{}

	stmts := pb.Stmts{
		StmtClass: []*pb.StmtClass{
			{
				LinesOfCode: &pb.LinesOfCode{LinesOfCode: 12, LogicalLinesOfCode: 4},
				Stmts: &pb.Stmts{
					StmtFunction: []*pb.StmtFunction{
						{
							LinesOfCode: &pb.LinesOfCode{LinesOfCode: 5, LogicalLinesOfCode: 2},
							Stmts:       &pb.Stmts{},
						},
						{
							LinesOfCode: &pb.LinesOfCode{LinesOfCode: 5, LogicalLinesOfCode: 2},
							Stmts:       &pb.Stmts{},
						},
					},
				},
			},
		},
	}

	visitor.Visit(&stmts, nil)

	class := stmts.StmtClass[0]
	assert.Equal(t, int32(12), class.Stmts.Analyze.Volume.GetLoc(),
		"the class keeps its own 12 lines, not the 10 of its two methods")
	assert.Equal(t, int32(4), class.Stmts.Analyze.Volume.GetLloc())
}

func TestLocVisitorOnAParsedFile(t *testing.T) {
	fileContent := `package main

import "fmt"

func example() {
    if true {
        if true {
            fmt.Println("Hello")
        }
    } else if true {
        fmt.Println("Hello")
    } else {
        fmt.Println("Hello")
    }
}
`

	parser := &golang.GolangRunner{}
	pbFile, err := engine.CreateTestFileWithCode(parser, fileContent)
	assert.Nil(t, err)

	visitor := LocVisitor{}
	visitor.Visit(pbFile.Stmts, nil)

	fn := pbFile.Stmts.StmtFunction[0]
	// the function spans its declaration line down to its closing brace
	assert.Equal(t, int32(11), fn.Stmts.Analyze.Volume.GetLoc())
	// if, nested if, the three calls, and the else-if
	assert.Equal(t, int32(6), fn.Stmts.Analyze.Volume.GetLloc())
}
