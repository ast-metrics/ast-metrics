package golang

import (
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	enginePkg "github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/stretchr/testify/assert"
)

func Test_Cyclomatic_Complexity_Is_Correct(t *testing.T) {
	src := sampleGo
	r := &GolangRunner{}
	file, _ := enginePkg.CreateTestFileWithCode(r, src)
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
