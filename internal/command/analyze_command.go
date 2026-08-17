package command

import (
	"bufio"
	"errors"
	"fmt"
	"os"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	Activity "github.com/ast-metrics/ast-metrics/internal/analyzer/activity"
	"github.com/ast-metrics/ast-metrics/internal/analyzer/classifier"
	requirement "github.com/ast-metrics/ast-metrics/internal/analyzer/requirement"
	"github.com/ast-metrics/ast-metrics/internal/analyzer/ruleset"
	"github.com/ast-metrics/ast-metrics/internal/cli"
	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/report"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/fsnotify/fsnotify"
	"github.com/inancgumus/screen"
	"github.com/pterm/pterm"
	log "github.com/sirupsen/logrus"
)

type AnalyzeCommand struct {
	Configuration       *configuration.Configuration
	outWriter           *bufio.Writer
	runners             []engine.Engine
	predictArchitecture func(string, []*pb.File, string) ([]classifier.ClassPrediction, error)
	isInteractive       bool
	moonSpinner         *pterm.SpinnerPrinter
	alreadyExecuted     bool
	currentPage         *cli.ScreenHome
	FileWatcher         *fsnotify.Watcher
	gitSummaries        []analyzer.ResultOfGitAnalysis
}

func NewAnalyzeCommand(configuration *configuration.Configuration, outWriter *bufio.Writer, runners []engine.Engine, isInteractive bool) *AnalyzeCommand {
	return &AnalyzeCommand{
		Configuration: configuration,
		outWriter:     outWriter,
		runners:       runners,
		predictArchitecture: func(modelDirectory string, files []*pb.File, root string) ([]classifier.ClassPrediction, error) {
			return classifier.NewPredictor(modelDirectory).Predict(files, root)
		},
		isInteractive:   isInteractive,
		alreadyExecuted: false,
	}
}

func (v *AnalyzeCommand) runArchitectureClassification(allResults []*pb.File, projectAggregated *analyzer.ProjectAggregated) {
	if v.Configuration == nil || !v.Configuration.Architecture || len(v.Configuration.SourcesToAnalyzePath) == 0 {
		return
	}

	predictions, err := v.predictArchitecture(
		v.Configuration.ModelClassifierDirectory,
		allResults,
		v.Configuration.SourcesToAnalyzePath[0],
	)
	if err != nil {
		log.Debugf("Classification skipped: %v", err)
		return
	}

	projectAggregated.Predictions = predictions
}

func (v *AnalyzeCommand) Execute() error {

	if v.alreadyExecuted {
		v.moonSpinner = nil
		v.outWriter.Flush()
	}

	if v.isInteractive && !v.alreadyExecuted {
		fmt.Print(cli.ScreenHeader("Analyzing"))
		fmt.Println()
		v.moonSpinner, _ = cli.NewMoonSpinner("Preparing analysis...")
	}

	if v.alreadyExecuted {
		v.moonSpinner = nil
	}

	// Parse source code into in-memory ASTs
	parsedFiles, err := v.ExecuteRunnerAnalysis(v.Configuration)
	if err != nil {
		return err
	}

	if v.moonSpinner != nil {
		v.outWriter.Flush()
	}

	// Now we start the analysis of each parsed file
	if v.moonSpinner != nil {
		v.moonSpinner.UpdateText("Analyzing source code...")
	}

	// Run global analysis on in-memory files
	allResults := analyzer.AnalyzeFiles(parsedFiles, nil)

	// Git analysis
	if v.moonSpinner != nil {
		v.moonSpinner.UpdateText("Analyzing git history...")
	}
	if v.gitSummaries == nil {
		gitAnalyzer := analyzer.NewGitAnalyzer()
		v.gitSummaries = gitAnalyzer.Start(allResults)
	}

	// Now compare with another branch (if needed)
	allResultsCloned := []*pb.File{}

	if v.Configuration.CompareWith != "" {

		if v.moonSpinner != nil {
			v.moonSpinner.UpdateText("Comparing with " + v.Configuration.CompareWith + "...")
		}

		// switch branches
		for _, gitSummary := range v.gitSummaries {
			err = gitSummary.GitRepository.Checkout(v.Configuration.CompareWith)
			if err != nil {
				return errors.New(`Cannot compare code with branch or commit "` + v.Configuration.CompareWith +
					`" for repository ` + gitSummary.GitRepository.Path)
			}
		}

		// execute analysis on the other branch (reset file discovery cache)
		clonedConfig := *v.Configuration
		clonedConfig.FileDiscovery = nil
		parsedCloned, err := v.ExecuteRunnerAnalysis(&clonedConfig)
		if err != nil {
			return err
		}

		// Run global analysis on the other branch
		allResultsCloned = analyzer.AnalyzeFiles(parsedCloned, nil)

		// switch back to the original branch
		for _, gitSummary := range v.gitSummaries {
			err = gitSummary.GitRepository.RestoreFirstBranch()
			if err != nil {
				log.Error("Cannot checkout back to branch " + gitSummary.GitRepository.InitialBranch + " for " + gitSummary.GitRepository.Path)
			}
		}
	}

	// Start aggregating results
	aggregator := analyzer.NewAggregator(allResults, v.gitSummaries)
	aggregator.WithAggregateAnalyzer(Activity.NewBusFactor())
	// Per-directory views of the HTML report: one scope per analyzed path
	aggregator.WithAnalyzedPaths(v.Configuration.SourcesToAnalyzePath)
	if v.Configuration.CompareWith != "" {
		aggregator.WithComparaison(allResultsCloned, v.Configuration.CompareWith)
	}

	if v.moonSpinner != nil {
		v.moonSpinner.UpdateText("Aggregating results...")
	}
	projectAggregated := aggregator.Aggregates()

	// Evaluate requirements generating reports so templates can use results
	if v.Configuration.Requirements != nil {
		requirementsEvaluator := requirement.NewRequirementsEvaluator(*v.Configuration.Requirements)
		if baselinePath := requirement.ResolveBaselinePath(v.Configuration.Requirements.Baseline); baselinePath != "" {
			baseline, err := requirement.LoadBaseline(baselinePath)
			if err != nil {
				return err
			}
			requirementsEvaluator.Baseline = baseline
		}
		projectCtx := buildProjectContext(projectAggregated)
		evaluation := requirementsEvaluator.Evaluate(allResults, requirement.ProjectAggregated{ProjectCtx: projectCtx})
		projectAggregated.Evaluation = &evaluation
	}

	// AI-based architecture classification is opt-in because loading and running
	// the local models is significantly more expensive than the static analysis.
	v.runArchitectureClassification(allResults, &projectAggregated)

	// Generate reports
	if v.moonSpinner != nil {
		v.moonSpinner.UpdateText("Generating reports...")
	}

	// Factory reporters
	reportersFactory := report.ReportersFactory{
		Configuration: v.Configuration,
	}
	reporters := reportersFactory.Factory(v.Configuration)

	generatedReports := []report.GeneratedReport{}
	if v.Configuration.Reports.HasReports() {
		// Generate reports
		for _, reporter := range reporters {
			reports, err := reporter.Generate(allResults, projectAggregated)
			if err != nil {
				cli.PrintError("Cannot generate report: " + err.Error())
				return err
			}
			generatedReports = append(generatedReports, reports...)
		}
	}

	if v.moonSpinner != nil {
		v.moonSpinner.Stop()
	}

	// Details errors
	if len(projectAggregated.ErroredFiles) > 0 {
		cli.PrintWarning(fmt.Sprintf("%d files could not be analyzed. Use the --verbose option to get details", len(projectAggregated.ErroredFiles)))
		if log.GetLevel() == log.DebugLevel {
			for _, file := range projectAggregated.ErroredFiles {
				cli.PrintError("File " + file.Path)
				for _, err := range file.Errors {
					cli.PrintError("    " + err)
				}
			}
		}
	}

	// The summary is the report you get when you ask for no other one, so that
	// an analysis always produces something readable. The dashboard replaces it
	// when it is on.
	if !v.isInteractive && v.Configuration.Reports.HasSummary() {
		summary := report.NewSummaryReportGenerator(os.Stdout)
		if _, err := summary.Generate(allResults, projectAggregated); err != nil {
			cli.PrintError("Cannot print the summary: " + err.Error())
		}
	}

	// Ask the user what to do next. Only the full-screen session does: plain
	// output always gives the shell back instead of waiting on a keypress.
	if v.isInteractive && !v.alreadyExecuted {
		choice := cli.AskPostAnalysis(allResults, projectAggregated)
		switch choice {
		case cli.PostAnalysisOpenHTML:
			cli.GenerateAndOpenHTMLReport(allResults, projectAggregated)
			v.alreadyExecuted = true
			return nil
		case cli.PostAnalysisExplore:
			// Fall through to show ScreenHome
		case cli.PostAnalysisQuit:
			v.alreadyExecuted = true
			return nil
		}
	}

	// Display results
	if v.currentPage == nil {
		if v.isInteractive {
			screen.Clear()
			screen.MoveTopLeft()
		}
		v.currentPage = cli.NewScreenHome(v.isInteractive, allResults, projectAggregated)
		v.currentPage.Render()
	} else {
		screen.MoveTopLeft()
		v.currentPage.Reset(allResults, projectAggregated)
	}

	// Link to file watcher (in order to close it when app is closed)
	if v.FileWatcher != nil {
		v.currentPage.FileWatcher = v.FileWatcher
	}

	// Store state of the command
	v.alreadyExecuted = true

	// End screen (non-interactive reports summary)
	if !v.isInteractive {
		endScreen := cli.NewScreenEnd(v.isInteractive, allResults, projectAggregated, *v.Configuration, generatedReports)
		endScreen.Render()
	}

	return nil
}

func buildProjectContext(pa analyzer.ProjectAggregated) ruleset.ProjectContext {
	ctx := ruleset.ProjectContext{}
	if cm := pa.Combined.Community; cm != nil {
		info := &ruleset.CommunitiesInfo{Count: cm.CommunitiesCount, CrossSharePct: cm.CrossShare * 100}
		names := map[string]string{}
		for _, c := range cm.Communities {
			names[c.ID] = c.ShortName
		}
		for _, cycle := range cm.Cycles {
			members := make([]string, 0, len(cycle))
			for _, id := range cycle {
				members = append(members, names[id])
			}
			info.Cycles = append(info.Cycles, members)
		}
		for i := len(cm.Edges) - 1; i >= 0; i-- {
			// edges come heaviest first: the lightest cuts first
			if e := cm.Edges[i]; e.Back {
				info.BackEdges = append(info.BackEdges, fmt.Sprintf("%s → %s (%d)", names[e.From], names[e.To], e.Weight))
			}
		}
		ctx.Communities = info
	}
	tq := pa.Combined.TestQuality
	if tq == nil {
		return ctx
	}
	ctx.TraceabilityPct = tq.TraceabilityPct
	ctx.GlobalIsolationScore = tq.GlobalIsolationScore
	// Project rules name the offending file inside their message, and the
	// baseline recognizes an entry by that message. Paths are made relative
	// here, at the boundary, so a baseline survives moving between the host, a
	// container and CI.
	for _, gt := range tq.GodTests {
		ctx.GodTests = append(ctx.GodTests, ruleset.GodTestInfo{
			FilePath: requirement.RelativeToCwd(gt.FilePath),
			FanOut:   gt.SUTFanOut,
		})
	}
	for _, oc := range tq.OrphanClasses {
		ctx.OrphanClasses = append(ctx.OrphanClasses, ruleset.OrphanClassInfo{
			ClassName: oc.ClassName,
			FilePath:  requirement.RelativeToCwd(oc.FilePath),
			Weight:    oc.Weight,
		})
	}
	return ctx
}

func (v *AnalyzeCommand) ExecuteRunnerAnalysis(config *configuration.Configuration) ([]*pb.File, error) {
	if v.moonSpinner != nil {
		v.moonSpinner.UpdateText("Parsing source files...")
	}

	parsed, err := engine.ParseFiles(config, v.runners)
	if err != nil {
		cli.PrintError(err.Error())
		return nil, err
	}

	return parsed, nil
}
