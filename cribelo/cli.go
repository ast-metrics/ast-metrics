// Package cribelo is the command line of Cribelo, which tells which files of a
// change are worth a human reader. It is the one public entry point of the
// tool: github.com/cribelo/cribelo builds its binary on it, and
// cmd/cribelo here does the same for local development. The analysis itself
// stays in internal/cribelo.
package cribelo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"time"

	core "github.com/ast-metrics/ast-metrics/internal/cribelo"
	"github.com/mattn/go-isatty"
	"github.com/urfave/cli/v2"
)

// exitGate is returned when the quality gate fails; exitError for anything
// else that went wrong, so that a CI step can tell the two apart.
const (
	exitGate  = 1
	exitError = 2
)

var errGateFailed = errors.New("gate failed")

// Run executes the cribelo command line with the given arguments (the first
// one being the program name) and returns the exit code: 0 when everything
// went well, 1 when the quality gate failed, 2 on any other error.
//
// A version other than "" is the one printed by --version and in the plan
// footer; it lets a module built around this package report its own release.
func Run(args []string, version string) int {
	if version != "" {
		core.Version = version
	}
	app := &cli.App{
		Name:      "cribelo",
		Usage:     "the reading map of a change: what to read, in which order, and what the diff does not show",
		UsageText: "cribelo [options]            plan for the working tree against the base branch\n   cribelo skip|explain|describe|backtest|calibrate",
		Version:   core.Version,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "dir", Aliases: []string{"C"}, Value: ".", Usage: "repository or directory inside it"},
			&cli.StringFlag{Name: "base", Usage: "reference to compare against (default: the default branch)"},
			&cli.StringFlag{Name: "format", Value: "text", Usage: "text, markdown or json"},
			&cli.IntFlag{Name: "top", Value: 0, Usage: "read exactly this many files (default: every high file, at least 3)"},
			&cli.DurationFlag{Name: "budget", Usage: "reading time available, e.g. 10m; overrides --top"},
			&cli.StringFlag{Name: "fail-on", Usage: "fail when a file reaches this level: high, medium or never (default never)"},
			&cli.StringFlag{Name: "weights", Usage: "weights file (default .cribelo/weights.json)"},
			&cli.BoolFlag{Name: "no-clones", Usage: "skip duplicate detection"},
			&cli.StringFlag{Name: "coverage", Usage: "coverage report to read the untested changed lines from (lcov, Cobertura, Clover or Go profile; default: the usual places; none to skip)"},
			&cli.BoolFlag{Name: "no-color", Usage: "plain output"},
		},
		Before: func(c *cli.Context) error {
			_, noColor := os.LookupEnv("NO_COLOR")
			tty := isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
			core.SetColor(!c.Bool("no-color") && !noColor && tty)
			return nil
		},
		Action: func(c *cli.Context) error {
			plan, err := runPlan(c)
			if err != nil {
				return err
			}
			if err := printPlan(plan, c.String("format")); err != nil {
				return err
			}
			if plan.Gate == "failed" {
				return errGateFailed
			}
			return nil
		},
		Commands: []*cli.Command{
			{
				Name:  "skip",
				Usage: "list the skipped files, one line each, with the reason",
				Action: func(c *cli.Context) error {
					plan, err := runPlan(c)
					if err != nil {
						return err
					}
					fmt.Print(plan.SkipText())
					return nil
				},
			},
			{
				Name:  "describe",
				Usage: "write the structural summary of the change, in Markdown, for the pull request description",
				Action: func(c *cli.Context) error {
					plan, err := runPlan(c)
					if err != nil {
						return err
					}
					fmt.Print(plan.Describe())
					return nil
				},
			},
			{
				Name:      "explain",
				Usage:     "show every signal behind one file's position",
				ArgsUsage: "<file>",
				Action: func(c *cli.Context) error {
					if c.NArg() != 1 {
						return errors.New("usage: cribelo explain <file>")
					}
					plan, err := runPlan(c)
					if err != nil {
						return err
					}
					repo, err := core.OpenRepo(c.String("dir"))
					if err != nil {
						return err
					}
					out, err := plan.Explain(relToRoot(repo.Root, c.Args().First()))
					if err != nil {
						return err
					}
					fmt.Print(out)
					return nil
				},
			},
			{
				Name:  "backtest",
				Usage: "replay the main line and measure how well the ranking predicts later fixes",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "ref", Usage: "main line to replay (default: the default branch)"},
					&cli.IntFlag{Name: "max", Value: 300, Usage: "landings to replay"},
					&cli.IntFlag{Name: "top", Value: 3, Usage: "size of the reading list measured"},
					&cli.StringFlag{Name: "label", Value: "induced", Usage: "what counts as a file that needed a reader: induced (a later fix repaired lines this change wrote, SZZ) or fixed (a later fix touched the file)"},
					&cli.StringFlag{Name: "format", Value: "text", Usage: "text or json"},
				},
				Action: func(c *cli.Context) error {
					result, err := runBacktest(c)
					if err != nil {
						return err
					}
					if c.String("format") == "json" {
						out, err := result.JSON()
						if err != nil {
							return err
						}
						fmt.Print(out)
						return nil
					}
					fmt.Print(result.Text())
					return nil
				},
			},
			{
				Name:  "calibrate",
				Usage: "learn weights from this repository's history and store them in .cribelo/weights.json",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "ref", Usage: "main line to replay (default: the default branch)"},
					&cli.IntFlag{Name: "max", Value: 300, Usage: "landings to replay"},
					&cli.IntFlag{Name: "top", Value: 3, Usage: "size of the reading list optimized"},
					&cli.StringFlag{Name: "label", Value: "induced", Usage: "what counts as a file that needed a reader: induced (a later fix repaired lines this change wrote, SZZ) or fixed (a later fix touched the file)"},
					&cli.BoolFlag{Name: "dry-run", Usage: "show the outcome without writing the weights"},
				},
				Action: func(c *cli.Context) error {
					result, err := runBacktest(c)
					if err != nil {
						return err
					}
					cal, err := core.Calibrate(result, time.Now())
					if err != nil {
						return err
					}
					result.GenericHeldOut = &cal.BeforeTest
					result.GenericHeldOut = &cal.BeforeTest
					result.Calibrated = &cal.AfterTest
					fmt.Print(result.Text())
					fmt.Printf("\n%s.\n", cal.Explanation)
					if !cal.Improved {
						return nil
					}
					if c.Bool("dry-run") {
						fmt.Println("Dry run: weights not written.")
						return nil
					}
					repo, err := core.OpenRepo(c.String("dir"))
					if err != nil {
						return err
					}
					target := filepath.Join(repo.Root, filepath.FromSlash(core.WeightsFile))
					if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
						return err
					}
					if err := cal.Weights.Save(target); err != nil {
						return err
					}
					fmt.Printf("Weights written to %s (commit it, or cache it in CI).\n", core.WeightsFile)
					return nil
				},
			},
		},
	}

	// CRIBELO_CPUPROFILE=path writes a CPU profile of the run, for development.
	if path := os.Getenv("CRIBELO_CPUPROFILE"); path != "" {
		if f, err := os.Create(path); err == nil {
			if err := pprof.StartCPUProfile(f); err == nil {
				defer pprof.StopCPUProfile()
			}
		}
	}
	if err := app.Run(args); err != nil {
		if errors.Is(err, errGateFailed) {
			return exitGate
		}
		fmt.Fprintln(os.Stderr, "cribelo:", err)
		return exitError
	}
	return 0
}

func runPlan(c *cli.Context) (*core.Plan, error) {
	repo, err := core.OpenRepo(c.String("dir"))
	if err != nil {
		return nil, err
	}
	return core.Run(repo, core.Options{
		Base:        c.String("base"),
		Top:         c.Int("top"),
		Exact:       c.IsSet("top"),
		Budget:      c.Duration("budget"),
		FailOn:      c.String("fail-on"),
		WeightsPath: c.String("weights"),
		Clones:      !c.Bool("no-clones"),
		Coverage:    c.String("coverage"),
	})
}

func runBacktest(c *cli.Context) (*core.Backtest, error) {
	repo, err := core.OpenRepo(c.String("dir"))
	if err != nil {
		return nil, err
	}
	progress := func(done, total int) {
		if isatty.IsTerminal(os.Stderr.Fd()) {
			fmt.Fprintf(os.Stderr, "\rreplaying %d/%d", done, total)
			if done == total {
				fmt.Fprint(os.Stderr, "\r                    \r")
			}
		}
	}
	return core.RunBacktest(repo, core.BacktestOptions{
		Ref: c.String("ref"), Max: c.Int("max"), Top: c.Int("top"), Label: c.String("label"), Progress: progress,
	})
}

func printPlan(plan *core.Plan, format string) error {
	switch format {
	case "", "text":
		fmt.Print(plan.Text())
	case "markdown", "md":
		fmt.Print(plan.Markdown())
	case "json":
		out, err := plan.JSON()
		if err != nil {
			return err
		}
		fmt.Print(out)
	default:
		return fmt.Errorf("unknown format %q (text, markdown or json)", format)
	}
	return nil
}

// relToRoot turns a user-given path into the repository-relative form.
func relToRoot(root, p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return p
	}
	return filepath.ToSlash(rel)
}
