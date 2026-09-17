package command

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/ast-metrics/ast-metrics/internal/cli"
	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/pterm/pterm"
	"golang.org/x/term"
)

// WhoUsesCommand answers "who depends on this library?": the files importing
// it, then the files depending on those, level after level, and the
// communities they belong to. This is the question a vulnerability report
// raises, and a grep on the library's name answers only its first level.
type WhoUsesCommand struct {
	Configuration *configuration.Configuration
	runners       []engine.Engine
	out           io.Writer

	// Query is matched case-insensitively anywhere in an imported module.
	Query string
	// MaxDepth bounds the levels past the importers; zero lifts the bound.
	MaxDepth int
	// Limit caps the files listed per level in the text output; zero lists
	// them all.
	Limit int
	// Format of the output: text or json.
	Format string
}

func NewWhoUsesCommand(configuration *configuration.Configuration, out io.Writer, runners []engine.Engine, query string) *WhoUsesCommand {
	return &WhoUsesCommand{
		Configuration: configuration,
		runners:       runners,
		out:           out,
		Query:         query,
		Limit:         20,
		Format:        "text",
	}
}

func (c *WhoUsesCommand) Execute() error {
	if strings.TrimSpace(c.Query) == "" {
		return fmt.Errorf("please name the library to look for, e.g. ast-metrics who-uses log4j")
	}
	if c.Format != "text" && c.Format != "json" {
		return fmt.Errorf("unknown format %q: text or json", c.Format)
	}

	// Only use spinners when connected to a real terminal, and never over a
	// JSON output meant to be parsed.
	var spinner *pterm.SpinnerPrinter
	if c.Format == "text" && term.IsTerminal(int(os.Stdout.Fd())) {
		spinner, _ = cli.NewMoonSpinner("Analyzing source code...")
	}
	stop := func() {
		if spinner != nil {
			spinner.Stop()
			spinner = nil
		}
	}
	defer stop()

	files, err := engine.ParseFiles(c.Configuration, c.runners)
	if err != nil {
		return err
	}
	results := analyzer.AnalyzeFiles(files, nil)
	aggregator := analyzer.NewAggregator(results, nil)
	aggregator.WithAnalyzedPaths(c.Configuration.SourcesToAnalyzePath)
	combined := aggregator.Aggregates().Combined
	stop()

	reach := combined.WhoUses(c.Query, c.MaxDepth)
	if c.Format == "json" {
		encoder := json.NewEncoder(c.out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(reach)
	}
	c.printReach(reach)
	if len(reach.Modules) == 0 {
		return fmt.Errorf("no imported module matches %q", c.Query)
	}
	return nil
}

func (c *WhoUsesCommand) printReach(reach analyzer.Reach) {
	w := c.out
	fmt.Fprintf(w, "Who uses %q?\n\n", reach.Query)
	if len(reach.Modules) == 0 {
		fmt.Fprintf(w, "No imported module matches. The names are the ones the imports use, e.g. \"org.apache.logging.log4j\", \"github.com/sirupsen/logrus\" or \"react\".\n")
		return
	}

	fmt.Fprintf(w, "Imported modules matching (%d):\n", len(reach.Modules))
	for _, module := range reach.Modules {
		fmt.Fprintf(w, "  %s\n", module)
	}
	fmt.Fprintln(w)

	files := reach.Files()
	fmt.Fprintf(w, "Reach: %d of %d files (%s)", files, reach.Scope, percent(files, reach.Scope))
	if levels := len(reach.Levels) - 1; levels > 0 {
		fmt.Fprintf(w, ", up to %d %s away from the import", levels, plural(levels, "level"))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w)

	relative := relativePathsFrom(c.Configuration.SourcesToAnalyzePath)
	for depth, level := range reach.Levels {
		if depth == 0 {
			fmt.Fprintf(w, "Level 0, imports it (%d %s):\n", len(level), plural(len(level), "file"))
		} else {
			fmt.Fprintf(w, "Level %d (%d %s):\n", depth, len(level), plural(len(level), "file"))
		}
		shown := level
		if c.Limit > 0 && len(shown) > c.Limit {
			shown = shown[:c.Limit]
		}
		for _, file := range shown {
			fmt.Fprintf(w, "  %s\n", relative(file))
		}
		if hidden := len(level) - len(shown); hidden > 0 {
			fmt.Fprintf(w, "  ... and %d more (--limit 0 lists them all)\n", hidden)
		}
	}

	if len(reach.Communities) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Communities reached:")
		width := 0
		for _, community := range reach.Communities {
			if len(community.Name) > width {
				width = len(community.Name)
			}
		}
		for _, community := range reach.Communities {
			fmt.Fprintf(w, "  %-*s  %d of %d files (%s)\n", width, community.Name, community.Reached, community.Files, percent(community.Reached, community.Files))
		}
	}
}

// relativePathsFrom spells a path from the analyzed source it sits under,
// or from the working directory, the way a user would type it.
func relativePathsFrom(sources []string) func(string) string {
	roots := make([]string, 0, len(sources)+1)
	for _, source := range sources {
		if absolute, err := filepath.Abs(source); err == nil {
			roots = append(roots, absolute)
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		roots = append(roots, cwd)
	}
	// the deepest root first, so that a file under src/billing is spelled
	// from there rather than from the project
	sort.Slice(roots, func(i, j int) bool { return len(roots[i]) > len(roots[j]) })
	return func(path string) string {
		for _, root := range roots {
			if relative, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(relative, "..") {
				return relative
			}
		}
		return path
	}
}

func percent(part, whole int) string {
	if whole <= 0 {
		return "0%"
	}
	return fmt.Sprintf("%d%%", int(float64(part)*100/float64(whole)+0.5))
}

func plural(count int, noun string) string {
	if count == 1 {
		return noun
	}
	return noun + "s"
}
