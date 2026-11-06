package cribelo

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// Version is set at build time; it is printed in every report footer.
var Version = "dev"

// SetColor disables colors when false (NO_COLOR, pipes, --no-color).
func SetColor(enabled bool) {
	if !enabled {
		lipgloss.SetColorProfile(termenv.Ascii)
	}
}

var (
	styleTitle  = lipgloss.NewStyle().Bold(true)
	styleDim    = lipgloss.NewStyle().Faint(true)
	styleHigh   = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	styleMedium = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleLow    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	stylePath   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
)

func levelStyle(level Level) lipgloss.Style {
	switch level {
	case LevelHigh:
		return styleHigh
	case LevelMedium:
		return styleMedium
	}
	return styleLow
}

// Text renders the plan for a terminal.
func (p *Plan) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", styleTitle.Render("cribelo"), styleDim.Render(fmt.Sprintf("· %d %s changed vs %s (merge-base %s)", p.Changed, plural(p.Changed, "file"), p.Base, shortSha(p.BaseSha))))
	if p.Changed == 0 {
		b.WriteString("\nNothing changed.\n")
		return b.String()
	}
	b.WriteString("\n")
	if p.ReadCount == 0 {
		b.WriteString(styleTitle.Render("Nothing to read") + styleDim.Render("  no code file changed") + "\n")
	} else {
		b.WriteString(styleTitle.Render(p.summary()) + "\n")
		b.WriteString(p.startSentence("") + "\n\n")
		p.textTable(&b)
	}

	if len(p.Skipped) > 0 {
		b.WriteString("\n")
		fmt.Fprintf(&b, "%s\n", styleTitle.Render(fmt.Sprintf("Skip (%d)", len(p.Skipped))))
		fmt.Fprintf(&b, "  %s%s\n", skipSummary(p.Skipped, " · "), styleDim.Render("    cribelo skip"))
	}

	b.WriteString("\n")
	gateStyle := styleLow
	if p.Gate == "failed" {
		gateStyle = styleHigh
	}
	fmt.Fprintf(&b, "Gate: %s %s\n", gateStyle.Render(p.Gate), styleDim.Render(fmt.Sprintf("(%d high, %d medium, fail on %s)", p.Count(LevelHigh), p.Count(LevelMedium), p.FailOn)))
	fmt.Fprintf(&b, "%s\n", styleDim.Render("Weights: "+p.weightsLine()))
	return b.String()
}

func (p *Plan) weightsLine() string {
	if p.Calibrated {
		return fmt.Sprintf("calibrated on this repository's last %d changes (%s)", p.TrainedOn, p.TrainedAt)
	}
	return "generic, not yet calibrated on this repository (run: cribelo calibrate)"
}

// skipSummary counts skipped files by reason, in a stable order.
func skipSummary(skipped []Skipped, sep string) string {
	counts := map[string]int{}
	for _, s := range skipped {
		counts[s.Reason]++
	}
	reasons := make([]string, 0, len(counts))
	for r := range counts {
		reasons = append(reasons, r)
	}
	sort.Slice(reasons, func(i, j int) bool {
		if counts[reasons[i]] != counts[reasons[j]] {
			return counts[reasons[i]] > counts[reasons[j]]
		}
		return reasons[i] < reasons[j]
	})
	parts := make([]string, 0, len(reasons))
	for _, r := range reasons {
		parts = append(parts, fmt.Sprintf("%s %d", r, counts[r]))
	}
	return strings.Join(parts, sep)
}

// SkipText lists every skipped file with its reason.
func (p *Plan) SkipText() string {
	if len(p.Skipped) == 0 {
		return "Nothing skipped.\n"
	}
	var b strings.Builder
	width := 0
	for _, s := range p.Skipped {
		if len(s.Path) > width {
			width = len(s.Path)
		}
	}
	for _, s := range p.Skipped {
		reason := s.Reason
		if s.Kind == "code" {
			reason = fmt.Sprintf("%s (score %.2f)", s.Reason, s.Score)
		}
		fmt.Fprintf(&b, "  %s%s  %s\n", stylePath.Render(s.Path), strings.Repeat(" ", width-len(s.Path)), styleDim.Render(reason))
	}
	return b.String()
}

// Explain details every signal behind one file's position.
func (p *Plan) Explain(path string) (string, error) {
	for _, f := range p.Files {
		if f.Path != path {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s  score %.2f (%s), ~%d min, %s\n\n", stylePath.Render(f.Path), f.Score, levelStyle(f.Level).Render(string(f.Level)), f.Minutes, map[bool]string{true: "in the reading list", false: "not in the reading list"}[f.Read])
		b.WriteString(styleTitle.Render("Signals") + styleDim.Render("  value → weight in the decision (positive pushes up)") + "\n")
		for _, c := range f.Contributions {
			value := fmt.Sprintf("%.0f", c.Value)
			if c.Feature == FeatureClone {
				value = fmt.Sprintf("%.2f", c.Value)
			}
			fmt.Fprintf(&b, "  %-56s %8s  %+.2f\n", c.Feature.Describe(), value, c.Logit)
		}
		if len(f.Functions) > 0 {
			b.WriteString("\n" + styleTitle.Render("Functions touched") + "\n")
			for _, fn := range f.Functions {
				state := fmt.Sprintf("complexity %d → %d", fn.Before, fn.After)
				if fn.New {
					state = fmt.Sprintf("new, complexity %d", fn.After)
				}
				fmt.Fprintf(&b, "  %-40s line %-5d %s\n", fn.Name, fn.Line, state)
			}
		}
		if f.Clone != nil {
			fmt.Fprintf(&b, "\n%s\n  %s is %d%% similar to %s:%d\n", styleTitle.Render("Possible duplicate"), f.Clone.Function, int(f.Clone.Similarity*100+0.5), f.Clone.Path, f.Clone.Line)
		}
		fmt.Fprintf(&b, "\n%s\n", styleDim.Render("Reading time assumes about twenty changed lines a minute. Weights: "+p.weightsLine()))
		return b.String(), nil
	}
	for _, s := range p.Skipped {
		if s.Path == path {
			return fmt.Sprintf("%s is skipped: %s\n", path, s.Reason), nil
		}
	}
	return "", fmt.Errorf("%s is not part of the change", path)
}

// Markdown renders the plan as a pull request comment.
func (p *Plan) Markdown() string {
	var b strings.Builder
	if p.Changed == 0 {
		b.WriteString("## Cribelo: nothing changed\n")
	} else if p.ReadCount == 0 {
		fmt.Fprintf(&b, "## Cribelo: nothing to read (%d %s changed, no code)\n\n", p.Changed, plural(p.Changed, "file"))
	} else {
		lead := p.summary()
		fmt.Fprintf(&b, "## Cribelo: %s\n\n", strings.TrimSuffix(strings.ToLower(lead[:1])+lead[1:], "."))
		fmt.Fprintf(&b, "%s\n\n", p.startSentence("`"))
		p.markdownTable(&b)
		b.WriteString("\n")
	}
	if len(p.Skipped) > 0 {
		fmt.Fprintf(&b, "<details><summary><b>Skip</b> (%d %s): %s</summary>\n\n", len(p.Skipped), plural(len(p.Skipped), "file"), skipSummary(p.Skipped, " · "))
		for _, s := range p.Skipped {
			fmt.Fprintf(&b, "- `%s`: %s\n", s.Path, s.Reason)
		}
		b.WriteString("\n</details>\n\n")
	}
	if p.Changed > 0 {
		fmt.Fprintf(&b, "**Gate:** %s · %d high, %d medium\n\n", p.Gate, p.Count(LevelHigh), p.Count(LevelMedium))
	}
	fmt.Fprintf(&b, "<sub>cribelo %s · methodology %s · %s · powered by AST Metrics</sub>\n", Version, p.Methodology, p.weightsLine())
	return b.String()
}

// JSON renders the whole plan.
func (p *Plan) JSON() (string, error) {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data) + "\n", nil
}
