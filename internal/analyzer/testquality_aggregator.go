package analyzer

import (
	"encoding/json"
	"math"
	"path/filepath"
	"sort"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// TopTestQualityListSize is how many god tests and orphan classes a report
// shows. It is a display budget, not an analysis one: the lists below are
// complete, so a lint rule sees every violation and a fixed one never promotes
// the next offender into a "new" violation.
const TopTestQualityListSize = 20

// Top returns the head of a list, for a caller that displays it.
func Top[T any](items []T) []T {
	if len(items) > TopTestQualityListSize {
		return items[:TopTestQualityListSize]
	}
	return items
}

// TestQualityMetrics holds all computed KPIs for test quality analysis
type TestQualityMetrics struct {
	GlobalIsolationScore float64
	IsolationLabel       string
	TestFiles            []TestFileMetrics
	// GodTests and OrphanClasses hold every offender, worst first. Use Top to
	// cut them down when displaying.
	GodTests           []TestFileMetrics
	OrphanClasses      []OrphanClass
	ProdClassCoverage  []ProdClassCoverage
	IsolationHistogram [5]int // bins: 0-19, 20-39, 40-59, 60-79, 80-100
	NbTestFiles        int
	NbProdFiles        int
	NbProdClasses      int
	NbTestedClasses    int
	TraceabilityPct    float64 // percentage of prod classes covered by at least one test
}

// TestFileMetrics holds per-test-file metrics
type TestFileMetrics struct {
	FilePath       string
	ShortPath      string
	SUTFanOut      int     // number of distinct prod classes touched
	MaxDepth       int     // max BFS depth through prod class dependency chains
	IsolationScore float64 // 0-100
	IsolationLabel string  // Isolated, Semi-isolated, Coupled
}

// ProdClassCoverage holds per-prod-class test coverage metrics
type ProdClassCoverage struct {
	ClassName  string
	FilePath   string
	TestCount  int
	Complexity int32
	Efferent   int32
	Afferent   int32
}

// OrphanClass is a production class with zero tests, weighted by importance
type OrphanClass struct {
	ClassName  string
	FilePath   string
	Complexity int32
	Efferent   int32
	Afferent   int32
	Weight     float64
}

// TestQualityAggregator computes test quality metrics
type TestQualityAggregator struct{}

func NewTestQualityAggregator() *TestQualityAggregator {
	return &TestQualityAggregator{}
}

func (tqa *TestQualityAggregator) Calculate(aggregate *Aggregated) {
	if aggregate == nil {
		return
	}

	metrics := &TestQualityMetrics{}

	// 1. Partition files into test and prod
	var testFiles, prodFiles []*pb.File
	for _, f := range aggregate.ConcernedFiles {
		if f == nil {
			continue
		}
		if f.GetIsTest() {
			testFiles = append(testFiles, f)
		} else {
			prodFiles = append(prodFiles, f)
		}
	}

	metrics.NbTestFiles = len(testFiles)
	metrics.NbProdFiles = len(prodFiles)

	if len(testFiles) == 0 && len(prodFiles) == 0 {
		aggregate.TestQuality = metrics
		return
	}

	// 2. Build prod structure index: qualifiedName -> structure info
	// Includes both classes and top-level functions (for functional codebases like TS/JS)
	type prodStructInfo struct {
		class    *pb.StmtClass    // non-nil for classes
		function *pb.StmtFunction // non-nil for standalone functions
		filePath string
	}
	prodClassIndex := make(map[string]*prodStructInfo)
	// Index classes
	for _, f := range prodFiles {
		classes := engine.GetClassesInFile(f)
		for _, c := range classes {
			if c == nil || c.Name == nil {
				continue
			}
			qName := c.Name.GetQualified()
			if qName == "" {
				qName = c.Name.GetShort()
			}
			if qName == "" {
				continue
			}
			prodClassIndex[qName] = &prodStructInfo{class: c, filePath: f.Path}
		}
		// Index top-level functions (not inside classes)
		funcs := engine.GetFunctionsOutsideClassesInFile(f)
		for _, fn := range funcs {
			if fn == nil || fn.Name == nil {
				continue
			}
			qName := fn.Name.GetQualified()
			if qName == "" {
				qName = fn.Name.GetShort()
			}
			if qName == "" {
				continue
			}
			// Don't overwrite a class entry with a function entry
			if _, exists := prodClassIndex[qName]; !exists {
				prodClassIndex[qName] = &prodStructInfo{function: fn, filePath: f.Path}
			}
		}
	}

	metrics.NbProdClasses = len(prodClassIndex)

	// 3. Build dependency graph among prod classes for BFS depth calculation
	// prodClass -> set of prod classes it depends on
	prodClassDeps := make(map[string]map[string]struct{})
	for _, f := range prodFiles {
		deps := engine.GetDependenciesInFile(f)
		classes := engine.GetClassesInFile(f)
		for _, c := range classes {
			if c == nil || c.Name == nil {
				continue
			}
			fromName := c.Name.GetQualified()
			if fromName == "" {
				fromName = c.Name.GetShort()
			}
			if fromName == "" {
				continue
			}
			if prodClassDeps[fromName] == nil {
				prodClassDeps[fromName] = make(map[string]struct{})
			}
		}
		for _, dep := range deps {
			if dep == nil {
				continue
			}
			depName := dep.GetClassName()
			if depName == "" {
				depName = dep.GetNamespace()
			}
			// Find which class in this file owns this dependency
			fromName := dep.GetFrom()
			if fromName == "" {
				continue
			}
			if _, isProd := prodClassIndex[depName]; isProd {
				if prodClassDeps[fromName] == nil {
					prodClassDeps[fromName] = make(map[string]struct{})
				}
				prodClassDeps[fromName][depName] = struct{}{}
			}
		}
	}

	// 4. For each test file: collect dependencies, match against prod class index
	testClassCoverage := make(map[string]int) // prodClassName -> count of test files touching it
	var allTestMetrics []TestFileMetrics

	for _, tf := range testFiles {
		deps := engine.GetDependenciesInFile(tf)
		touchedProdClasses := make(map[string]struct{})

		for _, dep := range deps {
			if dep == nil {
				continue
			}
			depName := dep.GetClassName()
			if depName == "" {
				depName = dep.GetNamespace()
			}
			if _, isProd := prodClassIndex[depName]; isProd {
				touchedProdClasses[depName] = struct{}{}
			}
		}

		fanOut := len(touchedProdClasses)

		// Compute MaxDepth via BFS through prod class dependency chains (max 4 levels)
		maxDepth := 0
		if fanOut > 0 {
			maxDepth = bfsMaxDepth(touchedProdClasses, prodClassDeps, 4)
		}

		// IsolationScore = clamp(100 - fanOut*10 - depth*5, 0, 100)
		score := float64(100 - fanOut*10 - maxDepth*5)
		score = math.Max(0, math.Min(100, score))

		label := isolationLabel(score)

		shortPath := tf.GetShortPath()
		if shortPath == "" {
			shortPath = filepath.Base(tf.GetPath())
		}

		tfm := TestFileMetrics{
			FilePath:       tf.GetPath(),
			ShortPath:      shortPath,
			SUTFanOut:      fanOut,
			MaxDepth:       maxDepth,
			IsolationScore: score,
			IsolationLabel: label,
		}
		allTestMetrics = append(allTestMetrics, tfm)

		// Track coverage
		for className := range touchedProdClasses {
			testClassCoverage[className]++
		}
	}

	sort.Slice(allTestMetrics, func(i, j int) bool {
		if allTestMetrics[i].FilePath != allTestMetrics[j].FilePath {
			return allTestMetrics[i].FilePath < allTestMetrics[j].FilePath
		}
		return allTestMetrics[i].ShortPath < allTestMetrics[j].ShortPath
	})
	metrics.TestFiles = allTestMetrics

	// 5. Build ProdClassCoverage
	var prodCoverage []ProdClassCoverage
	testedCount := 0
	for qName, info := range prodClassIndex {
		count := testClassCoverage[qName]
		var complexity int32
		var efferent, afferent int32

		// Extract metrics from class or function
		var stmts *pb.Stmts
		if info.class != nil {
			stmts = info.class.Stmts
		} else if info.function != nil {
			stmts = info.function.Stmts
		}
		if stmts != nil && stmts.Analyze != nil {
			if stmts.Analyze.Complexity != nil && stmts.Analyze.Complexity.Cyclomatic != nil {
				complexity = *stmts.Analyze.Complexity.Cyclomatic
			}
			if stmts.Analyze.Coupling != nil {
				efferent = stmts.Analyze.Coupling.Efferent
				afferent = stmts.Analyze.Coupling.Afferent
			}
		}

		prodCoverage = append(prodCoverage, ProdClassCoverage{
			ClassName:  qName,
			FilePath:   info.filePath,
			TestCount:  count,
			Complexity: complexity,
			Efferent:   efferent,
			Afferent:   afferent,
		})

		if count > 0 {
			testedCount++
		}
	}
	sort.Slice(prodCoverage, func(i, j int) bool {
		if prodCoverage[i].ClassName != prodCoverage[j].ClassName {
			return prodCoverage[i].ClassName < prodCoverage[j].ClassName
		}
		return prodCoverage[i].FilePath < prodCoverage[j].FilePath
	})
	metrics.ProdClassCoverage = prodCoverage
	metrics.NbTestedClasses = testedCount
	if len(prodClassIndex) > 0 {
		metrics.TraceabilityPct = float64(testedCount) / float64(len(prodClassIndex)) * 100
	}

	// 6. God Tests: fan-out >= 5, worst first
	var godTests []TestFileMetrics
	for _, t := range allTestMetrics {
		if t.SUTFanOut >= 5 {
			godTests = append(godTests, t)
		}
	}
	sort.Slice(godTests, func(i, j int) bool {
		if godTests[i].SUTFanOut != godTests[j].SUTFanOut {
			return godTests[i].SUTFanOut > godTests[j].SUTFanOut
		}
		if godTests[i].FilePath != godTests[j].FilePath {
			return godTests[i].FilePath < godTests[j].FilePath
		}
		return godTests[i].ShortPath < godTests[j].ShortPath
	})
	metrics.GodTests = godTests

	// 7. Orphan Classes: TestCount == 0, sorted by weight desc
	var orphans []OrphanClass
	for _, pc := range prodCoverage {
		if pc.TestCount == 0 {
			weight := float64(pc.Complexity) * (1 + float64(pc.Efferent) + float64(pc.Afferent))
			if weight < 1 {
				weight = 1
			}
			orphans = append(orphans, OrphanClass{
				ClassName:  pc.ClassName,
				FilePath:   pc.FilePath,
				Complexity: pc.Complexity,
				Efferent:   pc.Efferent,
				Afferent:   pc.Afferent,
				Weight:     weight,
			})
		}
	}
	sort.Slice(orphans, func(i, j int) bool {
		if orphans[i].Weight != orphans[j].Weight {
			return orphans[i].Weight > orphans[j].Weight
		}
		if orphans[i].ClassName != orphans[j].ClassName {
			return orphans[i].ClassName < orphans[j].ClassName
		}
		return orphans[i].FilePath < orphans[j].FilePath
	})
	metrics.OrphanClasses = orphans

	// 8. Compute global isolation score (avg) and histogram
	if len(allTestMetrics) > 0 {
		sum := 0.0
		for _, t := range allTestMetrics {
			sum += t.IsolationScore
			bin := int(t.IsolationScore / 20)
			if bin > 4 {
				bin = 4
			}
			metrics.IsolationHistogram[bin]++
		}
		metrics.GlobalIsolationScore = sum / float64(len(allTestMetrics))
	}
	metrics.IsolationLabel = isolationLabel(metrics.GlobalIsolationScore)

	aggregate.TestQuality = metrics
}

// bfsMaxDepth performs BFS from the initial set of prod classes through their dependencies,
// returning the maximum depth reached (capped at maxLevels).
func bfsMaxDepth(initial map[string]struct{}, deps map[string]map[string]struct{}, maxLevels int) int {
	visited := make(map[string]struct{})
	current := make(map[string]struct{})
	for k := range initial {
		current[k] = struct{}{}
		visited[k] = struct{}{}
	}

	depth := 0
	for level := 0; level < maxLevels; level++ {
		next := make(map[string]struct{})
		for node := range current {
			for dep := range deps[node] {
				if _, seen := visited[dep]; !seen {
					visited[dep] = struct{}{}
					next[dep] = struct{}{}
				}
			}
		}
		if len(next) == 0 {
			break
		}
		depth = level + 1
		current = next
	}
	return depth
}

func isolationLabel(score float64) string {
	if score >= 80 {
		return "Isolated"
	}
	if score >= 50 {
		return "Semi-isolated"
	}
	return "Coupled"
}

// testQualityJSON is the JSON-serializable representation of TestQualityMetrics
type testQualityJSON struct {
	GlobalIsolationScore float64           `json:"globalIsolationScore"`
	IsolationLabel       string            `json:"isolationLabel"`
	NbTestFiles          int               `json:"nbTestFiles"`
	NbProdFiles          int               `json:"nbProdFiles"`
	NbProdClasses        int               `json:"nbProdClasses"`
	NbTestedClasses      int               `json:"nbTestedClasses"`
	TraceabilityPct      float64           `json:"traceabilityPct"`
	IsolationHistogram   [5]int            `json:"isolationHistogram"`
	TestFiles            []testFileJSON    `json:"testFiles"`
	GodTests             []godTestJSON     `json:"godTests"`
	OrphanClasses        []orphanClassJSON `json:"orphanClasses"`
}

type testFileJSON struct {
	FilePath       string  `json:"filePath"`
	ShortPath      string  `json:"shortPath"`
	FanOut         int     `json:"fanOut"`
	MaxDepth       int     `json:"maxDepth"`
	IsolationScore float64 `json:"isolationScore"`
	IsolationLabel string  `json:"isolationLabel"`
}

type godTestJSON struct {
	ShortPath      string  `json:"shortPath"`
	FanOut         int     `json:"fanOut"`
	IsolationScore float64 `json:"isolationScore"`
	IsolationLabel string  `json:"isolationLabel"`
}

type orphanClassJSON struct {
	ClassName  string  `json:"className"`
	FilePath   string  `json:"filePath"`
	Complexity int32   `json:"complexity"`
	Efferent   int32   `json:"efferent"`
	Afferent   int32   `json:"afferent"`
	Weight     float64 `json:"weight"`
}

// sanitizeFloat replaces NaN/Inf with 0 and rounds to 2 decimal places
func sanitizeFloat(f float64) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return math.Round(f*100) / 100
}

// BuildTestQualityJSON produces a JSON string for the test quality template
func BuildTestQualityJSON(tq *TestQualityMetrics) string {
	if tq == nil {
		return "{}"
	}

	testFiles := make([]testFileJSON, len(tq.TestFiles))
	for i, t := range tq.TestFiles {
		testFiles[i] = testFileJSON{
			FilePath:       t.FilePath,
			ShortPath:      t.ShortPath,
			FanOut:         t.SUTFanOut,
			MaxDepth:       t.MaxDepth,
			IsolationScore: sanitizeFloat(t.IsolationScore),
			IsolationLabel: t.IsolationLabel,
		}
	}

	// The report shows the head of each list, the metrics keep them whole.
	topGodTests := Top(tq.GodTests)
	godTests := make([]godTestJSON, len(topGodTests))
	for i, t := range topGodTests {
		godTests[i] = godTestJSON{
			ShortPath:      t.ShortPath,
			FanOut:         t.SUTFanOut,
			IsolationScore: sanitizeFloat(t.IsolationScore),
			IsolationLabel: t.IsolationLabel,
		}
	}

	topOrphans := Top(tq.OrphanClasses)
	orphans := make([]orphanClassJSON, len(topOrphans))
	for i, o := range topOrphans {
		orphans[i] = orphanClassJSON{
			ClassName:  o.ClassName,
			FilePath:   o.FilePath,
			Complexity: o.Complexity,
			Efferent:   o.Efferent,
			Afferent:   o.Afferent,
			Weight:     sanitizeFloat(o.Weight),
		}
	}

	data := testQualityJSON{
		GlobalIsolationScore: sanitizeFloat(tq.GlobalIsolationScore),
		IsolationLabel:       tq.IsolationLabel,
		NbTestFiles:          tq.NbTestFiles,
		NbProdFiles:          tq.NbProdFiles,
		NbProdClasses:        tq.NbProdClasses,
		NbTestedClasses:      tq.NbTestedClasses,
		TraceabilityPct:      sanitizeFloat(tq.TraceabilityPct),
		IsolationHistogram:   tq.IsolationHistogram,
		TestFiles:            testFiles,
		GodTests:             godTests,
		OrphanClasses:        orphans,
	}

	b, err := json.Marshal(data)
	if err != nil {
		return "{}"
	}
	return string(b)
}
