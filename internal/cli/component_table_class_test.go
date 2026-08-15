package cli

import (
	"testing"

	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewComponentTableClass(t *testing.T) {
	isInteractive := true
	files := []*pb.File{
		{
			ProgrammingLanguage: "Go",
		},
		{
			ProgrammingLanguage: "Python",
		},
	}

	table := NewComponentTableClass(isInteractive, files)

	if table.isInteractive != isInteractive {
		t.Errorf("Expected isInteractive to be %v, got %v", isInteractive, table.isInteractive)
	}

	if len(table.files) != len(files) {
		t.Errorf("Expected %d files, got %d", len(files), len(table.files))
	}

	for i, file := range table.files {
		if file.ProgrammingLanguage != files[i].ProgrammingLanguage {
			t.Errorf("Expected file at index %d to have ProgrammingLanguage %s, got %s", i, files[i].ProgrammingLanguage, file.ProgrammingLanguage)
		}
	}

	if table.sortColumnIndex != 0 {
		t.Errorf("Expected sortColumnIndex to be 0, got %d", table.sortColumnIndex)
	}
}

func TestComponentTableClass_Render(t *testing.T) {

	mi := float64(120)
	ccn := int32(5)
	loc := int32(100)
	halsteadLength := int32(100)
	halsteadVolume := float64(100)

	files := []*pb.File{
		{
			Path: "file1.php",
			Stmts: &pb.Stmts{
				StmtClass: []*pb.StmtClass{
					{
						Name: &pb.Name{
							Qualified: "ClassA",
							Short:     "ClassA",
						},
						Stmts: &pb.Stmts{
							Analyze: &pb.Analyze{
								Complexity: &pb.Complexity{
									Cyclomatic: &ccn,
								},
								Maintainability: &pb.Maintainability{
									MaintainabilityIndex: &mi,
								},
								Volume: &pb.Volume{
									Loc:            &loc,
									HalsteadLength: &halsteadLength,
									HalsteadVolume: &halsteadVolume,
								},
							},
						},
					},
				},
			},
		},
	}
	component := NewComponentTableClass(false, files)
	component.Init()

	rendered := component.Render()

	assert.Contains(t, rendered, "Use arrows to navigate and esc to quit", "Help is present")
	assert.Contains(t, rendered, "ClassA", "ClassA is present")
}

func TestComponentTableClassHandlesMissingClassMetrics(t *testing.T) {
	files := []*pb.File{{
		Path: "forward.hpp",
		Stmts: &pb.Stmts{
			StmtClass: []*pb.StmtClass{{
				Name:  &pb.Name{Qualified: "Forward", Short: "Forward"},
				Stmts: &pb.Stmts{Analyze: &pb.Analyze{}},
			}},
			StmtNamespace: []*pb.StmtNamespace{{
				Stmts: &pb.Stmts{StmtClass: []*pb.StmtClass{{
					Name:  &pb.Name{Qualified: "Forward", Short: "Forward"},
					Stmts: &pb.Stmts{Analyze: &pb.Analyze{}},
				}}},
			}},
		},
	}}

	component := NewComponentTableClass(false, files)
	require.Len(t, component.table.Rows(), 1, "namespace and file views of a class must not be duplicated")
	row := component.table.Rows()[0]
	require.Len(t, row, 9)
	assert.Equal(t, "Forward", row[0])
	assert.Equal(t, "0", row[4])
	assert.Equal(t, "0.00", row[7])
}

func TestComponentTableClass_Sort(t *testing.T) {

	// class 1
	mi1 := float64(120)
	ccn1 := int32(5)
	loc1 := int32(100)
	halsteadLength1 := int32(5)
	halsteadVolume := float64(7)

	//  class 2
	mi2 := float64(110)
	ccn2 := int32(10)
	loc2 := int32(80)
	halsteadLength2 := int32(7)

	files := []*pb.File{
		{
			Path: "file1.php",
			Stmts: &pb.Stmts{
				StmtClass: []*pb.StmtClass{
					{
						Name: &pb.Name{
							Qualified: "ClassA",
							Short:     "ClassA",
						},
						Stmts: &pb.Stmts{
							Analyze: &pb.Analyze{
								Complexity: &pb.Complexity{
									Cyclomatic: &ccn1,
								},
								Maintainability: &pb.Maintainability{
									MaintainabilityIndex: &mi1,
								},
								Volume: &pb.Volume{
									Loc:            &loc1,
									HalsteadLength: &halsteadLength1,
									HalsteadVolume: &halsteadVolume,
								},
							},
						},
					},
					{
						Name: &pb.Name{
							Qualified: "ClassB",
							Short:     "ClassB",
						},
						Stmts: &pb.Stmts{
							Analyze: &pb.Analyze{
								Complexity: &pb.Complexity{
									Cyclomatic: &ccn2,
								},
								Maintainability: &pb.Maintainability{
									MaintainabilityIndex: &mi2,
								},
								Volume: &pb.Volume{
									Loc:            &loc2,
									HalsteadLength: &halsteadLength2,
									HalsteadVolume: &halsteadVolume,
								},
							},
						},
					},
				},
			},
		},
	}
	component := NewComponentTableClass(false, files)
	component.Init()

	// Sort by maintainability index
	component.SortByMaintainabilityIndex()
	firstRow := component.table.Rows()[0]
	assert.Contains(t, firstRow[0], "ClassB", "ClassB is first")

	// Sort by name
	component.SortByName()
	firstRow = component.table.Rows()[0]
	assert.Contains(t, firstRow[0], "ClassA", "ClassA is first")

	// Sort by cyclomatic complexity
	component.SortByCyclomaticComplexity()
	firstRow = component.table.Rows()[0]
	assert.Contains(t, firstRow[0], "ClassB", "ClassB is first")

	// Sort by number of methods
	component.SortByNumberOfMethods()
	firstRow = component.table.Rows()[0]
	assert.Contains(t, firstRow[0], "ClassB", "ClassB is first")

	// Sort again by name
	component.SortByName()
	firstRow = component.table.Rows()[0]
	assert.Contains(t, firstRow[0], "ClassA", "ClassA is first")

}
