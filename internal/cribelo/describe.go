package cribelo

import (
	"fmt"
	"strings"
)

// Describe writes the structural summary of a change, in Markdown, for the
// author to paste into the description of the pull request: what the change
// does to the shape of the code, what moved, what is mechanical, and in
// which order to read it. A description that says this much shortens the
// review; this one costs the author nothing and leaves the code where it is.
func (p *Plan) Describe() string {
	var b strings.Builder
	if p.Changed == 0 {
		return "Nothing changed.\n"
	}
	fmt.Fprintf(&b, "%d %s changed", p.Changed, plural(p.Changed, "file"))
	if p.LinesChanged > 0 {
		fmt.Fprintf(&b, ", %d lines", p.LinesChanged)
		if p.LinesToRead+p.LinesToGlance < p.LinesChanged || p.LinesToGlance > 0 {
			fmt.Fprintf(&b, ", %d to read closely", p.LinesToRead)
			if p.LinesToGlance > 0 {
				fmt.Fprintf(&b, " and %d to glance at", p.LinesToGlance)
			}
		}
	}
	b.WriteString(".\n")
	if parts := p.mechanicalParts(); len(parts) > 0 {
		fmt.Fprintf(&b, "Set aside: %s.\n", strings.Join(parts, ", "))
	}

	sections := []struct {
		title string
		kinds []string
	}{
		{"Signatures and API", []string{FactSignature, FactAPI}},
		{"Structure", []string{FactCycle, FactDependency, FactLibrary}},
		{"Worth a second look", []string{FactClone, FactHistory}},
	}
	for _, section := range sections {
		var lines []string
		for _, f := range p.Facts {
			for _, kind := range section.kinds {
				if f.Kind == kind {
					lines = append(lines, fmt.Sprintf("- `%s`: %s", f.Path, f.Text))
				}
			}
		}
		if len(lines) > 0 {
			fmt.Fprintf(&b, "\n**%s**\n\n%s\n", section.title, strings.Join(lines, "\n"))
		}
	}
	if len(p.Moves) > 0 || len(p.Renames) > 0 {
		b.WriteString("\n**Moves and renames**\n\n")
		for _, m := range p.Moves {
			fmt.Fprintf(&b, "- %s\n", moveLine(m, "`"))
		}
		for _, r := range p.Renames {
			fmt.Fprintf(&b, "- `%s` in %d %s\n", r.Pair, r.Hunks, plural(r.Hunks, "hunk"))
		}
	}
	if p.ReadCount > 0 {
		fmt.Fprintf(&b, "\n**Suggested reading order** (about %s)\n\n", duration(p.ReadMinutes))
		n := 0
		for _, g := range p.Groups {
			for i, path := range g.Files {
				n++
				note := ""
				if i == 0 && len(g.Files) > 1 && g.Linked {
					note = ": the others use it"
				}
				if f := p.file(path); f != nil {
					if f.Test {
						note = " (test)"
					}
					if ranges := readRanges(f); ranges != "" {
						note += ": " + ranges
					}
				}
				fmt.Fprintf(&b, "%d. `%s`%s\n", n, path, note)
			}
		}
	}
	if skipped := len(p.Skipped); skipped > 0 {
		fmt.Fprintf(&b, "\nLeft out: %s.\n", skipSummary(p.Skipped, ", "))
	}
	return b.String()
}

// readRanges words where the lines to read closely sit in a file: the
// functions that hold some, or the line spans of the hunks that do, four at
// most, and whether the rest is glanced at. Empty when the whole change is
// read.
func readRanges(f *FileReport) string {
	if f.ToGlance == 0 {
		return ""
	}
	var spans []string
	more := 0
	for _, fn := range f.Functions {
		if fn.Glance || fn.ToRead == 0 {
			continue
		}
		if len(spans) == 4 {
			more++
			continue
		}
		spans = append(spans, fmt.Sprintf("%s() l.%d", shortName(fn.Name), fn.Line))
	}
	if len(spans) > 0 {
		text := "read " + strings.Join(spans, ", ")
		if more > 0 {
			text += fmt.Sprintf(" and %d more %s", more, plural(more, "function"))
		}
		return text + ", glance at the rest"
	}
	for _, h := range f.Hunks {
		if h.ReadCost()-h.Glance <= 0 {
			continue
		}
		if len(spans) == 4 {
			more++
			continue
		}
		if h.Added == 0 {
			spans = append(spans, fmt.Sprintf("before line %d", h.Start))
		} else if h.Start == h.End {
			spans = append(spans, fmt.Sprintf("line %d", h.Start))
		} else {
			spans = append(spans, fmt.Sprintf("lines %d-%d", h.Start, h.End))
		}
	}
	if len(spans) == 0 {
		return "a glance is enough"
	}
	text := "read " + strings.Join(spans, ", ")
	if more > 0 {
		text += fmt.Sprintf(" and %d more %s", more, plural(more, "hunk"))
	}
	return text + ", glance at the rest"
}

// file finds a report by path.
func (p *Plan) file(path string) *FileReport {
	for i := range p.Files {
		if p.Files[i].Path == path {
			return &p.Files[i]
		}
	}
	return nil
}
