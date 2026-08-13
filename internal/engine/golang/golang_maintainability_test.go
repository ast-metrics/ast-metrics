package golang

import (
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	enginePkg "github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/stretchr/testify/assert"
)

const proceduralGo = `package main

func A(x int) int { if x > 0 { x = x + 1 } else { x = x - 1 }; return x }
func B(y int) int { for i:=0;i<y;i++ { y += i }; return y }
`

func Test_FileLevel_Maintainability_And_Halstead_IsComputed_ForProceduralGo(t *testing.T) {
	r := &GolangRunner{}
	file, _ := enginePkg.CreateTestFileWithCode(r, proceduralGo)

	analyzer.AnalyzeFile(file)

	if file.Stmts.Analyze == nil {
		t.Fatalf("missing Analyze on file")
	}
	if file.Stmts.Analyze.Volume == nil {
		t.Fatalf("missing Volume on file")
	}
	if file.Stmts.Analyze.Volume.Loc == nil || file.Stmts.Analyze.Volume.Lloc == nil || file.Stmts.Analyze.Volume.Cloc == nil {
		t.Fatalf("missing LOC/LLOC/CLOC on file: %+v", file.Stmts.Analyze.Volume)
	}
	if file.Stmts.Analyze.Complexity == nil || file.Stmts.Analyze.Complexity.Cyclomatic == nil {
		t.Fatalf("missing Complexity on file: %+v", file.Stmts.Analyze.Complexity)
	}
	if file.Stmts.Analyze.Volume.HalsteadVolume == nil {
		t.Fatalf("missing HalsteadVolume on file")
	}
	if file.Stmts.Analyze.Maintainability == nil || file.Stmts.Analyze.Maintainability.MaintainabilityIndex == nil {
		t.Fatalf("expected file-level maintainability index to be computed")
	}

	mi := *file.Stmts.Analyze.Maintainability.MaintainabilityIndex
	if mi <= 0 {
		t.Fatalf("expected file-level MI > 0, got %v", mi)
	}
	if mi == 171 {
		t.Fatalf("file-level MI should not default to 171; got %v", mi)
	}

	if mi == 7 {
		t.Fatalf("file-level MI should not be the fallback 7; got %v", mi)
	}
}

func Test_FileLevel_Maintainability_SampleGo(t *testing.T) {
	r := &GolangRunner{}
	file, _ := enginePkg.CreateTestFileWithCode(r, sampleGo)
	analyzer.AnalyzeFile(file)

	if file.Stmts.Analyze == nil {
		t.Fatalf("missing Analyze on sampleGo file")
	}
	if file.Stmts.Analyze.Volume == nil {
		t.Fatalf("missing Volume on sampleGo file")
	}
	// Average of the struct C (whose only method M holds 17 distinct symbols:
	// 13 operators and 4 operands) and of the top-level function F (4 distinct
	// symbols): (17 + 4) / 2 = 10.5, so 11.
	if *file.Stmts.Analyze.Volume.HalsteadVocabulary != int32(11) {
		t.Fatalf("incorrect halstead vocabulary on file, got %d", *file.Stmts.Analyze.Volume.HalsteadVocabulary)
	}
	if file.Stmts.Analyze.Volume.Loc == nil || file.Stmts.Analyze.Volume.Lloc == nil || file.Stmts.Analyze.Volume.Cloc == nil {
		t.Fatalf("missing LOC/LLOC/CLOC on sampleGo file")
	}
	if file.Stmts.Analyze.Complexity == nil || file.Stmts.Analyze.Complexity.Cyclomatic == nil {
		t.Fatalf("missing Complexity on sampleGo file")
	}
	if file.Stmts.Analyze.Volume.HalsteadVolume == nil {
		t.Fatalf("missing HalsteadVolume on sampleGo file")
	}
	if file.Stmts.Analyze.Maintainability == nil || file.Stmts.Analyze.Maintainability.MaintainabilityIndex == nil {
		t.Fatalf("expected file-level maintainability index to be computed on sampleGo")
	}

	mi := *file.Stmts.Analyze.Maintainability.MaintainabilityIndex
	if mi <= 0 {
		t.Fatalf("expected MI > 0, got %v", mi)
	}
	if mi == 171 {
		t.Fatalf("MI should not be the constant 171 for non-empty file; got %v", mi)
	}
}

func Test_FileLevel_Loc_SampleGo(t *testing.T) {
	r := &GolangRunner{}
	file, _ := enginePkg.CreateTestFileWithCode(r, sampleGo)
	analyzer.AnalyzeFile(file)

	if file.Stmts.Analyze == nil {
		t.Fatalf("missing Analyze on sampleGo file")
	}
	if file.Stmts.Analyze.Volume == nil {
		t.Fatalf("missing Volume on sampleGo file")
	}
	if *file.Stmts.Analyze.Volume.Loc != int32(27) {
		t.Fatalf("incorrect Loc on sampleGo file, got %d", *file.Stmts.Analyze.Volume.Loc)
	}
	// statement lines: if, 2x Println, for, switch, return x, return z * 2
	if *file.Stmts.Analyze.Volume.Lloc != int32(7) {
		t.Fatalf("incorrect logical Loc on sampleGo file, got %d", *file.Stmts.Analyze.Volume.Lloc)
	}
	if *file.Stmts.Analyze.Volume.Cloc != int32(3) {
		t.Fatalf("incorrect comment Loc on sampleGo file, got %d", *file.Stmts.Analyze.Volume.Cloc)
	}

	// M has a receiver: it is a method of the struct C, not a function of the file
	if len(file.Stmts.StmtFunction) != 1 {
		t.Fatal("expected the top-level function F in gofile")
	}
	if len(file.Stmts.StmtClass) != 1 || len(file.Stmts.StmtClass[0].Stmts.StmtFunction) != 1 {
		t.Fatal("method M not attached to the struct C")
	}
	method := file.Stmts.StmtClass[0].Stmts.StmtFunction[0]
	assert.Equal(t, "M", method.Name.Short)
	expected := int32(1)
	if *method.Stmts.Analyze.Volume.Cloc != expected {
		t.Fatalf("incorrect comment lines of code for method, got %d", *method.Stmts.Analyze.Volume.Cloc)
	}

}
