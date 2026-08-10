package command

import (
	"bufio"
	"fmt"
	"os"

	"github.com/pterm/pterm"
	"golang.org/x/term"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	requirement "github.com/ast-metrics/ast-metrics/internal/analyzer/requirement"
	"github.com/ast-metrics/ast-metrics/internal/cli"
	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

// BaselineCommand analyzes the project and snapshots every current lint
// violation into a baseline file, the same way PHPStan's --generate-baseline
// does. Running lint/analyze/ci afterwards ignores those pre-existing
// violations, so a project can start enforcing requirements without having
// to fix everything at once.
type BaselineCommand struct {
	Configuration *configuration.Configuration
	outWriter     *bufio.Writer
	runners       []engine.Engine
	// Path overrides the baseline file to write. Defaults to
	// c.Configuration.Requirements.Baseline, then to
	// requirement.DefaultBaselineFilename.
	Path string
}

func NewBaselineCommand(configuration *configuration.Configuration, outWriter *bufio.Writer, runners []engine.Engine) *BaselineCommand {
	return &BaselineCommand{
		Configuration: configuration,
		outWriter:     outWriter,
		runners:       runners,
	}
}

func (c *BaselineCommand) Execute() error {
	fmt.Print(cli.ScreenHeader("Baseline"))
	fmt.Println()

	if c.Configuration.Requirements == nil {
		return fmt.Errorf("no requirements configured; add a requirements.rules section to your configuration file first (see `ast-metrics ruleset add`)")
	}

	path := c.Path
	if path == "" {
		path = c.Configuration.Requirements.Baseline
	}
	usingDefault := path == ""
	if usingDefault {
		path = requirement.DefaultBaselineFilename
	}

	isTTY := term.IsTerminal(int(os.Stdout.Fd()))
	var spinner *pterm.SpinnerPrinter
	if isTTY {
		spinner, _ = cli.NewMoonSpinner("Analyzing source code...")
	}

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
		allParsed = append(allParsed, runner.DumpAST()...)
		if err := runner.Finish(); err != nil {
			if spinner != nil {
				spinner.Stop()
			}
			return err
		}
	}

	if spinner != nil {
		spinner.UpdateText("Evaluating lint rules...")
	}

	allResults := analyzer.AnalyzeFiles(allParsed, nil)
	aggregator := analyzer.NewAggregator(allResults, nil)
	projectAggregated := aggregator.Aggregates()

	reqEval := requirement.NewRequirementsEvaluator(*c.Configuration.Requirements)
	projectCtx := buildProjectContext(projectAggregated)
	evaluation := reqEval.Evaluate(allResults, requirement.ProjectAggregated{ProjectCtx: projectCtx})

	if spinner != nil {
		spinner.Stop()
	}

	baseline := requirement.NewBaselineFromOutcomes(evaluation.Errors)
	if err := baseline.Save(path); err != nil {
		return fmt.Errorf("cannot write baseline file %q: %w", path, err)
	}

	cli.PrintSuccess(fmt.Sprintf("Baseline generated: %s (%d violation(s) ignored)", path, baseline.Len()))
	if usingDefault {
		cli.PrintInfo("It will be picked up automatically. Commit it, then run `ast-metrics lint` to see only new violations.")
	}
	return nil
}
