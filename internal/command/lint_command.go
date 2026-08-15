package command

import (
	"bufio"
	"fmt"
	"os"
	"sort"

	"github.com/charmbracelet/lipgloss"
	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	requirement "github.com/ast-metrics/ast-metrics/internal/analyzer/requirement"
	"github.com/ast-metrics/ast-metrics/internal/cli"
	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/report"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/pterm/pterm"
	"golang.org/x/term"
)

// LintCommand runs the analysis and prints only requirement violations (lint), grouped by file.
type LintCommand struct {
	Configuration *configuration.Configuration
	outWriter     *bufio.Writer
	runners       []engine.Engine
	verbose       bool
}

// lintTestHook is a test hook to force an error during LintCommand execution.
// It is set by unit tests when needed and should remain nil in production.
var lintTestHook func() error

func (c *LintCommand) SetVerbose(v bool) { c.verbose = v }

func NewLintCommand(configuration *configuration.Configuration, outWriter *bufio.Writer, runners []engine.Engine) *LintCommand {
	return &LintCommand{
		Configuration: configuration,
		outWriter:     outWriter,
		runners:       runners,
		verbose:       false,
	}
}

func (c *LintCommand) Execute() error {
	fmt.Print(cli.ScreenHeader("Lint"))
	fmt.Println()

	// test hook (used by unit tests to force a lint error)
	if lintTestHook != nil {
		if err := lintTestHook(); err != nil {
			return err
		}
	}

	// Only use spinners when connected to a real terminal to avoid
	// data races in pterm's SpinnerPrinter goroutine during tests.
	isTTY := term.IsTerminal(int(os.Stdout.Fd()))

	var spinner *pterm.SpinnerPrinter
	if isTTY {
		spinner, _ = cli.NewMoonSpinner("Analyzing source code...")
	}

	// Run engines to parse source files into in-memory ASTs
	var allParsed []*pb.File
	for _, runner := range c.runners {
		runner.SetConfiguration(c.Configuration)
		if !runner.IsRequired() {
			continue
		}
		if err := runner.Ensure(); err != nil {
			if spinner != nil {
				spinner.Stop()
			}
			return err
		}
		parsed := runner.DumpAST()
		allParsed = append(allParsed, parsed...)
		if err := runner.Finish(); err != nil {
			if spinner != nil {
				spinner.Stop()
			}
			return err
		}
	}

	if spinner != nil {
		spinner.Stop()
		spinner, _ = cli.NewMoonSpinner("Evaluating lint rules...")
	}

	// Global analysis on in-memory files
	allResults := analyzer.AnalyzeFiles(allParsed, nil)

	// Aggregate to get project-level metrics (TestQuality etc.)
	aggregator := analyzer.NewAggregator(allResults, nil)
	// One aggregate per analyzed source, so that a project holding its own
	// requirements is judged on its own metrics.
	aggregator.WithAnalyzedPaths(c.Configuration.SourcesToAnalyzePath)
	projectAggregated := aggregator.Aggregates()

	if spinner != nil {
		spinner.Stop()
	}

	// Evaluate requirements
	if c.Configuration.Requirements == nil {
		cli.PrintInfo("No requirements configured. Nothing to lint.")
		return nil
	}
	reqEval := requirement.NewScopedRequirementsEvaluator(c.Configuration)
	baselinePath := requirement.ResolveBaselinePath(c.Configuration.Requirements.Baseline)
	if baselinePath != "" {
		baseline, err := requirement.LoadBaseline(baselinePath)
		if err != nil {
			return err
		}
		reqEval.Baseline = baseline
	}
	projectCtx := buildProjectContext(projectAggregated)
	evaluation := reqEval.Evaluate(allResults, requirement.ProjectAggregated{
		ProjectCtx: projectCtx,
		ByScope:    buildProjectContexts(projectAggregated),
	})
	if evaluation.Baselined > 0 {
		cli.PrintInfo(fmt.Sprintf("%d violation(s) ignored via baseline (%s)", evaluation.Baselined, baselinePath))
	}

	// If SARIF path provided, write SARIF report from violations
	if c.Configuration.Reports.Sarif != "" {
		_, err := report.GenerateSarifFromOutcomes(c.Configuration.Reports.Sarif, evaluation.Errors, c.Configuration.Reports.SarifMaxLevel)
		if err != nil {
			return err
		}
		cli.PrintSuccess(fmt.Sprintf("SARIF report generated: %s", c.Configuration.Reports.Sarif))
	}

	// Build a map[filePath][]outcomes directly from structured results
	grouped := map[string][]requirement.RuleOutcome{}
	ungrouped := []requirement.RuleOutcome{}
	for _, out := range evaluation.Errors {
		if out.File == "" {
			ungrouped = append(ungrouped, out)
			continue
		}
		grouped[out.File] = append(grouped[out.File], out)
	}

	// When verbose, also prepare successes grouped by file
	groupedOK := map[string][]requirement.RuleOutcome{}
	if c.verbose {
		for _, ok := range evaluation.Successes {
			if ok.File == "" {
				continue
			}
			groupedOK[ok.File] = append(groupedOK[ok.File], ok)
		}
	}

	// Pretty print lint by file
	totalHigh := 0
	totalMedium := 0
	totalLow := 0
	total := 0
	files := make([]string, 0, len(grouped))
	for f := range grouped {
		files = append(files, f)
	}
	// If verbose, include files that only have successes
	if c.verbose {
		for f := range groupedOK {
			found := false
			for _, existing := range files {
				if existing == f {
					found = true
					break
				}
			}
			if !found {
				files = append(files, f)
			}
		}
	}
	sort.Strings(files)

	for _, f := range files {
		underline := lipgloss.NewStyle().Underline(true).Bold(true)
		fmt.Println(underline.Render("File: " + f))

		// successes first if verbose
		if c.verbose {
			oks := groupedOK[f]
			sort.Slice(oks, func(i, j int) bool { return oks[i].Message < oks[j].Message })
			for _, s := range oks {
				cli.PrintSuccess(f + " — " + stripPathPrefix(s.Message, f))
			}
		}
		// sort messages for deterministic output
		msgs := grouped[f]
		sort.Slice(msgs, func(i, j int) bool { return msgs[i].Message < msgs[j].Message })
		for _, m := range msgs {
			badge := ""
			switch m.Severity {
			case requirement.SeverityHigh:
				style := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true)
				badge = style.Render("HIGH    ")
				totalHigh++
			case requirement.SeverityMedium:
				style := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")).Bold(true)
				badge = style.Render("MEDIUM  ")
				totalMedium++
			case requirement.SeverityLow:
				style := lipgloss.NewStyle().Foreground(lipgloss.Color("#008000")).Bold(true)
				badge = style.Render("LOW     ")
				totalLow++
			}

			greyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

			ruleStyled := greyStyle.Render(" #" + m.Rule + "")
			content := "  • " + badge + stripPathPrefix(m.Message, f) + ruleStyled
			fmt.Println(content)

			total++
		}
		fmt.Println()
	}

	if len(ungrouped) > 0 {
		underline := lipgloss.NewStyle().Underline(true).Bold(true)
		fmt.Println(underline.Render("Project-level"))
		sort.Slice(ungrouped, func(i, j int) bool { return ungrouped[i].Message < ungrouped[j].Message })
		for _, m := range ungrouped {
			badge := ""
			switch m.Severity {
			case requirement.SeverityHigh:
				style := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF0000")).Bold(true)
				badge = style.Render("HIGH    ")
				totalHigh++
			case requirement.SeverityMedium:
				style := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")).Bold(true)
				badge = style.Render("MEDIUM  ")
				totalMedium++
			case requirement.SeverityLow:
				style := lipgloss.NewStyle().Foreground(lipgloss.Color("#008000")).Bold(true)
				badge = style.Render("LOW     ")
				totalLow++
			}

			greyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
			ruleStyled := greyStyle.Render(" #" + m.Rule)
			content := "  • " + badge + m.Message + ruleStyled
			fmt.Println(content)

			total++
		}
		fmt.Println()
	}

	// Summary and exit code
	if total == 0 {
		return nil
	}

	// return new Error
	return fmt.Errorf("%d lint issue(s) found (%d high, %d medium, %d low)", total, totalHigh, totalMedium, totalLow)
}

// extractPath tries to match a File.Path from analysis results inside the message string
func extractPath(msg string, files []*pb.File) string {
	for _, f := range files {
		if f.Path != "" && contains(msg, f.Path) {
			return f.Path
		}
	}
	return ""
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (s == sub || (len(s) > len(sub) && (indexOf(s, sub) >= 0)))
}

func indexOf(s, sub string) int {
	// simple implementation to avoid importing strings for now
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// stripPathPrefix removes the file path info from a message, preserving the rule description.
// It specifically strips patterns like " in file <path>".
func stripPathPrefix(msg, path string) string {
	// Try the common pattern first
	pattern := " in file " + path
	if i := indexOf(msg, pattern); i >= 0 {
		res := msg[:i] + msg[i+len(pattern):]
		return res
	}
	// Fallback: remove just the path, keeping surrounding text
	if i := indexOf(msg, path); i >= 0 {
		res := msg[:i] + msg[i+len(path):]
		return res
	}
	return msg
}
