package review

import (
	"encoding/json"
	"fmt"
	"strings"

	requirement "github.com/ast-metrics/ast-metrics/internal/analyzer/requirement"
)

// GateLabel returns "passed" or "failed" depending on the fail-on level.
// level is one of "high", "medium", "any" (alias of "low"), "never".
func (r *Result) EvaluateGate(level string) string {
	failed := false
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "high":
		failed = r.HasRegressionAtLeast(SeverityHigh)
	case "medium":
		failed = r.HasRegressionAtLeast(SeverityMedium)
	case "any", "low":
		failed = r.HasRegressionAtLeast(SeverityLow)
	case "", "never":
		failed = false
	}
	if failed {
		return "failed"
	}
	return "passed"
}

func (r *Result) headline() string {
	if r.Gate == "failed" {
		return "AST Metrics found blocking regressions"
	}
	if len(r.Regressions) > 0 {
		return "AST Metrics found regressions"
	}
	return "AST Metrics found no regression"
}

func (r *Result) statsLine() string {
	parts := []string{}
	parts = append(parts, fmt.Sprintf("%d new critical issue(s)", r.Summary.High))
	parts = append(parts, fmt.Sprintf("%d other regression(s)", r.Summary.Medium+r.Summary.Low))
	if r.Summary.Improvements > 0 {
		parts = append(parts, fmt.Sprintf("%d improvement(s)", r.Summary.Improvements))
	}
	return strings.Join(parts, ", ")
}

// metricDelta is the net change of a metric family across every finding,
// e.g. every complexity regression and improvement summed together.
type metricDelta struct {
	label          string
	delta          float64
	decimals       int
	higherIsBetter bool
}

// metricFamilies lists every checklist line, in display order: the internal
// key used to sum matching findings, the label shown to the reader, how many
// decimals its delta needs (Bugs is a small fractional estimate, everything
// else is effectively an integer count), and whether a rising value is good
// news (Maintainability) or bad news (everything else).
//
// The delta itself is always the natural After - Before change, so the
// number reads the same way an English sentence would ("Complexity: +7"
// means complexity went up by 7); the icon alone judges whether that
// direction is good or bad, so numbers never have to lie about their sign
// to fit a single "positive is always better" convention.
var metricFamilies = []struct {
	key            string
	label          string
	decimals       int
	higherIsBetter bool
}{
	{"Complexity", "Complexity", 0, false},
	{"Maintainability", "Ease of maintenance", 0, true},
	{"Coupling", "Outgoing dependencies", 0, false},
	{"Bugs", "Probability of bugs", 2, false},
}

// metricDeltas aggregates Before/After across all findings, grouped by
// metric family, in a fixed display order. Every family is always returned,
// even with a zero delta, so the report always shows the full checklist
// rather than only the metrics that happened to have a finding.
func (r *Result) metricDeltas() []metricDelta {
	sums := map[string]float64{}
	for _, family := range metricFamilies {
		sums[family.key] = 0
	}

	add := func(f Finding) {
		switch {
		case f.Rule == "new-complex-function" || strings.HasPrefix(f.Rule, "complexity-"):
			sums["Complexity"] += f.After - f.Before
		case strings.HasPrefix(f.Rule, "maintainability-"):
			sums["Maintainability"] += f.After - f.Before
		case f.Rule == "coupling-regression":
			sums["Coupling"] += f.After - f.Before
		case strings.HasPrefix(f.Rule, "bugs-"):
			sums["Bugs"] += f.After - f.Before
		}
	}
	for _, f := range r.Regressions {
		add(f)
	}
	for _, f := range r.Improvements {
		add(f)
	}

	deltas := make([]metricDelta, 0, len(metricFamilies))
	for _, family := range metricFamilies {
		deltas = append(deltas, metricDelta{
			label:          family.label,
			delta:          sums[family.key],
			decimals:       family.decimals,
			higherIsBetter: family.higherIsBetter,
		})
	}
	return deltas
}

// icon is a per-line status marker, so the good/bad signal sits next to each
// metric instead of being collapsed into a single icon on the headline. The
// trailing space is part of the icon itself: "⚠️" renders narrower than "✅"
// and "➖" in most fonts, so it needs an extra space to stay aligned with
// the others.
func (d metricDelta) icon() string {
	if d.delta == 0 {
		return "➖ "
	}
	improved := d.delta < 0
	if d.higherIsBetter {
		improved = d.delta > 0
	}
	if improved {
		return "✅ "
	}
	return "⚠️  "
}

// describe renders the signed, natural delta. The icon already carries the
// good/bad signal, so the number can stay compact and honest about what
// actually changed.
func (d metricDelta) describe() string {
	if d.delta == 0 {
		return fmt.Sprintf("%s: -", d.label)
	}
	return fmt.Sprintf("%s: %+.*f", d.label, d.decimals, d.delta)
}

func (f *Finding) title() string {
	if f.Subject != "" {
		return f.Subject
	}
	return f.File
}

func (f *Finding) location() string {
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d", f.File, f.Line)
	}
	return f.File
}

// Text renders a compact terminal report, capped to maxFindings regressions.
func (r *Result) Text(maxFindings int) string {
	var b strings.Builder
	b.WriteString(r.headline() + "\n\n")
	b.WriteString(r.statsLine() + "\n")

	b.WriteString("\nSummary:\n")
	for _, d := range r.metricDeltas() {
		b.WriteString("- " + d.icon() + d.describe() + "\n")
	}

	if len(r.Regressions) > 0 {
		b.WriteString("\nRegressions:\n")
		for i, f := range r.Regressions {
			if maxFindings > 0 && i >= maxFindings {
				b.WriteString(fmt.Sprintf("  ... and %d more (see JSON or SARIF report)\n", len(r.Regressions)-maxFindings))
				break
			}
			b.WriteString(fmt.Sprintf("- [%s] %s (%s)\n", strings.ToUpper(string(f.Severity)), f.title(), f.location()))
			b.WriteString("      " + f.Message + "\n")
			if f.Suggestion != "" {
				b.WriteString("      Suggested action: " + f.Suggestion + "\n")
			}
		}
	}

	if len(r.Improvements) > 0 {
		b.WriteString("\nImprovements:\n")
		for i, f := range r.Improvements {
			if i >= 3 {
				b.WriteString(fmt.Sprintf("  ... and %d more\n", len(r.Improvements)-3))
				break
			}
			b.WriteString(fmt.Sprintf("- %s (%s): %s\n", f.title(), f.location(), f.Message))
		}
	}

	return b.String()
}

// Markdown renders a report suitable for a PR comment or a job summary.
func (r *Result) Markdown(maxFindings int) string {
	var b strings.Builder

	b.WriteString("## " + r.headline() + "\n\n")
	b.WriteString(r.statsLine() + "\n")

	b.WriteString("\n### Summary\n\n")
	for _, d := range r.metricDeltas() {
		b.WriteString("- " + d.icon() + "**" + d.describe() + "**\n")
	}

	if len(r.Regressions) > 0 {
		b.WriteString("\n### Regressions\n\n")
		for i, f := range r.Regressions {
			if maxFindings > 0 && i >= maxFindings {
				b.WriteString(fmt.Sprintf("\n_... and %d more. Download the full report for details._\n", len(r.Regressions)-maxFindings))
				break
			}
			b.WriteString(fmt.Sprintf("- **%s** (`%s`, %s)\n", escapeMarkdown(f.title()), f.location(), f.Severity))
			b.WriteString("  " + escapeMarkdown(f.Message) + "\n")
			if f.Suggestion != "" {
				b.WriteString("  Suggested action: " + escapeMarkdown(f.Suggestion) + "\n")
			}
		}
	}

	if len(r.Improvements) > 0 {
		b.WriteString("\n### Improvements\n\n")
		for i, f := range r.Improvements {
			if i >= 3 {
				b.WriteString(fmt.Sprintf("\n_... and %d more._\n", len(r.Improvements)-3))
				break
			}
			b.WriteString(fmt.Sprintf("- **%s** (`%s`): %s\n", escapeMarkdown(f.title()), f.location(), escapeMarkdown(f.Message)))
		}
	}

	if len(r.Regressions) == 0 && len(r.Improvements) == 0 {
		b.WriteString("\nNo architectural change detected on modified code.\n")
	}

	return b.String()
}

// JSON renders the full, uncapped machine-readable report.
func (r *Result) JSON() (string, error) {
	out, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out) + "\n", nil
}

// ToRuleOutcomes converts regressions so they can feed the existing SARIF
// generator. Improvements are not exported to SARIF.
func (r *Result) ToRuleOutcomes() []requirement.RuleOutcome {
	outcomes := make([]requirement.RuleOutcome, 0, len(r.Regressions))
	for _, f := range r.Regressions {
		message := f.Message
		if f.Subject != "" {
			message = f.Subject + ": " + message
		}
		outcomes = append(outcomes, requirement.RuleOutcome{
			Severity: sarifSeverity(f.Severity),
			Rule:     f.Rule,
			Message:  message,
			File:     f.File,
			Line:     f.Line,
		})
	}
	return outcomes
}

func sarifSeverity(s Severity) requirement.Severity {
	switch s {
	case SeverityHigh:
		return requirement.SeverityHigh
	case SeverityMedium:
		return requirement.SeverityMedium
	default:
		return requirement.SeverityLow
	}
}

// escapeMarkdown neutralizes characters that could open an HTML tag or break
// a table cell. A lone ">" (e.g. in "8 -> 15") is harmless and kept as-is.
func escapeMarkdown(s string) string {
	replacer := strings.NewReplacer("<", "\\<", "|", "\\|")
	return replacer.Replace(s)
}
