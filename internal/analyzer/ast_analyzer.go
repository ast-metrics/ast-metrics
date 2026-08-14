package analyzer

import (
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"

	Complexity "github.com/ast-metrics/ast-metrics/internal/analyzer/complexity"
	Component "github.com/ast-metrics/ast-metrics/internal/analyzer/component"
	Volume "github.com/ast-metrics/ast-metrics/internal/analyzer/volume"
	engine "github.com/ast-metrics/ast-metrics/internal/engine"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/pterm/pterm"
)

type analyzeJob struct {
	index int
	file  *pb.File
}

// AnalyzeFiles runs all metric visitors on pre-parsed in-memory files.
func AnalyzeFiles(parsedFiles []*pb.File, progressbar *pterm.SpinnerPrinter) []*pb.File {
	if len(parsedFiles) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	var nbDone atomic.Uint64
	total := len(parsedFiles)

	numWorkers := runtime.NumCPU()
	filesChan := make(chan analyzeJob, numWorkers)
	allResults := make([]*pb.File, total)

	for i := 0; i < numWorkers; i++ {
		go func() {
			for job := range filesChan {
				AnalyzeFile(job.file)

				nbDone.Add(1)
				if progressbar != nil {
					details := strconv.Itoa(int(nbDone.Load())) + "/" + strconv.Itoa(total)
					progressbar.UpdateText("Analyzing (" + details + ")")
				}
				// Each worker owns a distinct slot. This preserves the parser order
				// even though analysis completes concurrently.
				allResults[job.index] = job.file
				wg.Done()
			}
		}()
	}

	for i, file := range parsedFiles {
		wg.Add(1)
		filesChan <- analyzeJob{index: i, file: file}
	}

	close(filesChan)
	wg.Wait()

	if progressbar != nil {
		progressbar.Info("AST Analysis finished")
	}

	return allResults
}

func AnalyzeFile(file *pb.File) {
	root := &ASTNode{children: file.Stmts}

	// register visitors
	cyclomaticVisitor := &Complexity.CyclomaticComplexityVisitor{}
	root.Accept(cyclomaticVisitor)

	locVisitor := &Volume.LocVisitor{}
	root.Accept(locVisitor)

	halsteadVisitor := &Volume.HalsteadMetricsVisitor{}
	root.Accept(halsteadVisitor)

	lcomVisitor := &Component.LackOfCohesionOfMethodsVisitor{Language: file.ProgrammingLanguage}
	root.Accept(lcomVisitor)

	maintainabilityIndexVisitor := &Component.MaintainabilityIndexVisitor{}
	root.Accept(maintainabilityIndexVisitor)

	// visit AST
	root.Visit()

	// After visitors, ensure file-level Volume metrics exist and are coherent
	consolidateLoc(file)

	// Ensure structure is complete
	engine.EnsureNodeTypeIsComplete(file)

	// Recompute file cyclomatic complexity using classes plus functions
	// that are not attached to classes.
	recomputeFileCyclomatic(file)

	// Recompute Maintainability Index at file level after adjustments
	mi2 := &Component.MaintainabilityIndexVisitor{}
	mi2.Calculate(file.Stmts)
}

func recomputeFileCyclomatic(file *pb.File) {
	if file == nil || file.Stmts == nil {
		return
	}

	if file.Stmts.Analyze == nil {
		file.Stmts.Analyze = &pb.Analyze{}
	}
	if file.Stmts.Analyze.Complexity == nil {
		file.Stmts.Analyze.Complexity = &pb.Complexity{}
	}

	// The complexity of a file is what its outermost scopes add up to, plus the
	// branches written outside of any of them, as a script does. Calculate
	// walks that structure: a class already carries the complexity of its
	// methods and of the classes nested in it, so it is summed once and only
	// once, whereas summing the flattened list of everything the file declares
	// would count an inner class both for itself and inside its parent.
	//
	// Going through Calculate also keeps the file total from ever drifting from
	// the per-function numbers reported next to it: both come from the same
	// definition of the model.
	visitor := &Complexity.CyclomaticComplexityVisitor{}
	fileCyclomatic := visitor.Calculate(file.Stmts)

	file.Stmts.Analyze.Complexity.Cyclomatic = &fileCyclomatic
}

// consolidateLoc exposes the line counts of the file as its Volume metrics.
//
// The whole file is one range of source, measured once by the parser: its LOC,
// LLOC and CLOC are read off that measure and never summed from the functions
// it holds. Summing would ignore everything written outside a function (fields,
// top-level code, the comments between two methods) and would count a nested
// function twice.
func consolidateLoc(file *pb.File) {
	if file == nil || file.LinesOfCode == nil {
		return
	}
	if file.Stmts == nil {
		file.Stmts = &pb.Stmts{}
	}
	if file.Stmts.Analyze == nil {
		file.Stmts.Analyze = &pb.Analyze{}
	}
	if file.Stmts.Analyze.Volume == nil {
		file.Stmts.Analyze.Volume = &pb.Volume{}
	}
	file.Stmts.Analyze.Volume.Loc = &file.LinesOfCode.LinesOfCode
	file.Stmts.Analyze.Volume.Lloc = &file.LinesOfCode.LogicalLinesOfCode
	file.Stmts.Analyze.Volume.Cloc = &file.LinesOfCode.CommentLinesOfCode
}
