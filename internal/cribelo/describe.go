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
		if p.LinesToRead < p.LinesChanged {
			fmt.Fprintf(&b, ", %d to read once the mechanical hunks are set aside", p.LinesToRead)
		}
	}
	b.WriteString(".\n")
	if mechanical := p.mechanicalSentence(); mechanical != "" {
		fmt.Fprintf(&b, "%s\n", strings.TrimPrefix(mechanical, fmt.Sprintf("%d of %d changed lines to read: ", p.LinesToRead, p.LinesChanged)))
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
				if f := p.file(path); f != nil && f.Test {
					note = " (test)"
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

// file finds a report by path.
func (p *Plan) file(path string) *FileReport {
	for i := range p.Files {
		if p.Files[i].Path == path {
			return &p.Files[i]
		}
	}
	return nil
}
