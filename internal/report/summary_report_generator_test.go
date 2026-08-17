package report

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/ast-metrics/ast-metrics/internal/analyzer/requirement"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// fileWithRisk builds a file carrying the metrics the summary reads: a risk
// score, a maintainability index on its class, and a commit history.
func fileWithRisk(path string, risk float64, maintainability float64, commits int) *pb.File {
	history := make([]*pb.Commit, commits)
	for i := range history {
		history[i] = &pb.Commit{}
	}

	return &pb.File{
		Path: path,
		Stmts: &pb.Stmts{
			Analyze: &pb.Analyze{
				Risk: &pb.Risk{Score: risk},
			},
			StmtClass: []*pb.StmtClass{
				{
					Name: &pb.Name{Qualified: path + "\\Class"},
					Stmts: &pb.Stmts{
						Analyze: &pb.Analyze{
							Maintainability: &pb.Maintainability{
								MaintainabilityIndex: proto.Float64(maintainability),
							},
						},
					},
				},
			},
		},
		Commits: &pb.Commits{Count: int32(commits), Commits: history},
	}
}

func aggregatedForTest() analyzer.ProjectAggregated {
	combined := analyzer.Aggregated{
		NbFiles:     3,
		NbTestFiles: 1,
		NbMethods:   42,
		NbFunctions: 7,
		// Counter is what tells a real measurement from an aggregate the
		// analysis never fed, so every fixture carries one.
		// 3 files: 2 of production summing to 1200 lines, 1 test file of 450
		Loc:                           analyzer.AggregateResult{Sum: 1200, Counter: 2},
		TestLoc:                       analyzer.AggregateResult{Sum: 450, Counter: 1},
		Cloc:                          analyzer.AggregateResult{Sum: 300, Counter: 3},
		Lloc:                          analyzer.AggregateResult{Sum: 800, Counter: 3},
		LocPerClass:                   analyzer.AggregateResult{Avg: 47, Counter: 10},
		LocPerMethod:                  analyzer.AggregateResult{Avg: 8, Counter: 42},
		CyclomaticComplexity:          analyzer.AggregateResult{Sum: 155, Counter: 3},
		CyclomaticComplexityPerClass:  analyzer.AggregateResult{Avg: 9.12, Max: 61, Counter: 10},
		CyclomaticComplexityPerMethod: analyzer.AggregateResult{Avg: 4.21, Max: 38, Counter: 42},
		MaintainabilityIndex:          analyzer.AggregateResult{Avg: 71, Counter: 10},
		HalsteadBugs:                  analyzer.AggregateResult{Sum: 12.4, Avg: 0.4, Max: 2.1, Counter: 10},
		HalsteadVolume:                analyzer.AggregateResult{Avg: 1204, Max: 18903, Counter: 10},
		HalsteadDifficulty:            analyzer.AggregateResult{Avg: 14.2, Max: 96, Counter: 10},
		HalsteadEffort:                analyzer.AggregateResult{Avg: 18203, Counter: 10},
		HalsteadTime:                  analyzer.AggregateResult{Sum: 7200, Counter: 10},
		EfferentCoupling:              analyzer.AggregateResult{Avg: 4.1, Max: 38, Counter: 10},
		AfferentCoupling:              analyzer.AggregateResult{Avg: 3.7, Max: 121, Counter: 10},
		Instability:                   analyzer.AggregateResult{Avg: 0.53},
		Community: &analyzer.CommunityMetrics{
			CommunitiesCount: 12,
			MaxSize:          31,
			InternalShare:    0.82,
			Granularity:      analyzer.GranularityClass,
		},
		TestQuality: &analyzer.TestQualityMetrics{
			NbTestFiles:          4,
			NbProdClasses:        10,
			NbTestedClasses:      6,
			TraceabilityPct:      60,
			GlobalIsolationScore: 71,
			IsolationLabel:       "Semi-isolated",
			GodTests:             []analyzer.TestFileMetrics{{FilePath: "a_test.go"}},
			OrphanClasses:        []analyzer.OrphanClass{{ClassName: "Lonely"}},
		},
	}

	return analyzer.ProjectAggregated{
		Combined: combined,
		ByClass: analyzer.Aggregated{
			NbClasses:       10,
			MethodsPerClass: analyzer.AggregateResult{Avg: 4.2, Counter: 10},
			Lcom4PerClass:   analyzer.AggregateResult{Avg: 1.8, Counter: 10},
		},
	}
}

// TestSummaryLeadsWithWhatTheToolKnows checks that the metrics setting AST
// Metrics apart from a line counter are present and come before the raw
// counters, which is the whole point of the layout.
func TestSummaryLeadsWithWhatTheToolKnows(t *testing.T) {
	generator := &SummaryReportGenerator{}
	rendered := generator.Render([]*pb.File{}, aggregatedForTest())

	ordered := []string{"Maintainability", "Bug probability", "Coupling", "Complexity", "Size"}
	position := 0
	for _, section := range ordered {
		found := strings.Index(rendered[position:], section)
		if found < 0 {
			t.Fatalf("section %q is missing, or comes before %q:\n%s", section, ordered[0], rendered)
		}
		position += found
	}

	expected := []string{
		"71  (moderate)",     // maintainability index and its label
		"12.40",              // estimated delivered bugs
		"1204.00 / 18903.00", // Halstead volume
		"4.10 / 38.00",       // efferent coupling
		"0.53",               // instability
		"1.80",               // LCOM4
		"12",                 // communities
		"2.0 h",              // Halstead time, in hours rather than seconds
	}
	for _, value := range expected {
		if !strings.Contains(rendered, value) {
			t.Errorf("expected %q in the summary:\n%s", value, rendered)
		}
	}
}

// TestSummaryAccountsForEveryFileItCounted pins down what a bare "Lines of
// code" used to hide: the number covers the production files only, while the
// file count above it covers the whole tree. A reader comparing the total with
// cloc or scc on the same directory found a third of it missing and nothing to
// explain the gap.
//
// Both sides carry their scope in their own label, so the split is read rather
// than deduced, and the two numbers add up to the whole tree.
func TestSummaryAccountsForEveryFileItCounted(t *testing.T) {
	rendered := (&SummaryReportGenerator{}).Render([]*pb.File{}, aggregatedForTest())

	expected := []string{
		"production                       2",
		"test                             1",
		"Production lines of code (LOC)     1200",
		"Test lines of code (LOC)           450",
	}
	for _, value := range expected {
		if !strings.Contains(rendered, value) {
			t.Errorf("expected %q in the summary:\n%s", value, rendered)
		}
	}

	// nothing is left saying "lines of code" without saying whose
	if strings.Contains(rendered, "  Lines of code") {
		t.Errorf("a lines-of-code row must name the files it covers:\n%s", rendered)
	}
}

// TestSummaryStaysQuietWithoutTestFiles keeps the test row out of a project that
// has none, where it would only ever read zero.
func TestSummaryStaysQuietWithoutTestFiles(t *testing.T) {
	aggregated := aggregatedForTest()
	aggregated.Combined.NbTestFiles = 0
	aggregated.Combined.TestLoc = analyzer.AggregateResult{}

	rendered := (&SummaryReportGenerator{}).Render([]*pb.File{}, aggregated)
	if strings.Contains(rendered, "Test lines of code") {
		t.Errorf("the test LOC row should be absent without test files:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Production lines of code (LOC)") {
		t.Errorf("the production row stays, whatever the tests:\n%s", rendered)
	}
}

func TestSummaryReportsTestQuality(t *testing.T) {
	generator := &SummaryReportGenerator{}
	rendered := generator.Render([]*pb.File{}, aggregatedForTest())

	for _, value := range []string{"60.0%  (6 / 10)", "71  (Semi-isolated)"} {
		if !strings.Contains(rendered, value) {
			t.Errorf("expected %q in the summary:\n%s", value, rendered)
		}
	}
}

func TestSummaryListsTheRiskiestFilesFirst(t *testing.T) {
	files := []*pb.File{
		fileWithRisk("low.go", 0.10, 90, 1),
		fileWithRisk("worst.go", 0.92, 41, 12),
		fileWithRisk("middle.go", 0.50, 60, 4),
		fileWithRisk("untouched.go", 0, 95, 0),
	}

	generator := &SummaryReportGenerator{}
	rendered := generator.Render(files, aggregatedForTest())

	worst := strings.Index(rendered, "worst.go")
	middle := strings.Index(rendered, "middle.go")
	if worst < 0 || middle < 0 {
		t.Fatalf("expected the hotspots to be listed:\n%s", rendered)
	}
	if worst > middle {
		t.Errorf("expected the riskiest file first:\n%s", rendered)
	}

	if !strings.Contains(rendered, "MI 41, 12 commits") {
		t.Errorf("expected the maintainability and the churn of a hotspot:\n%s", rendered)
	}

	if strings.Contains(rendered, "untouched.go") {
		t.Errorf("a file without any risk should not be listed as a hotspot:\n%s", rendered)
	}

	// 2 of the 4 classes are below the refactoring threshold.
	if !strings.Contains(rendered, "Classes below the 65 threshold") || !strings.Contains(rendered, "2 of 4 measured") {
		t.Errorf("expected the count of classes to refactor:\n%s", rendered)
	}
}

func TestSummaryReportsTheLintVerdict(t *testing.T) {
	aggregated := aggregatedForTest()
	aggregated.Evaluation = &requirement.EvaluationResult{Succeeded: false}

	generator := &SummaryReportGenerator{}
	rendered := generator.Render([]*pb.File{}, aggregated)

	if !strings.Contains(rendered, "ast-metrics lint") {
		t.Errorf("a failed evaluation should point at the lint command:\n%s", rendered)
	}
}

// TestSummarySkipsWhatItDoesNotKnow covers a project analyzed without git,
// tests or dependency graph: a section with nothing to say disappears instead
// of printing zeroes that read like real measurements.
func TestSummarySkipsWhatItDoesNotKnow(t *testing.T) {
	generator := &SummaryReportGenerator{}
	rendered := generator.Render([]*pb.File{}, analyzer.ProjectAggregated{})

	for _, section := range []string{
		"Maintainability", "Bug probability", "Coupling", "Complexity",
		"Tests", "Hotspots", "Lint", "Languages",
	} {
		if strings.Contains(rendered, section) {
			t.Errorf("section %q should be skipped when nothing was measured:\n%s", section, rendered)
		}
	}

	if !strings.Contains(rendered, "Size") {
		t.Errorf("the counters are always known and should still be printed:\n%s", rendered)
	}
}

// TestSummaryNeverInventsAMeasurement is the rule the whole layout rests on: an
// aggregate the analysis never fed carries a counter of zero, and an average of
// zero would be read as a result rather than as a gap.
func TestSummaryNeverInventsAMeasurement(t *testing.T) {
	aggregated := aggregatedForTest()
	// LocPerClass is one of the aggregates the aggregator declares but never
	// fills in: it must not show up as a measured zero.
	aggregated.Combined.LocPerClass = analyzer.AggregateResult{}
	// A maximum that was never tracked stays at zero while the average is not:
	// only the average can be shown.
	aggregated.Combined.HalsteadEffort = analyzer.AggregateResult{Sum: 100, Avg: 50, Counter: 2}

	generator := &SummaryReportGenerator{}
	rendered := generator.Render([]*pb.File{}, aggregated)

	if strings.Contains(rendered, "LOC per class") {
		t.Errorf("an aggregate that was never measured must be left out:\n%s", rendered)
	}

	if !strings.Contains(rendered, "Effort (avg)") || strings.Contains(rendered, "Effort (avg / max)") {
		t.Errorf("a maximum that was never tracked must not be printed:\n%s", rendered)
	}

	// The same aggregate, once a maximum is tracked, shows both.
	aggregated.Combined.HalsteadEffort.Max = 90
	rendered = generator.Render([]*pb.File{}, aggregated)
	if !strings.Contains(rendered, "Effort (avg / max)") {
		t.Errorf("a tracked maximum should be printed:\n%s", rendered)
	}
}

func TestSummaryWritesToTheStreamAndCreatesNoFile(t *testing.T) {
	out := &strings.Builder{}
	generator := NewSummaryReportGenerator(out)

	reports, err := generator.Generate([]*pb.File{}, aggregatedForTest())
	if err != nil {
		t.Fatalf("did not expect an error, got: %v", err)
	}

	if len(reports) != 0 {
		t.Errorf("the summary produces no file, so it must report no generated report, got %d", len(reports))
	}

	if out.Len() == 0 {
		t.Error("nothing was written to the stream")
	}
}
