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
	"github.com/charmbracelet/lipgloss"
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

// stepLabel says how far a level stands from the library, in words: a
// file of level d depends on a file of level d-1, and a file of level 0
// imports the library itself.
func stepLabel(depth int) string {
	switch depth {
	case 0:
		return "Import it directly"
	case 1:
		return "Depend on a file that imports it"
	case 2:
		return "Two imports away"
	case 3:
		return "Three imports away"
	default:
		return fmt.Sprintf("%d imports away", depth)
	}
}

func (c *WhoUsesCommand) printReach(reach analyzer.Reach) {
	w := c.out
	title := lipgloss.NewStyle().Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("#73F59F")).Bold(true)
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA500")).Bold(true)
	bad := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5555")).Bold(true)
	shareStyle := func(part, whole int) lipgloss.Style {
		switch {
		case whole > 0 && part*2 >= whole:
			return bad
		case whole > 0 && part*5 >= whole:
			return warn
		default:
			return accent
		}
	}

	fmt.Fprint(w, cli.ScreenHeader("Who uses "+reach.Query+"?"))
	fmt.Fprintln(w)
	if len(reach.Modules) == 0 {
		fmt.Fprintln(w, "  No imported module matches "+title.Render(reach.Query)+".")
		fmt.Fprintln(w, dim.Render("  Names are the ones the imports use: \"org.apache.logging.log4j\", \"github.com/sirupsen/logrus\", \"react\", \"Monolog\"."))
		return
	}

	fmt.Fprintln(w, title.Render(fmt.Sprintf("  %d %s matching", len(reach.Modules), plural(len(reach.Modules), "imported module"))))
	modules := reach.Modules
	if len(modules) > 12 {
		modules = modules[:12]
	}
	for _, module := range modules {
		fmt.Fprintln(w, dim.Render("    "+module))
	}
	if hidden := len(reach.Modules) - len(modules); hidden > 0 {
		fmt.Fprintln(w, dim.Render(fmt.Sprintf("    … and %d more (--format json lists them all)", hidden)))
	}
	fmt.Fprintln(w)

	files := reach.Files()
	fmt.Fprintf(w, "  %s of %d files depend on it (%s)\n", title.Render(fmt.Sprint(files)), reach.Scope, shareStyle(files, reach.Scope).Render(percent(files, reach.Scope)))
	fmt.Fprintln(w, dim.Render("  A file that imports it is one step away; a file depending on that file is two steps away, and so on."))
	fmt.Fprintln(w)

	relative := relativePathsFrom(c.Configuration.SourcesToAnalyzePath)
	for depth, level := range reach.Levels {
		fmt.Fprintf(w, "  %s %s\n", title.Render(stepLabel(depth)), dim.Render(fmt.Sprintf("· %d %s", len(level), plural(len(level), "file"))))
		shown := level
		if c.Limit > 0 && len(shown) > c.Limit {
			shown = shown[:c.Limit]
		}
		for _, file := range shown {
			fmt.Fprintf(w, "    %s\n", relative(file))
		}
		if hidden := len(level) - len(shown); hidden > 0 {
			fmt.Fprintln(w, dim.Render(fmt.Sprintf("    … and %d more (--limit 0 lists them all)", hidden)))
		}
		fmt.Fprintln(w)
	}

	if len(reach.Communities) > 0 {
		fmt.Fprintln(w, title.Render("  Communities reached"))
		fmt.Fprintln(w, dim.Render("  The parts of the code the dependents belong to, the most exposed first."))
		width := 0
		for _, community := range reach.Communities {
			if len(community.Name) > width {
				width = len(community.Name)
			}
		}
		for _, community := range reach.Communities {
			fmt.Fprintf(w, "    %-*s  %s  %s\n", width, community.Name, meter(community.Reached, community.Files, shareStyle(community.Reached, community.Files)), dim.Render(fmt.Sprintf("%d of %d files (%s)", community.Reached, community.Files, percent(community.Reached, community.Files))))
		}
		fmt.Fprintln(w)
	}
}

// meter draws a ten-cell bar of a share.
func meter(part, whole int, style lipgloss.Style) string {
	filled := 0
	if whole > 0 {
		filled = (part*10 + whole/2) / whole
	}
	if part > 0 && filled == 0 {
		filled = 1
	}
	return style.Render(strings.Repeat("█", filled)) + lipgloss.NewStyle().Foreground(lipgloss.Color("#333333")).Render(strings.Repeat("░", 10-filled))
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
