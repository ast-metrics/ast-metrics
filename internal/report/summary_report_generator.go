package report

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// maintainabilityRefactorThreshold is the Maintainability Index below which a
// class is worth refactoring first. It matches the threshold already used to
// build the refactoring suggestions.
const maintainabilityRefactorThreshold = 65

// nbTopRisks is how many hotspots the summary lists. Enough to act on, short
// enough to stay readable in a terminal or a CI log.
const nbTopRisks = 5

// SummaryReportGenerator writes a plain-text summary of the analysis.
//
// It is the report you get when you ask for no other one: unlike the other
// generators it writes to a stream instead of a file, so that a run always
// produces something readable, in a terminal as well as in a CI log, without
// creating a file nobody asked for.
//
// The sections are ordered by what AST Metrics knows that a line counter does
// not: maintainability, bug probability and coupling come first, raw counters
// last.
type SummaryReportGenerator struct {
	Writer io.Writer
}

func NewSummaryReportGenerator(writer io.Writer) Reporter {
	return &SummaryReportGenerator{Writer: writer}
}

// Generate prints the summary. It returns no GeneratedReport: nothing durable
// has been produced, so the views listing the generated files stay untouched.
func (v *SummaryReportGenerator) Generate(files []*pb.File, projectAggregated analyzer.ProjectAggregated) ([]GeneratedReport, error) {
	if v.Writer == nil {
		return nil, nil
	}

	if _, err := io.WriteString(v.Writer, v.Render(files, projectAggregated)); err != nil {
		return nil, err
	}

	return nil, nil
}

// Render builds the summary. It is kept separate from Generate so the layout
// can be asserted in tests without going through a stream.
func (v *SummaryReportGenerator) Render(files []*pb.File, projectAggregated analyzer.ProjectAggregated) string {
	combined := projectAggregated.Combined
	byClass := projectAggregated.ByClass

	out := &strings.Builder{}
	out.WriteString("\n")

	section(out, "Maintainability", func(body *strings.Builder) {
		if combined.MaintainabilityIndex.Counter > 0 {
			row(body, "Maintainability index", fmt.Sprintf("%.0f  (%s)",
				combined.MaintainabilityIndex.Avg, maintainabilityLabel(combined.MaintainabilityIndex.Avg)))
		}
		metric(body, "  without comments", combined.MaintainabilityIndexWithoutComments)
		metric(body, "  comment weight", combined.MaintainabilityCommentWeight)

		nbHardToMaintain, nbMeasuredClasses := countClassesToRefactor(files)
		if nbMeasuredClasses > 0 {
			row(body, fmt.Sprintf("Classes below the %d threshold", maintainabilityRefactorThreshold),
				fmt.Sprintf("%d of %d measured", nbHardToMaintain, nbMeasuredClasses))
		}
	})

	section(out, "Bug probability (Halstead)", func(body *strings.Builder) {
		if combined.HalsteadBugs.Counter > 0 {
			row(body, "Estimated delivered bugs", decimal(combined.HalsteadBugs.Sum))
		}
		metric(body, "  per class", combined.HalsteadBugs)
		metric(body, "Volume", combined.HalsteadVolume)
		metric(body, "Difficulty", combined.HalsteadDifficulty)
		metric(body, "Effort", combined.HalsteadEffort)
		if combined.HalsteadTime.Counter > 0 {
			// Halstead time is expressed in seconds, which says nothing at this
			// scale.
			row(body, "Estimated time to write (total)", fmt.Sprintf("%.1f h", combined.HalsteadTime.Sum/3600))
		}
	})

	section(out, "Coupling", func(body *strings.Builder) {
		metric(body, "Efferent coupling", combined.EfferentCoupling)
		metric(body, "Afferent coupling", combined.AfferentCoupling)
		// Instability is derived from the coupling sums, so it carries no
		// counter of its own: it is measured exactly when the coupling is.
		if combined.EfferentCoupling.Counter > 0 {
			row(body, "Instability (avg)", decimal(combined.Instability.Avg))
		}
		metric(body, "Lack of cohesion, LCOM4", byClass.Lcom4PerClass)
		if combined.Community != nil && combined.Community.CommunitiesCount > 0 {
			unit := "classes"
			if combined.Community.Granularity == analyzer.GranularityNamespace {
				unit = "packages"
			}
			row(body, "Communities detected", integer(combined.Community.CommunitiesCount))
			row(body, "  largest one", fmt.Sprintf("%d %s", combined.Community.MaxSize, unit))
			row(body, "  dependencies kept inside", fmt.Sprintf("%d%%", int(combined.Community.InternalShare*100+0.5)))
			if n := len(combined.Community.Cycles); n > 0 {
				row(body, "  cycles between communities", integer(n))
			}
			if combined.Community.HistoryAvailable && combined.Community.HistoryCommits > 0 {
				row(body, "  commits agreeing with them", fmt.Sprintf("%d%%", int(combined.Community.HistoryAgreement*100+0.5)))
			}
		}
	})

	section(out, "Complexity", func(body *strings.Builder) {
		if combined.CyclomaticComplexity.Counter > 0 {
			row(body, "Cyclomatic complexity (total)", integer(int(combined.CyclomaticComplexity.Sum)))
		}
		metric(body, "  per class", combined.CyclomaticComplexityPerClass)
		metric(body, "  per method", combined.CyclomaticComplexityPerMethod)
	})

	section(out, "Size", func(body *strings.Builder) {
		// Production and test lines are two labelled rows next to each other, so
		// that the split is read off the labels rather than explained in a note,
		// and so that the two add up to what a line counter reports for the same
		// directory. Everything else in this report describes production code,
		// which is why only the lines of the test files appear at all.
		row(body, "Files", integer(combined.NbFiles))
		row(body, "  production", integer(combined.NbFiles-combined.NbTestFiles))
		row(body, "  test", integer(combined.NbTestFiles))
		total(body, "Production lines of code (LOC)", combined.Loc)
		total(body, "  comment lines (CLOC)", combined.Cloc)
		total(body, "  logical lines (LLOC)", combined.Lloc)
		total(body, "Test lines of code (LOC)", combined.TestLoc)
		row(body, "Classes", integer(byClass.NbClasses))
		row(body, "Methods", integer(combined.NbMethods))
		row(body, "Functions", integer(combined.NbFunctions))
		metric(body, "LOC per class", combined.LocPerClass)
		metric(body, "LOC per method", combined.LocPerMethod)
		metric(body, "Methods per class", byClass.MethodsPerClass)
	})

	renderLanguages(out, projectAggregated)
	renderTestQuality(out, combined)
	renderRisks(out, files)
	renderLint(out, projectAggregated)

	out.WriteString("\n")

	return out.String()
}

// renderLanguages lists one line per language. It is skipped on a single
// language project, where it would only repeat the totals above.
func renderLanguages(out *strings.Builder, projectAggregated analyzer.ProjectAggregated) {
	if len(projectAggregated.ByProgrammingLanguage) < 2 {
		return
	}

	names := make([]string, 0, len(projectAggregated.ByProgrammingLanguage))
	for name := range projectAggregated.ByProgrammingLanguage {
		names = append(names, name)
	}
	// Biggest language first, so the main one is at the top.
	sort.Slice(names, func(i, j int) bool {
		return projectAggregated.ByProgrammingLanguage[names[i]].Loc.Sum >
			projectAggregated.ByProgrammingLanguage[names[j]].Loc.Sum
	})

	section(out, "Languages", func(body *strings.Builder) {
		for _, name := range names {
			language := projectAggregated.ByProgrammingLanguage[name]
			row(body, name, fmt.Sprintf("%s LOC, MI %.0f, %.1f CC/method",
				integer(int(language.Loc.Sum)),
				language.MaintainabilityIndex.Avg,
				language.CyclomaticComplexityPerMethod.Avg))
		}
	})
}

func renderTestQuality(out *strings.Builder, combined analyzer.Aggregated) {
	quality := combined.TestQuality
	if quality == nil || quality.NbTestFiles == 0 {
		return
	}

	section(out, "Tests", func(body *strings.Builder) {
		row(body, "Classes covered by a test", fmt.Sprintf("%.1f%%  (%d / %d)",
			quality.TraceabilityPct, quality.NbTestedClasses, quality.NbProdClasses))
		row(body, "Isolation score", fmt.Sprintf("%.0f  (%s)", quality.GlobalIsolationScore, quality.IsolationLabel))
		row(body, "God tests", integer(len(quality.GodTests)))
		row(body, "Classes without any test", integer(len(quality.OrphanClasses)))
	})
}

// renderRisks lists the hotspots: the files combining a poor maintainability
// with a high change frequency, which is where refactoring pays off most.
func renderRisks(out *strings.Builder, files []*pb.File) {
	riskiest := make([]*pb.File, 0, len(files))
	for _, file := range files {
		if file == nil || file.IsTest || file.Stmts == nil || file.Stmts.Analyze == nil || file.Stmts.Analyze.Risk == nil {
			continue
		}
		if file.Stmts.Analyze.Risk.Score <= 0 {
			continue
		}
		riskiest = append(riskiest, file)
	}

	if len(riskiest) == 0 {
		return
	}

	sort.Slice(riskiest, func(i, j int) bool {
		return riskiest[i].Stmts.Analyze.Risk.Score > riskiest[j].Stmts.Analyze.Risk.Score
	})

	nbAtRisk := len(riskiest)
	if len(riskiest) > nbTopRisks {
		riskiest = riskiest[:nbTopRisks]
	}

	section(out, fmt.Sprintf("Hotspots (top %d of %d files at risk)", len(riskiest), nbAtRisk), func(body *strings.Builder) {
		for _, file := range riskiest {
			details := []string{}
			if index, ok := fileMaintainabilityIndex(file); ok {
				details = append(details, fmt.Sprintf("MI %.0f", index))
			}
			if file.Commits != nil {
				details = append(details, fmt.Sprintf("%d commits", len(file.Commits.Commits)))
			}

			line := fmt.Sprintf("  %.2f  %s", file.Stmts.Analyze.Risk.Score, filePath(file))
			if len(details) > 0 {
				line += "  (" + strings.Join(details, ", ") + ")"
			}
			fmt.Fprintln(body, line)
		}
	})
}

func renderLint(out *strings.Builder, projectAggregated analyzer.ProjectAggregated) {
	if projectAggregated.Evaluation == nil {
		return
	}

	section(out, "Lint", func(body *strings.Builder) {
		if projectAggregated.Evaluation.Succeeded {
			body.WriteString("  ✅ Requirements are met\n")
			return
		}
		body.WriteString("  ❌ Requirements are not met. Run: ast-metrics lint\n")
	})
}

// filePath prefers the path relative to the analyzed sources: an absolute path
// pushes the interesting part of the line out of the terminal.
func filePath(file *pb.File) string {
	if file.ShortPath != "" {
		return file.ShortPath
	}
	return file.Path
}

// fileMaintainabilityIndex returns the maintainability index of a file, taken
// from its first class for object-oriented code. It reports false when the file
// carries no index at all, so the caller can leave the detail out instead of
// printing a misleading zero.
func fileMaintainabilityIndex(file *pb.File) (float64, bool) {
	if file.Stmts != nil && file.Stmts.Analyze != nil && file.Stmts.Analyze.Maintainability != nil &&
		file.Stmts.Analyze.Maintainability.MaintainabilityIndex != nil {
		return *file.Stmts.Analyze.Maintainability.MaintainabilityIndex, true
	}

	for _, class := range engine.GetClassesInFile(file) {
		if class.Stmts == nil || class.Stmts.Analyze == nil || class.Stmts.Analyze.Maintainability == nil ||
			class.Stmts.Analyze.Maintainability.MaintainabilityIndex == nil {
			continue
		}
		return *class.Stmts.Analyze.Maintainability.MaintainabilityIndex, true
	}

	return 0, false
}

// countClassesToRefactor returns how many classes fall below the refactoring
// threshold, and how many classes carry a maintainability index at all. Test
// files are left out, like everywhere else in the aggregates: they are not the
// production code being measured.
func countClassesToRefactor(files []*pb.File) (int, int) {
	below, total := 0, 0
	for _, file := range files {
		if file == nil || file.IsTest {
			continue
		}
		for _, class := range engine.GetClassesInFile(file) {
			if class.Stmts == nil || class.Stmts.Analyze == nil || class.Stmts.Analyze.Maintainability == nil ||
				class.Stmts.Analyze.Maintainability.MaintainabilityIndex == nil {
				continue
			}
			total++
			if *class.Stmts.Analyze.Maintainability.MaintainabilityIndex < maintainabilityRefactorThreshold {
				below++
			}
		}
	}
	return below, total
}

// maintainabilityLabel translates the index into the wording used across the
// documentation and the HTML report.
func maintainabilityLabel(index float64) string {
	switch {
	case index > 85:
		return "easy to maintain"
	case index > 65:
		return "moderate"
	default:
		return "hard to maintain"
	}
}

// section writes a titled block. The title only appears when the block has
// something to say: a project analyzed without git, without a dependency graph
// or in a language carrying no coupling gets a shorter summary rather than a
// list of empty headers.
func section(out *strings.Builder, title string, build func(body *strings.Builder)) {
	body := &strings.Builder{}
	build(body)
	if body.Len() == 0 {
		return
	}

	style := lipgloss.NewStyle().Foreground(lipgloss.Color("#00FFFF")).Bold(true)
	fmt.Fprintf(out, "\n%s\n", style.Render(title))
	out.WriteString(body.String())
}

// metric writes one aggregate, and writes nothing at all when the analysis
// never fed it: a counter of zero means no measurement was taken, and printing
// an average of zero would read as a result rather than as a gap. The maximum
// is only shown when the aggregation actually tracked one.
func metric(out *strings.Builder, label string, result analyzer.AggregateResult) {
	if result.Counter == 0 {
		return
	}

	if result.Max > result.Avg {
		row(out, label+" (avg / max)", decimal(result.Avg)+" / "+decimal(result.Max))
		return
	}

	row(out, label+" (avg)", decimal(result.Avg))
}

// total writes the sum of an aggregate, and nothing when it was never fed.
func total(out *strings.Builder, label string, result analyzer.AggregateResult) {
	if result.Counter == 0 {
		return
	}

	row(out, label, integer(int(result.Sum)))
}

// row prints a label and its value. The label is padded so the values line up
// in a column, and the value is left as-is so it stays easy to grep.
func row(out *strings.Builder, label string, value string) {
	fmt.Fprintf(out, "  %-34s %s\n", label, value)
}

func decimal(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64)
}

func integer(value int) string {
	return strconv.Itoa(value)
}
