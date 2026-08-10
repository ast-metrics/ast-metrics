package main

import (
	"bufio"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/cli"
	"github.com/ast-metrics/ast-metrics/internal/command"
	"github.com/ast-metrics/ast-metrics/internal/configuration"
	mcpserver "github.com/ast-metrics/ast-metrics/internal/mcp"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/csharp"
	"github.com/ast-metrics/ast-metrics/internal/engine/golang"
	"github.com/ast-metrics/ast-metrics/internal/engine/java"
	"github.com/ast-metrics/ast-metrics/internal/engine/php"
	"github.com/ast-metrics/ast-metrics/internal/engine/python"
	"github.com/ast-metrics/ast-metrics/internal/engine/rust"
	"github.com/ast-metrics/ast-metrics/internal/engine/typescript"
	"github.com/ast-metrics/ast-metrics/internal/watcher"
	"github.com/pterm/pterm"
	"github.com/sirupsen/logrus"
	cliV2 "github.com/urfave/cli/v2"
	osterm "golang.org/x/term"
)

var (
	// Current version. Managed by goreleaser during build
	// @see https://goreleaser.com/cookbooks/using-main.version/
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// isInteractiveSession reports whether the full-screen interface should be used.
//
// It requires a terminal on stdin: a CI job, a pipe, or a tool driving the CLI
// must never get a full-screen interface, whether or not it knows about the
// --non-interactive flag.
//
// The flag is looked up along the whole context lineage, because it is declared
// as a global option: "ast-metrics --non-interactive analyze ." has to be
// honoured just like "ast-metrics analyze --non-interactive .".
func isInteractiveSession(cCtx *cliV2.Context) bool {
	if !osterm.IsTerminal(int(os.Stdin.Fd())) {
		return false
	}

	for _, ctx := range cCtx.Lineage() {
		if ctx.Bool("non-interactive") || ctx.Bool("ci") {
			return false
		}
	}

	return true
}

// isTerminalOutput reports whether stdout is a terminal, which is the only thing
// that decides whether colour is meaningful. It is deliberately independent from
// the interactive interface: a non-interactive run in a terminal still deserves
// colours, and an interactive-looking run whose output is redirected does not.
func isTerminalOutput() bool {
	return osterm.IsTerminal(int(os.Stdout.Fd()))
}

func main() {

	logrus.SetLevel(logrus.TraceLevel)

	// Create a temporary directory
	build, err := os.MkdirTemp("", "ast-metrics")
	if err != nil {
		logrus.Error(err)
	}
	defer os.RemoveAll(build)

	// Prepare accepted languages
	runnerPhp := php.PhpRunner{}
	runnerGolang := golang.GolangRunner{}
	runnerPython := python.PythonRunner{}
	runnerRust := rust.RustRunner{}
	runnerTypeScript := typescript.TypeScriptRunner{}
	runnerJava := java.JavaRunner{}
	runnerCSharp := csharp.CSharpRunner{}
	runners := []engine.Engine{&runnerPhp, &runnerGolang, &runnerPython, &runnerRust, &runnerTypeScript, &runnerJava, &runnerCSharp}

	app := &cliV2.App{
		Name:  "ast-metrics",
		Usage: "Static code analysis tool",
		Flags: []cliV2.Flag{
			&cliV2.BoolFlag{
				Name:  "non-interactive",
				Usage: "Disable interactive mode",
			},
		},
		Before: func(cCtx *cliV2.Context) error {
			// Colour follows the output stream, not the interface: a redirected
			// or piped stream gets plain text, and a terminal keeps its colours
			// even when the interactive interface is disabled.
			if !isTerminalOutput() {
				pterm.DisableColor()
			}

			return nil
		},
		Action: func(cCtx *cliV2.Context) error {
			if !isInteractiveSession(cCtx) {
				// Non-interactive: print banner + help
				fmt.Println(cli.RenderBanner(version))
				return cliV2.ShowAppHelp(cCtx)
			}

			// Interactive: show welcome screen in a loop
			var lastError string
			for {
				result := cli.ShowWelcomeScreen(version, lastError)
				lastError = "" // reset after displaying

				if result.Command == "quit" {
					return nil
				}

				// Clear the main screen buffer so previous command output
				// doesn't accumulate (the welcome alt-screen restores the
				// main buffer on exit, which still holds old output).
				fmt.Print("\033[H\033[2J")

				if result.Command == "help" {
					_ = cliV2.ShowAppHelp(cCtx)
					fmt.Println()
					cli.PressAnyKeyToContinue()
					continue
				}

				// Build args: app-name + command + any user-provided args
				args := []string{cCtx.App.Name, result.Command}
				args = append(args, result.Args...)
				cmdErr := cCtx.App.Run(args)

				// Commands that have their own interactive TUI: no pause needed
				switch result.Command {
				case "analyze", "ci":
					if cmdErr != nil {
						lastError = cmdErr.Error()
					}
				default:
					// For commands that print output, show the error inline
					// and pause so the user can read before returning.
					if cmdErr != nil {
						cli.PrintError(cmdErr.Error())
					}
					fmt.Println()
					cli.PressAnyKeyToContinue()
				}
			}
		},
		Commands: []*cliV2.Command{
			{
				Name:    "analyze",
				Aliases: []string{"a"},
				Usage:   "Start analyzing the project",
				Flags: []cliV2.Flag{
					&cliV2.BoolFlag{
						Name:     "verbose",
						Aliases:  []string{"v"},
						Usage:    "Enable verbose mode",
						Category: "Global options",
					},
					&cliV2.StringSliceFlag{
						Name:     "exclude",
						Usage:    "Regular expression to exclude files from analysis",
						Category: "File selection",
					},
					&cliV2.BoolFlag{
						Name:     "non-interactive",
						Aliases:  []string{"i"},
						Usage:    "Disable interactive mode",
						Category: "Global options",
					},
					// HTML report
					&cliV2.StringFlag{
						Name:     "report-html",
						Usage:    "Generate an HTML report",
						Category: "Report",
					},
					&cliV2.BoolFlag{
						Name:     "open-html",
						Usage:    "Automatically open HTML report in browser",
						Category: "Report",
					},
					// Markdown report
					&cliV2.StringFlag{
						Name:     "report-markdown",
						Usage:    "Generate an Markdown report file",
						Category: "Report",
					},
					// JSON report
					&cliV2.StringFlag{
						Name:     "report-json",
						Usage:    "Generate a report in JSON format",
						Category: "Report",
					},
					// OpenMetrics report
					// https://github.com/prometheus/OpenMetrics/blob/main/specification/OpenMetrics.md
					&cliV2.StringFlag{
						Name:     "report-openmetrics",
						Usage:    "Generate a report in OpenMetrics format",
						Category: "Report",
					},
					// SARIF report
					&cliV2.StringFlag{
						Name:     "report-sarif",
						Usage:    "Generate a report in SARIF format (2.1.0)",
						Category: "Report",
					},
					&cliV2.StringFlag{
						Name:     "sarif-max-level",
						Usage:    "Cap the level of the SARIF results: error, warning or note. GitHub code scanning fails its own pull request check on new error level alerts, whatever the quality gate decided",
						Category: "Report",
					},
					// Watch mode
					&cliV2.BoolFlag{
						Name:     "watch",
						Usage:    "Re-run the analysis when files change",
						Category: "Global options",
					},
					// CI mode (alias of --non-interactive, --report-html and --report-markdown)
					&cliV2.BoolFlag{
						Name:     "ci",
						Usage:    "Enable CI mode",
						Category: "Global options",
					},
					// Configuration
					&cliV2.StringFlag{
						Name:     "config",
						Usage:    "Load configuration from file",
						Category: "Configuration",
					},
					// Diff mode (comparaison between current branch and another one or commit)
					&cliV2.StringFlag{
						Name:     "compare-with",
						Usage:    "Compare with another Git branch or commit",
						Category: "Global options",
					},
					// Profiling (with pprof)
					&cliV2.BoolFlag{
						Name:     "profile",
						Usage:    "Generate a profiling reports into files ast-metrics.cpu and ast-metrics.mem",
						Category: "Global options",
					},
					// Extra file extensions per language
					&cliV2.StringFlag{
						Name:     "php-extensions",
						Usage:    "Extra file extensions for PHP (comma-separated, e.g. .inc,.module)",
						Category: "File selection",
					},
					&cliV2.StringFlag{
						Name:     "go-extensions",
						Usage:    "Extra file extensions for Go (comma-separated)",
						Category: "File selection",
					},
					&cliV2.StringFlag{
						Name:     "python-extensions",
						Usage:    "Extra file extensions for Python (comma-separated)",
						Category: "File selection",
					},
					&cliV2.StringFlag{
						Name:     "rust-extensions",
						Usage:    "Extra file extensions for Rust (comma-separated)",
						Category: "File selection",
					},
					&cliV2.StringFlag{
						Name:     "typescript-extensions",
						Usage:    "Extra file extensions for TypeScript (comma-separated)",
						Category: "File selection",
					},
					&cliV2.StringFlag{
						Name:     "java-extensions",
						Usage:    "Extra file extensions for Java (comma-separated)",
						Category: "File selection",
					},
					&cliV2.StringFlag{
						Name:     "csharp-extensions",
						Usage:    "Extra file extensions for C# (comma-separated)",
						Category: "File selection",
					},
				},
				Action: func(cCtx *cliV2.Context) error {

					// get option --verbose
					if cCtx.Bool("verbose") {
						logrus.SetLevel(logrus.DebugLevel)
					}

					// get option --profile
					profile := cCtx.Bool("profile")
					if profile {
						cpufile := "ast-metrics.cpu"
						memfile := "ast-metrics.mem"
						f, err := os.Create(cpufile)
						if err != nil {
							logrus.Fatal("could not create CPU profile: ", err)
						}
						defer f.Close() // error handling omitted for example
						if err := pprof.StartCPUProfile(f); err != nil {
							logrus.Fatal("could not start CPU profile: ", err)
						}
						defer pprof.StopCPUProfile()

						f, err = os.Create(memfile)
						if err != nil {
							logrus.Fatal("could not create memory profile: ", err)
						}
						defer f.Close() // error handling omitted for example
						runtime.GC()    // get up-to-date statistics
						if err := pprof.WriteHeapProfile(f); err != nil {
							logrus.Fatal("could not write memory profile: ", err)
						}
					}

					// The interactive interface needs a terminal to drive it, so it
					// is not enough to look at --non-interactive: an analysis run
					// from a CI job or a script gets the plain output too.
					isInteractive := isInteractiveSession(cCtx)

					// Stdout
					outWriter := bufio.NewWriter(os.Stdout)

					// Prepare configuration object
					config := configuration.NewConfiguration()

					// Load configuration file
					loader := configuration.NewConfigurationLoader()
					if cCtx.String("config") != "" {
						loader.FilenameToChecks = []string{cCtx.String("config")}
					}

					config, err = loader.Loads(config)
					if err != nil {
						cli.PrintError("Cannot load configuration file: " + err.Error())
					}

					// If no configuration file is found, we ask the user to select a file or take it from arguments

					// If paths are provided in arguments, we use them
					paths := cCtx.Args()
					pathsSlice := make([]string, paths.Len())
					for i := 0; i < paths.Len(); i++ {
						pathsSlice[i] = paths.Get(i)
					}
					if cCtx.Args().Len() == 0 {
						if config.SourcesToAnalyzePath == nil || len(config.SourcesToAnalyzePath) == 0 {
							if isInteractive {
								// we try to ask the user to select a file
								pathsSlice = cli.AskUserToSelectFile()
							}
						} else {
							// Resolve config-sourced paths to absolute (verify existence, clean)
							err := config.SetSourcesToAnalyzePath(config.SourcesToAnalyzePath)
							if err != nil {
								cli.PrintError(err.Error())
								return err
							}
						}
					} else {
						if len(pathsSlice) == 0 && (config.SourcesToAnalyzePath == nil || len(config.SourcesToAnalyzePath) == 0) {
							cli.PrintError("Please provide a path to analyze")
							return nil
						}
						err := config.SetSourcesToAnalyzePath(pathsSlice)
						if err != nil {
							cli.PrintError(err.Error())
							return err
						}
					}

					// Exclude patterns
					if len(config.ExcludePatterns) == 0 {
						excludePatterns := cCtx.StringSlice("exclude")
						if len(excludePatterns) > 0 {
							config.SetExcludePatterns(excludePatterns)
						}
					}

					// Merge extra file extensions from CLI flags into config
					mergeExtensionFlags(cCtx, config)

					// Reports
					if cCtx.String("report-html") != "" {
						config.Reports.Html = cCtx.String("report-html")
					}
					if cCtx.String("report-markdown") != "" {
						config.Reports.Markdown = cCtx.String("report-markdown")
					}
					if cCtx.String("report-json") != "" {
						config.Reports.Json = cCtx.String("report-json")
					}
					if cCtx.String("report-openmetrics") != "" {
						config.Reports.OpenMetrics = cCtx.String("report-openmetrics")
					}
					if cCtx.String("report-sarif") != "" {
						config.Reports.Sarif = cCtx.String("report-sarif")
					}
					if cCtx.String("sarif-max-level") != "" {
						config.Reports.SarifMaxLevel = cCtx.String("sarif-max-level")
					}
					if cCtx.Bool("open-html") {
						config.Reports.OpenHtml = true
					}

					// CI mode
					if cCtx.Bool("ci") {
						cli.PrintWarning("[DEPRECATION] The --ci option for 'analyze' is deprecated. Please use the dedicated command: ast-metrics ci")
						if config.Reports.Html == "" {
							config.Reports.Html = "ast-metrics-html-report"
						}
						if config.Reports.Markdown == "" {
							config.Reports.Markdown = "ast-metrics-markdown-report.md"
						}
						if config.Reports.Json == "" {
							config.Reports.Json = "ast-metrics-report.json"
						}
						if config.Reports.OpenMetrics == "" {
							// we don't prefix the file with ast-metrics- because "metrics.txt" is a common filename for CI
							// @see https://docs.gitlab.com/ee/ci/testing/metrics_reports.html
							config.Reports.OpenMetrics = "metrics.txt"
						}
						if config.Reports.Sarif == "" {
							config.Reports.Sarif = "ast-metrics-report.sarif"
						}
						// isInteractive is already false here: isInteractiveSession
						// takes --ci into account.
					}

					// Compare with
					if cCtx.String("compare-with") != "" {
						config.CompareWith = cCtx.String("compare-with")
					}

					// Run command
					command := command.NewAnalyzeCommand(config, outWriter, runners, isInteractive)

					// Watch mode
					config.Watching = cCtx.Bool("watch")
					err = watcher.NewCommandWatcher(config).Start(command)
					if err != nil {
						cli.PrintError("Cannot watch files: " + err.Error())
					}

					// Execute command
					err = command.Execute()
					if err != nil {
						cli.PrintError(err.Error())
						return err
					}

					return nil
				},
			},
			{
				Name:    "clean",
				Aliases: []string{"c"},
				Usage:   "Clean workdir",
				Action: func(cCtx *cliV2.Context) error {
					// Run command
					config := configuration.NewConfiguration()
					command := command.NewCleanCommand(config.Storage)
					err := command.Execute()
					if err != nil {
						cli.PrintError(err.Error())
						return err
					}
					return nil
				},
			},
			{
				Name:    "self-update",
				Aliases: []string{"u"},
				Usage:   "Update current binary",
				Action: func(cCtx *cliV2.Context) error {
					// Run command
					command := command.NewSelfUpdateCommand(version)
					err := command.Execute()
					if err != nil {
						cli.PrintError(err.Error())
						return err
					}
					return nil
				},
			},
			{
				Name:  "ruleset",
				Usage: "Manage requirement rulesets",
				Subcommands: []*cliV2.Command{
					{
						Name:  "list",
						Usage: "List available rulesets",
						Action: func(cCtx *cliV2.Context) error {
							command := command.NewRulesetListCommand()
							return command.Execute()
						},
					},
					{
						Name:  "show",
						Usage: "Show rules inside a ruleset",
						Action: func(cCtx *cliV2.Context) error {
							if cCtx.Args().Len() == 0 {
								return fmt.Errorf("usage: ast-metrics ruleset show <name>")
							}
							name := cCtx.Args().First()
							command := command.NewRulesetShowCommand(name)
							return command.Execute()
						},
					},
					{
						Name:  "add",
						Usage: "Add all rules from a ruleset to the configuration file",
						Action: func(cCtx *cliV2.Context) error {
							if cCtx.Args().Len() == 0 {
								return fmt.Errorf("usage: ast-metrics ruleset add <name>")
							}
							name := cCtx.Args().First()
							command := command.NewRulesetAddCommand(name)
							return command.Execute()
						},
					},
				},
			},
			{
				Name:    "version",
				Aliases: []string{"v"},
				Usage:   "Print version information",
				Action: func(cCtx *cliV2.Context) error {
					// Run command
					command := command.NewVersionCommand(version)
					err := command.Execute()
					if err != nil {
						cli.PrintError(err.Error())
						return err
					}
					return nil
				},
			},
			{
				Name:    "lint",
				Aliases: []string{"l"},
				Usage:   "Run analysis and print lint (requirements) only",
				Flags: []cliV2.Flag{
					&cliV2.BoolFlag{Name: "verbose", Aliases: []string{"v"}, Usage: "Enable verbose mode", Category: "Global options"},
					&cliV2.StringSliceFlag{Name: "exclude", Usage: "Regular expression to exclude files from analysis", Category: "File selection"},
					&cliV2.StringFlag{Name: "config", Usage: "Load configuration from file", Category: "Configuration"},
					&cliV2.StringFlag{Name: "baseline", Usage: "Path to the baseline file used to ignore pre-existing violations (default: .ast-metrics-baseline.yaml if it exists)", Category: "Configuration"},
					&cliV2.StringFlag{Name: "report-sarif", Usage: "Write lint violations as SARIF 2.1.0 to the given file", Category: "Report"},
					&cliV2.StringFlag{Name: "sarif-max-level", Usage: "Cap the level of the SARIF results: error, warning or note", Category: "Report"},
					&cliV2.StringFlag{Name: "php-extensions", Usage: "Extra file extensions for PHP (comma-separated, e.g. .inc,.module)", Category: "File selection"},
					&cliV2.StringFlag{Name: "go-extensions", Usage: "Extra file extensions for Go (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "python-extensions", Usage: "Extra file extensions for Python (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "rust-extensions", Usage: "Extra file extensions for Rust (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "typescript-extensions", Usage: "Extra file extensions for TypeScript (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "java-extensions", Usage: "Extra file extensions for Java (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "csharp-extensions", Usage: "Extra file extensions for C# (comma-separated)", Category: "File selection"},
				},
				Action: func(cCtx *cliV2.Context) error {
					if cCtx.Bool("verbose") {
						logrus.SetLevel(logrus.DebugLevel)
					}
					outWriter := bufio.NewWriter(os.Stdout)
					config := configuration.NewConfiguration()
					loader := configuration.NewConfigurationLoader()
					if cCtx.String("config") != "" {
						loader.FilenameToChecks = []string{cCtx.String("config")}
					}
					cfg, err := loader.Loads(config)
					if err != nil {
						cli.PrintError("Cannot load configuration file: " + err.Error())
					}
					// report sarif flag
					if cCtx.String("report-sarif") != "" {
						cfg.Reports.Sarif = cCtx.String("report-sarif")
					}
					if cCtx.String("sarif-max-level") != "" {
						cfg.Reports.SarifMaxLevel = cCtx.String("sarif-max-level")
					}

					// Paths from args, then configuration file, then current directory.
					// Always resolved to absolute paths (like analyze/ci/review/baseline)
					// so that file paths reported by rules, and matched against the
					// baseline or exclude patterns, are consistent across commands.
					pathsSlice := []string{}
					for i := 0; i < cCtx.Args().Len(); i++ {
						pathsSlice = append(pathsSlice, cCtx.Args().Get(i))
					}
					if len(pathsSlice) == 0 {
						if len(cfg.SourcesToAnalyzePath) > 0 {
							pathsSlice = cfg.SourcesToAnalyzePath
						} else {
							pathsSlice = []string{"."}
						}
					}
					if err := cfg.SetSourcesToAnalyzePath(pathsSlice); err != nil {
						cli.PrintError(err.Error())
						return err
					}
					// exclude
					if len(cfg.ExcludePatterns) == 0 {
						ex := cCtx.StringSlice("exclude")
						if len(ex) > 0 {
							cfg.SetExcludePatterns(ex)
						}
					}
					// Merge extra file extensions from CLI flags into config
					mergeExtensionFlags(cCtx, cfg)
					// baseline override
					if cCtx.String("baseline") != "" && cfg.Requirements != nil {
						cfg.Requirements.Baseline = cCtx.String("baseline")
					}
					// No report generation here; just lint
					cmd := command.NewLintCommand(cfg, outWriter, runners)
					// pass verbose to command
					cmd.SetVerbose(cCtx.Bool("verbose"))
					command := cmd
					if err := command.Execute(); err != nil {
						return err
					}

					cli.PrintSuccess("No lint violations found.")

					return nil
				},
			},
			{
				Name:  "baseline",
				Usage: "Generate a baseline file to ignore existing lint violations, like PHPStan",
				Flags: []cliV2.Flag{
					&cliV2.BoolFlag{Name: "verbose", Aliases: []string{"v"}, Usage: "Enable verbose mode", Category: "Global options"},
					&cliV2.StringSliceFlag{Name: "exclude", Usage: "Regular expression to exclude files from analysis", Category: "File selection"},
					&cliV2.StringFlag{Name: "config", Usage: "Load configuration from file", Category: "Configuration"},
					&cliV2.StringFlag{Name: "baseline", Usage: "Path to the baseline file to write (default: .ast-metrics-baseline.yaml, or requirements.baseline from the configuration)", Category: "Configuration"},
					&cliV2.StringFlag{Name: "php-extensions", Usage: "Extra file extensions for PHP (comma-separated, e.g. .inc,.module)", Category: "File selection"},
					&cliV2.StringFlag{Name: "go-extensions", Usage: "Extra file extensions for Go (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "python-extensions", Usage: "Extra file extensions for Python (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "rust-extensions", Usage: "Extra file extensions for Rust (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "typescript-extensions", Usage: "Extra file extensions for TypeScript (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "java-extensions", Usage: "Extra file extensions for Java (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "csharp-extensions", Usage: "Extra file extensions for C# (comma-separated)", Category: "File selection"},
				},
				Action: func(cCtx *cliV2.Context) error {
					if cCtx.Bool("verbose") {
						logrus.SetLevel(logrus.DebugLevel)
					}
					outWriter := bufio.NewWriter(os.Stdout)
					config := configuration.NewConfiguration()
					loader := configuration.NewConfigurationLoader()
					if cCtx.String("config") != "" {
						loader.FilenameToChecks = []string{cCtx.String("config")}
					}
					cfg, err := loader.Loads(config)
					if err != nil {
						cli.PrintError("Cannot load configuration file: " + err.Error())
					}
					// Paths from args, then configuration file, then current directory
					pathsSlice := []string{}
					for i := 0; i < cCtx.Args().Len(); i++ {
						pathsSlice = append(pathsSlice, cCtx.Args().Get(i))
					}
					if len(pathsSlice) == 0 {
						if len(cfg.SourcesToAnalyzePath) > 0 {
							pathsSlice = cfg.SourcesToAnalyzePath
						} else {
							pathsSlice = []string{"."}
						}
					}
					if err := cfg.SetSourcesToAnalyzePath(pathsSlice); err != nil {
						cli.PrintError(err.Error())
						return err
					}
					// exclude
					if len(cfg.ExcludePatterns) == 0 {
						ex := cCtx.StringSlice("exclude")
						if len(ex) > 0 {
							cfg.SetExcludePatterns(ex)
						}
					}
					// Merge extra file extensions from CLI flags into config
					mergeExtensionFlags(cCtx, cfg)

					cmd := command.NewBaselineCommand(cfg, outWriter, runners)
					cmd.Path = cCtx.String("baseline")
					return cmd.Execute()
				},
			},
			{
				Name:  "ci",
				Usage: "Run lint then full analysis with reports (CI mode)",
				Flags: []cliV2.Flag{
					&cliV2.BoolFlag{Name: "verbose", Aliases: []string{"v"}, Usage: "Enable verbose mode", Category: "Global options"},
					&cliV2.StringSliceFlag{Name: "exclude", Usage: "Regular expression to exclude files from analysis", Category: "File selection"},
					&cliV2.StringFlag{Name: "report-html", Usage: "Generate an HTML report", Category: "Report"},
					&cliV2.BoolFlag{Name: "open-html", Usage: "Automatically open HTML report in browser", Category: "Report"},
					&cliV2.StringFlag{Name: "report-markdown", Usage: "Generate a Markdown report file", Category: "Report"},
					&cliV2.StringFlag{Name: "report-json", Usage: "Generate a report in JSON format", Category: "Report"},
					&cliV2.StringFlag{Name: "report-openmetrics", Usage: "Generate a report in OpenMetrics format", Category: "Report"},
					&cliV2.StringFlag{Name: "report-sarif", Usage: "Generate a report in SARIF format (2.1.0)", Category: "Report"},
					&cliV2.StringFlag{Name: "config", Usage: "Load configuration from file", Category: "Configuration"},
					&cliV2.StringFlag{Name: "compare-with", Usage: "Compare with another Git branch or commit", Category: "Global options"},
					&cliV2.StringFlag{Name: "php-extensions", Usage: "Extra file extensions for PHP (comma-separated, e.g. .inc,.module)", Category: "File selection"},
					&cliV2.StringFlag{Name: "go-extensions", Usage: "Extra file extensions for Go (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "python-extensions", Usage: "Extra file extensions for Python (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "rust-extensions", Usage: "Extra file extensions for Rust (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "typescript-extensions", Usage: "Extra file extensions for TypeScript (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "java-extensions", Usage: "Extra file extensions for Java (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "csharp-extensions", Usage: "Extra file extensions for C# (comma-separated)", Category: "File selection"},
				},
				Action: func(cCtx *cliV2.Context) error {
					if cCtx.Bool("verbose") {
						logrus.SetLevel(logrus.DebugLevel)
					}
					// Stdout
					outWriter := bufio.NewWriter(os.Stdout)
					// Prepare configuration object
					config := configuration.NewConfiguration()
					// Load configuration file
					loader := configuration.NewConfigurationLoader()
					if cCtx.String("config") != "" {
						loader.FilenameToChecks = []string{cCtx.String("config")}
					}
					cfg, err := loader.Loads(config)
					if err != nil {
						cli.PrintError("Cannot load configuration file: " + err.Error())
					}
					// Paths from args, then configuration file, then current directory.
					// Always resolved to absolute paths so file paths reported by rules
					// (and matched against a baseline or exclude patterns) are consistent
					// with the lint/baseline/review commands.
					pathsSlice := []string{}
					for i := 0; i < cCtx.Args().Len(); i++ {
						pathsSlice = append(pathsSlice, cCtx.Args().Get(i))
					}
					if len(pathsSlice) == 0 {
						if len(cfg.SourcesToAnalyzePath) > 0 {
							pathsSlice = cfg.SourcesToAnalyzePath
						} else {
							pathsSlice = []string{"."}
						}
					}
					if err := cfg.SetSourcesToAnalyzePath(pathsSlice); err != nil {
						cli.PrintError(err.Error())
						return err
					}
					// Exclude patterns
					if len(cfg.ExcludePatterns) == 0 {
						excludePatterns := cCtx.StringSlice("exclude")
						if len(excludePatterns) > 0 {
							cfg.SetExcludePatterns(excludePatterns)
						}
					}
					// Merge extra file extensions from CLI flags into config
					mergeExtensionFlags(cCtx, cfg)
					// Reports from flags
					if cCtx.String("report-html") != "" {
						cfg.Reports.Html = cCtx.String("report-html")
					}
					if cCtx.String("report-markdown") != "" {
						cfg.Reports.Markdown = cCtx.String("report-markdown")
					}
					if cCtx.String("report-json") != "" {
						cfg.Reports.Json = cCtx.String("report-json")
					}
					if cCtx.String("report-openmetrics") != "" {
						cfg.Reports.OpenMetrics = cCtx.String("report-openmetrics")
					}
					if cCtx.String("report-sarif") != "" {
						cfg.Reports.Sarif = cCtx.String("report-sarif")
					}
					if cCtx.Bool("open-html") {
						cfg.Reports.OpenHtml = true
					}
					// CI defaults for reports if not set
					if cfg.Reports.Html == "" {
						cfg.Reports.Html = "ast-metrics-html-report"
					}
					if cfg.Reports.Markdown == "" {
						cfg.Reports.Markdown = "ast-metrics-markdown-report.md"
					}
					if cfg.Reports.Json == "" {
						cfg.Reports.Json = "ast-metrics-report.json"
					}
					if cfg.Reports.OpenMetrics == "" {
						cfg.Reports.OpenMetrics = "metrics.txt"
					}
					if cfg.Reports.Sarif == "" {
						cfg.Reports.Sarif = "ast-metrics-report.sarif"
					}
					// Compare with
					if cCtx.String("compare-with") != "" {
						cfg.CompareWith = cCtx.String("compare-with")
					}
					// Run CI command
					cmd := command.NewCICommand(cfg, outWriter, runners)
					if err := cmd.Execute(); err != nil {
						return err
					}
					return nil
				},
			},
			{
				Name:  "review",
				Usage: "Compare the current code with a base branch and report only new or worsened findings (PR-oriented)",
				Flags: []cliV2.Flag{
					&cliV2.BoolFlag{Name: "verbose", Aliases: []string{"v"}, Usage: "Enable verbose mode", Category: "Global options"},
					&cliV2.StringFlag{Name: "base", Usage: "Git branch or commit to compare against (default: origin/main, origin/master, main or master)", Category: "Review"},
					&cliV2.StringFlag{Name: "format", Usage: "Output format: text, markdown or json", Value: "text", Category: "Review"},
					&cliV2.StringFlag{Name: "fail-on", Usage: "Fail when a regression of at least this severity exists: high, medium, any or never", Value: "never", Category: "Review"},
					&cliV2.IntFlag{Name: "max-findings", Usage: "Maximum number of regressions displayed in text and markdown outputs", Value: 5, Category: "Review"},
					&cliV2.StringFlag{Name: "report-markdown", Usage: "Write the Markdown report to the given file", Category: "Report"},
					&cliV2.StringFlag{Name: "report-json", Usage: "Write the full JSON report to the given file", Category: "Report"},
					&cliV2.StringFlag{Name: "report-sarif", Usage: "Write regressions as SARIF 2.1.0 to the given file", Category: "Report"},
					&cliV2.StringFlag{Name: "sarif-max-level", Usage: "Cap the level of the SARIF results: error, warning or note. GitHub code scanning fails its own pull request check on new error level alerts, whatever --fail-on decided", Category: "Report"},
					&cliV2.StringSliceFlag{Name: "exclude", Usage: "Regular expression to exclude files from analysis", Category: "File selection"},
					&cliV2.StringFlag{Name: "config", Usage: "Load configuration from file", Category: "Configuration"},
					&cliV2.StringFlag{Name: "php-extensions", Usage: "Extra file extensions for PHP (comma-separated, e.g. .inc,.module)", Category: "File selection"},
					&cliV2.StringFlag{Name: "go-extensions", Usage: "Extra file extensions for Go (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "python-extensions", Usage: "Extra file extensions for Python (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "rust-extensions", Usage: "Extra file extensions for Rust (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "typescript-extensions", Usage: "Extra file extensions for TypeScript (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "java-extensions", Usage: "Extra file extensions for Java (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "csharp-extensions", Usage: "Extra file extensions for C# (comma-separated)", Category: "File selection"},
				},
				Action: func(cCtx *cliV2.Context) error {
					if cCtx.Bool("verbose") {
						logrus.SetLevel(logrus.DebugLevel)
					}
					config := configuration.NewConfiguration()
					loader := configuration.NewConfigurationLoader()
					if cCtx.String("config") != "" {
						loader.FilenameToChecks = []string{cCtx.String("config")}
					}
					cfg, err := loader.Loads(config)
					if err != nil {
						cli.PrintError("Cannot load configuration file: " + err.Error())
					}
					// Paths from args, then configuration file, then current directory
					pathsSlice := []string{}
					for i := 0; i < cCtx.Args().Len(); i++ {
						pathsSlice = append(pathsSlice, cCtx.Args().Get(i))
					}
					if len(pathsSlice) == 0 {
						if len(cfg.SourcesToAnalyzePath) > 0 {
							pathsSlice = cfg.SourcesToAnalyzePath
						} else {
							pathsSlice = []string{"."}
						}
					}
					if err := cfg.SetSourcesToAnalyzePath(pathsSlice); err != nil {
						cli.PrintError(err.Error())
						return err
					}
					// Exclude patterns
					if len(cfg.ExcludePatterns) == 0 {
						if ex := cCtx.StringSlice("exclude"); len(ex) > 0 {
							cfg.SetExcludePatterns(ex)
						}
					}
					mergeExtensionFlags(cCtx, cfg)

					cmd := command.NewReviewCommand(cfg, os.Stdout, runners)
					cmd.BaseRef = cCtx.String("base")
					cmd.Format = cCtx.String("format")
					cmd.FailOn = cCtx.String("fail-on")
					cmd.MaxFindings = cCtx.Int("max-findings")
					cmd.ReportMarkdown = cCtx.String("report-markdown")
					cmd.ReportJson = cCtx.String("report-json")
					cmd.ReportSarif = cCtx.String("report-sarif")
					cmd.ReportSarifMaxLevel = cCtx.String("sarif-max-level")
					return cmd.Execute()
				},
			},
			{
				Name:  "deploy:github",
				Usage: "Deploy AST-Metrics workflow to all repositories in a GitHub organization. It open a PR for each repository.",
				Flags: []cliV2.Flag{
					&cliV2.StringFlag{
						Name:     "token",
						Usage:    "GitHub personal access token (required)",
						Required: true,
					},
					&cliV2.StringFlag{
						Name:  "branch",
						Usage: "Branch name to create (default: chore/ast-metrics-setup)",
						Value: "chore/ast-metrics-setup",
					},
					&cliV2.StringFlag{
						Name:  "workflow-path",
						Usage: "Path to the workflow file (default: .github/workflows/ast-metrics.yml)",
						Value: ".github/workflows/ast-metrics.yml",
					},
					&cliV2.BoolFlag{
						Name:  "include-forks",
						Usage: "Include forked repositories",
						Value: false,
					},
				},
				Action: func(cCtx *cliV2.Context) error {
					if cCtx.Args().Len() == 0 {
						return fmt.Errorf("usage: ast-metrics deploy:github --token <token> <org>")
					}
					org := cCtx.Args().First()
					token := cCtx.String("token")
					branch := cCtx.String("branch")
					workflowPath := cCtx.String("workflow-path")
					includeForks := cCtx.Bool("include-forks")

					cmd := command.NewDeployGithubOrganizationCommand(org, token, branch, workflowPath, includeForks)
					err := cmd.Execute()
					if err != nil {
						cli.PrintError(err.Error())
						return err
					}
					return nil
				},
			},
			{
				Name:    "init",
				Aliases: []string{"i"},
				Usage:   "Create a default configuration file",
				Action: func(cCtx *cliV2.Context) error {
					// Run command
					command := command.NewInitConfigurationCommand()
					err := command.Execute()
					if err != nil {
						cli.PrintError(err.Error())
						return err
					}
					return nil
				},
			},
			{
				Name:  "mcp",
				Usage: "Start MCP (Model Context Protocol) server for AI coding agents (stdio transport)",
				Flags: []cliV2.Flag{
					&cliV2.StringSliceFlag{
						Name:     "exclude",
						Usage:    "Regular expression to exclude files from analysis",
						Category: "File selection",
					},
					&cliV2.StringFlag{
						Name:     "config",
						Usage:    "Load configuration from file",
						Category: "Configuration",
					},
					&cliV2.StringFlag{Name: "php-extensions", Usage: "Extra file extensions for PHP (comma-separated, e.g. .inc,.module)", Category: "File selection"},
					&cliV2.StringFlag{Name: "go-extensions", Usage: "Extra file extensions for Go (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "python-extensions", Usage: "Extra file extensions for Python (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "rust-extensions", Usage: "Extra file extensions for Rust (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "java-extensions", Usage: "Extra file extensions for Java (comma-separated)", Category: "File selection"},
					&cliV2.StringFlag{Name: "csharp-extensions", Usage: "Extra file extensions for C# (comma-separated)", Category: "File selection"},
				},
				Action: func(cCtx *cliV2.Context) error {
					// Redirect all logging to stderr (stdout is reserved for JSON-RPC)
					logrus.SetOutput(os.Stderr)
					logrus.SetLevel(logrus.WarnLevel)

					// Disable pterm output (would corrupt JSON-RPC on stdout)
					pterm.DisableColor()
					pterm.DisableOutput()

					// Prepare configuration
					config := configuration.NewConfiguration()
					loader := configuration.NewConfigurationLoader()
					if cCtx.String("config") != "" {
						loader.FilenameToChecks = []string{cCtx.String("config")}
					}
					config, err = loader.Loads(config)
					if err != nil {
						logrus.Warn("Cannot load configuration file: " + err.Error())
					}

					// Paths from args
					paths := cCtx.Args()
					if paths.Len() > 0 {
						pathsSlice := make([]string, paths.Len())
						for i := 0; i < paths.Len(); i++ {
							pathsSlice[i] = paths.Get(i)
						}
						if err := config.SetSourcesToAnalyzePath(pathsSlice); err != nil {
							return err
						}
					}

					// Exclude patterns
					if len(config.ExcludePatterns) == 0 {
						excludePatterns := cCtx.StringSlice("exclude")
						if len(excludePatterns) > 0 {
							config.SetExcludePatterns(excludePatterns)
						}
					}

					// Merge extra file extensions from CLI flags into config
					mergeExtensionFlags(cCtx, config)

					// Create and start MCP server
					s := mcpserver.NewMCPServer(version, config, runners)
					return mcpserver.ServeStdio(s)
				},
			},
		},
	}
	app.Suggest = true

	if err := app.Run(os.Args); err != nil {
		logrus.Error(err)
		os.Exit(1)
	}
}

func mergeExtensionFlags(cCtx *cliV2.Context, config *configuration.Configuration) {
	for _, pair := range []struct{ flag, lang string }{
		{"php-extensions", "php"}, {"go-extensions", "go"},
		{"python-extensions", "python"}, {"rust-extensions", "rust"},
		{"typescript-extensions", "typescript"},
		{"java-extensions", "java"}, {"csharp-extensions", "csharp"},
	} {
		if v := cCtx.String(pair.flag); v != "" {
			if config.Extensions == nil {
				config.Extensions = map[string][]string{}
			}
			for _, ext := range strings.Split(v, ",") {
				ext = strings.TrimSpace(ext)
				if ext != "" {
					config.Extensions[pair.lang] = append(config.Extensions[pair.lang], ext)
				}
			}
		}
	}
}
