// Command cribelo tells which files of a change are worth a human reader.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"time"

	"github.com/ast-metrics/ast-metrics/internal/cribelo"
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

func main() {
	app := &cli.App{
		Name:      "cribelo",
		Usage:     "which files of this change are worth a human reader",
		UsageText: "cribelo [options]            plan for the working tree against the base branch\n   cribelo skip|explain|backtest|calibrate",
		Version:   cribelo.Version,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "dir", Aliases: []string{"C"}, Value: ".", Usage: "repository or directory inside it"},
			&cli.StringFlag{Name: "base", Usage: "reference to compare against (default: the default branch)"},
			&cli.StringFlag{Name: "format", Value: "text", Usage: "text, markdown or json"},
			&cli.IntFlag{Name: "top", Value: 0, Usage: "read exactly this many files (default: every high file, at least 3)"},
			&cli.DurationFlag{Name: "budget", Usage: "reading time available, e.g. 10m; overrides --top"},
			&cli.StringFlag{Name: "fail-on", Usage: "fail when a file reaches this level: high, medium or never (default never)"},
			&cli.StringFlag{Name: "weights", Usage: "weights file (default .cribelo/weights.json)"},
			&cli.BoolFlag{Name: "no-clones", Usage: "skip duplicate detection"},
			&cli.BoolFlag{Name: "no-color", Usage: "plain output"},
		},
		Before: func(c *cli.Context) error {
			_, noColor := os.LookupEnv("NO_COLOR")
			tty := isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())
			cribelo.SetColor(!c.Bool("no-color") && !noColor && tty)
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
					repo, err := cribelo.OpenRepo(c.String("dir"))
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
					&cli.BoolFlag{Name: "dry-run", Usage: "show the outcome without writing the weights"},
				},
				Action: func(c *cli.Context) error {
					result, err := runBacktest(c)
					if err != nil {
						return err
					}
					cal, err := cribelo.Calibrate(result, time.Now())
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
					repo, err := cribelo.OpenRepo(c.String("dir"))
					if err != nil {
						return err
					}
					target := filepath.Join(repo.Root, filepath.FromSlash(cribelo.WeightsFile))
					if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
						return err
					}
					if err := cal.Weights.Save(target); err != nil {
						return err
					}
					fmt.Printf("Weights written to %s (commit it, or cache it in CI).\n", cribelo.WeightsFile)
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
	if err := app.Run(os.Args); err != nil {
		if errors.Is(err, errGateFailed) {
			os.Exit(exitGate)
		}
		fmt.Fprintln(os.Stderr, "cribelo:", err)
		os.Exit(exitError)
	}
}

func runPlan(c *cli.Context) (*cribelo.Plan, error) {
	repo, err := cribelo.OpenRepo(c.String("dir"))
	if err != nil {
		return nil, err
	}
	return cribelo.Run(repo, cribelo.Options{
		Base:        c.String("base"),
		Top:         c.Int("top"),
		Exact:       c.IsSet("top"),
		Budget:      c.Duration("budget"),
		FailOn:      c.String("fail-on"),
		WeightsPath: c.String("weights"),
		Clones:      !c.Bool("no-clones"),
	})
}

func runBacktest(c *cli.Context) (*cribelo.Backtest, error) {
	repo, err := cribelo.OpenRepo(c.String("dir"))
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
	return cribelo.RunBacktest(repo, cribelo.BacktestOptions{
		Ref: c.String("ref"), Max: c.Int("max"), Top: c.Int("top"), Progress: progress,
	})
}

func printPlan(plan *cribelo.Plan, format string) error {
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
