package cribelo

import (
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// The bars of the reading table have five cells. A value fills one cell, plus
// one for every step it passes, so that a glance compares files without
// reading the numbers.
var (
	complexitySteps = []int{5, 10, 20, 40}
	lineSteps       = []int{50, 150, 300, 600}
	afferentSteps   = []int{2, 5, 10, 25}
	commitSteps     = []int{2, 5, 10, 20}
)

const barCells = 5

func cells(value int, steps []int) int {
	if value <= 0 {
		return 0
	}
	n := 1
	for _, step := range steps {
		if value > step {
			n++
		}
	}
	return n
}

var (
	cellColors  = []lipgloss.Color{"2", "3", "208", "1", "1"}
	cellEmojis  = []string{"🟩", "🟨", "🟧", "🟥", "🟥"}
	styleGood   = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleBad    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleOrange = lipgloss.NewStyle().Foreground(lipgloss.Color("208"))
	styleNew    = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)
)

// cell is one table cell, rendered for a terminal and for markdown. Plain is
// the terminal text without escape codes, to measure its width.
type cell struct {
	text, plain, markdown string
}

func plainCell(s string) cell { return cell{s, s, s} }

// barCell draws a bar followed by its value. Filled cells take the color of
// the bar's level, empty ones are dimmed; without colors the two glyphs still
// tell them apart.
func barCell(value int, steps []int) cell {
	filled := cells(value, steps)
	text := styleDim.Render(strings.Repeat("▯", barCells))
	md := strings.Repeat("⬜", barCells)
	if filled > 0 {
		text = lipgloss.NewStyle().Foreground(cellColors[filled-1]).Render(strings.Repeat("▮", filled)) + styleDim.Render(strings.Repeat("▯", barCells-filled))
		md = strings.Repeat(cellEmojis[filled-1], filled) + strings.Repeat("⬜", barCells-filled)
	}
	number := ""
	if value > 0 {
		number = fmt.Sprintf(" %d", value)
	}
	return cell{text + number, strings.Repeat("▮", barCells) + number, md + number}
}

// column is one signal of the reading table. A column whose signal is zero for
// every file in the list is left out.
type column struct {
	header string
	cell   func(f *FileReport) cell
	shown  func(f *FileReport) bool
}

var columns = []column{
	{
		header: "complexity",
		cell:   func(f *FileReport) cell { return barCell(int(f.Values[FeatureMaxComplexity]), complexitySteps) },
	},
	{
		header: "lines",
		cell:   func(f *FileReport) cell { return barCell(f.Added+f.Deleted, lineSteps) },
	},
	{
		header: "used by",
		cell:   func(f *FileReport) cell { return barCell(int(f.Values[FeatureAfferent]), afferentSteps) },
		shown:  func(f *FileReport) bool { return f.Values[FeatureAfferent] > 0 },
	},
	{
		header: "commits 90d",
		cell: func(f *FileReport) cell {
			c := barCell(int(f.Values[FeatureChurn90d]), commitSteps)
			if fixes := int(f.Values[FeatureFixes90d]); fixes > 0 {
				word := "fix"
				if fixes > 1 {
					word = "fixes"
				}
				phrase := fmt.Sprintf(", %d %s", fixes, word)
				c.text += styleBad.Render(phrase)
				c.plain += phrase
				c.markdown += phrase
			}
			return c
		},
		shown: func(f *FileReport) bool { return f.Values[FeatureChurn90d] > 0 },
	},
	{
		header: "clone",
		cell: func(f *FileReport) cell {
			if f.Clone == nil {
				return plainCell("")
			}
			percent := int(f.Clone.Similarity*100 + 0.5)
			text := fmt.Sprintf("%d%%", percent)
			style := styleOrange
			if percent >= 90 {
				style = styleBad
			}
			names := strings.SplitN(f.Clone.Function, " ~ ", 2)
			md := fmt.Sprintf("%s of `%s:%s()`", text, path.Base(f.Clone.Path), shortName(names[len(names)-1]))
			return cell{style.Render(text), text, md}
		},
		shown: func(f *FileReport) bool { return f.Clone != nil },
	},
	{
		header: "tests",
		cell: func(f *FileReport) cell {
			if f.Values[FeatureUntested] > 0 {
				return cell{styleBad.Render("✗"), "✗", "❌"}
			}
			return cell{styleGood.Render("✓"), "✓", "✅"}
		},
	},
}

// readFiles are the files of the reading list, in order.
func (p *Plan) readFiles() []*FileReport {
	var out []*FileReport
	for i := range p.Files {
		if p.Files[i].Read {
			out = append(out, &p.Files[i])
		}
	}
	return out
}

// visibleColumns drops the columns that would be empty for every file.
func visibleColumns(files []*FileReport) []column {
	var out []column
	for _, c := range columns {
		if c.shown == nil {
			out = append(out, c)
			continue
		}
		for _, f := range files {
			if c.shown(f) {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

func isNew(f *FileReport) bool { return f.Status == "A" || f.Values[FeatureNewFile] > 0 }

// summary is the first sentence of a plan: how much there is to read.
func (p *Plan) summary() string {
	return fmt.Sprintf("Read %d of %d changed %s, about %s.", p.ReadCount, p.Changed, plural(p.Changed, "file"), duration(p.ReadMinutes))
}

// startSentence names the first file and why it comes first.
func (p *Plan) startSentence(quote string) string {
	for _, f := range p.Files {
		if f.Read {
			return fmt.Sprintf("Start with %s%s%s: %s.", quote, f.Path, quote, strings.Join(f.Reasons, ", "))
		}
	}
	return ""
}

func duration(minutes int) string {
	if minutes < 60 {
		return fmt.Sprintf("%d min", minutes)
	}
	if minutes%60 == 0 {
		return fmt.Sprintf("%d h", minutes/60)
	}
	return fmt.Sprintf("%d h %02d", minutes/60, minutes%60)
}

func width(s string) int { return utf8.RuneCountInString(s) }

func (p *Plan) textTable(b *strings.Builder) {
	files := p.readFiles()
	cols := visibleColumns(files)
	grid := make([][]cell, len(files))
	widths := make([]int, len(cols))
	for j, c := range cols {
		widths[j] = width(c.header)
	}
	pathWidth, anyNew := 0, false
	for i, f := range files {
		pathWidth = max(pathWidth, len(f.Path))
		anyNew = anyNew || isNew(f)
		for j, c := range cols {
			grid[i] = append(grid[i], c.cell(f))
			widths[j] = max(widths[j], width(grid[i][j].plain))
		}
	}
	indexWidth := len(fmt.Sprintf("%d", len(files)))
	tagWidth := 0
	if anyNew {
		tagWidth = len("  new")
	}

	header := strings.Repeat(" ", 2+indexWidth+2+pathWidth+tagWidth)
	for j, c := range cols {
		header += "  " + c.header + strings.Repeat(" ", widths[j]-width(c.header))
	}
	b.WriteString(styleDim.Render(strings.TrimRight(header, " ")) + "\n")

	for i, f := range files {
		tag := strings.Repeat(" ", tagWidth)
		if isNew(f) {
			tag = "  " + styleNew.Render("new")
		}
		line := fmt.Sprintf("  %s  %s%s%s", levelStyle(f.Level).Render(fmt.Sprintf("%*d", indexWidth, i+1)), stylePath.Render(f.Path), strings.Repeat(" ", pathWidth-len(f.Path)), tag)
		for j := range cols {
			c := grid[i][j]
			line += "  " + c.text
			if j < len(cols)-1 {
				line += strings.Repeat(" ", widths[j]-width(c.plain))
			}
		}
		b.WriteString(line + "\n")
	}
}

func (p *Plan) markdownTable(b *strings.Builder) {
	files := p.readFiles()
	cols := visibleColumns(files)
	b.WriteString("| | File |")
	for _, c := range cols {
		b.WriteString(" " + strings.ToUpper(c.header[:1]) + c.header[1:] + " |")
	}
	b.WriteString("\n|--:|:--|")
	for _, c := range cols {
		if c.header == "tests" {
			b.WriteString(":-:|")
		} else {
			b.WriteString(":--|")
		}
	}
	b.WriteString("\n")
	for i, f := range files {
		path := "`" + f.Path + "`"
		if isNew(f) {
			path += " 🆕"
		}
		fmt.Fprintf(b, "| %d | %s |", i+1, path)
		for _, c := range cols {
			b.WriteString(" " + c.cell(f).markdown + " |")
		}
		b.WriteString("\n")
	}
}
