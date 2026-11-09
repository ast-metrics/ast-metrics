package cribelo

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
)

// A plan shows the reading list in three blocks, each answering one
// question. Where do I read: the files in reading order, each with the lines
// to read closely. Why these files: one row per file, a bar per signal. How
// much is it: the line counts, per file and in total.

// The bars have five cells. A value fills one cell, plus one for every step
// it passes, so that a glance compares files without reading the numbers.
var (
	complexitySteps = []int{5, 10, 20, 40}
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

// Phrases are written with their identifiers and paths between backticks.
// Markdown keeps the marks, a terminal drops them.
func code(s string) string { return "`" + s + "`" }

func codeAll(words []string) []string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = code(w)
	}
	return out
}

func plain(marked string) string { return strings.ReplaceAll(marked, "`", "") }

// cell is one table cell, rendered for a terminal and for markdown. Plain is
// the terminal text without escape codes, to measure its width.
type cell struct {
	text, plain, markdown string
}

func plainCell(s string) cell { return cell{s, s, s} }

// A table that is too wide for a comment breaks its lines wherever it may,
// between two squares of a bar too. The word joiner forbids it there, and
// the no-break space keeps the value on the line of its bar.
const (
	wordJoiner   = "&#8288;"
	noBreakSpace = "&nbsp;"
)

// barCell draws a bar followed by its value. Filled cells take the color of
// the bar's level, empty ones are dimmed; without colors the two glyphs still
// tell them apart.
func barCell(value int, steps []int) cell {
	filled := cells(value, steps)
	text := styleDim.Render(strings.Repeat("▯", barCells))
	squares := make([]string, barCells)
	for i := range squares {
		squares[i] = "⬜"
		if i < filled {
			squares[i] = cellEmojis[filled-1]
		}
	}
	md := strings.Join(squares, wordJoiner)
	if filled > 0 {
		text = lipgloss.NewStyle().Foreground(cellColors[filled-1]).Render(strings.Repeat("▮", filled)) + styleDim.Render(strings.Repeat("▯", barCells-filled))
	}
	if value <= 0 {
		return cell{text, strings.Repeat("▮", barCells), md}
	}
	number := fmt.Sprintf("%d", value)
	return cell{text + " " + number, strings.Repeat("▮", barCells) + " " + number, md + noBreakSpace + number}
}

// column is one signal of the table that says why a file is in the list. A
// column whose signal is zero for every file is left out.
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
				c.markdown += fmt.Sprintf(", %d%s%s", fixes, noBreakSpace, word)
			}
			return c
		},
		shown: func(f *FileReport) bool { return f.Values[FeatureChurn90d] > 0 },
	},
	{
		// In Markdown the clone is told under the name of the file: the
		// function it resembles is too long for a column of a comment.
		header: columnClone,
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
			return cell{style.Render(text), text, ""}
		},
		shown: func(f *FileReport) bool { return f.Clone != nil },
	},
	{
		header: "test changed",
		cell: func(f *FileReport) cell {
			if f.Values[FeatureUntested] > 0 {
				return cell{styleBad.Render("✗"), "✗", "❌"}
			}
			return cell{styleGood.Render("✓"), "✓", "✅"}
		},
	},
}

const columnClone = "clone"

// cloneNote words the clone of a file: how close, and to which function. A
// clone in a file of the same name is told apart by its directory.
func cloneNote(f *FileReport) string {
	other := tail(f.Clone.Path, 1)
	if other == tail(f.Path, 1) {
		other = tail(f.Clone.Path, 3)
	}
	names := strings.SplitN(f.Clone.Function, " ~ ", 2)
	return fmt.Sprintf("%d%% similar to %s", int(f.Clone.Similarity*100+0.5), code(other+":"+shortName(names[len(names)-1])+"()"))
}

// visibleColumns drops the columns that would be empty for every file, and
// those named in without.
func visibleColumns(rows []readRow, without ...string) []column {
	var out []column
next:
	for _, c := range columns {
		for _, header := range without {
			if c.header == header {
				continue next
			}
		}
		if c.shown == nil {
			out = append(out, c)
			continue
		}
		for _, r := range rows {
			if c.shown(r.file) {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

// tail keeps the last n elements of a path.
func tail(p string, n int) string {
	parts := strings.Split(p, "/")
	if len(parts) > n {
		parts = parts[len(parts)-n:]
	}
	return strings.Join(parts, "/")
}

// shortNames gives every path the shortest tail that no other path of the
// list shares: the file name, with as many directories as it takes.
func shortNames(paths []string) map[string]string {
	out := map[string]string{}
	for depth := 1; len(out) < len(paths); depth++ {
		counts := map[string]int{}
		for _, p := range paths {
			counts[tail(p, depth)]++
		}
		for _, p := range paths {
			if _, done := out[p]; done {
				continue
			}
			if t := tail(p, depth); counts[t] == 1 || t == p {
				out[p] = t
			}
		}
	}
	return out
}

// Links gives the address of a changed file in the pull request, or of a
// span of its lines when start is positive.
type Links func(file string, start, end int) string

var (
	githubPull  = regexp.MustCompile(`^(https?://[^/]+/[^/]+/[^/]+)/pull/(\d+)`)
	gitlabMerge = regexp.MustCompile(`^(https?://.+)/-/merge_requests/\d+`)
)

// PullRequestLinks builds the links of a pull request from its address. On
// GitHub they open the diff of the pull request on the lines. On GitLab,
// whose diff anchors need the line numbers of both sides, they open the file
// at the head commit.
func PullRequestLinks(address, headSha string) (Links, error) {
	if m := githubPull.FindStringSubmatch(address); m != nil {
		files := m[1] + "/pull/" + m[2] + "/files"
		return func(file string, start, end int) string {
			anchor := fmt.Sprintf("%s#diff-%x", files, sha256.Sum256([]byte(file)))
			switch {
			case start <= 0:
				return anchor
			case end <= start:
				return fmt.Sprintf("%sR%d", anchor, start)
			}
			return fmt.Sprintf("%sR%d-R%d", anchor, start, end)
		}, nil
	}
	if m := gitlabMerge.FindStringSubmatch(address); m != nil {
		blob := m[1] + "/-/blob/" + headSha + "/"
		return func(file string, start, end int) string {
			segments := strings.Split(file, "/")
			for i, s := range segments {
				segments[i] = url.PathEscape(s)
			}
			address := blob + strings.Join(segments, "/")
			switch {
			case start <= 0:
				return address
			case end <= start:
				return fmt.Sprintf("%s#L%d", address, start)
			}
			return fmt.Sprintf("%s#L%d-%d", address, start, end)
		}, nil
	}
	return nil, fmt.Errorf("%s is not the address of a GitHub pull request or of a GitLab merge request", address)
}

// maxRanges bounds the spans listed for one file; the rest is counted.
const maxRanges = 4

// spans words line spans for a cell: "30-36, 66-77", each one a link when
// the plan knows the pull request. A lone line is called one, so that it is
// not taken for a count.
func (p *Plan) spans(f *FileReport, ranges []LineRange, markdown bool) string {
	if len(ranges) == 0 {
		return ""
	}
	// In a table cell a browser breaks a span at its hyphen: Markdown gets
	// the characters that forbid it.
	joiner, space := "", " "
	if markdown {
		joiner, space = wordJoiner, noBreakSpace
	}
	var out []string
	for i, r := range ranges {
		if i == maxRanges {
			break
		}
		span := fmt.Sprintf("%d", r.Start)
		if r.End > r.Start {
			span = fmt.Sprintf("%d-%s%d", r.Start, joiner, r.End)
		}
		if markdown && p.Links != nil {
			span = fmt.Sprintf("[%s](%s)", span, p.Links(f.Path, r.Start, r.End))
		}
		out = append(out, span)
	}
	text := strings.Join(out, ", ")
	if more := len(ranges) - maxRanges; more > 0 {
		text += fmt.Sprintf(", +%d%smore", more, space)
	}
	if len(ranges) == 1 && ranges[0].End <= ranges[0].Start {
		text = "line" + space + text
	}
	return text
}

// readLines are the lines of a file to read closely and to glance at. A file
// whose hunks were not classified is read whole.
func readLines(f *FileReport) (closely, glance int) {
	if f.Hunks == nil {
		return f.Added + f.Deleted, 0
	}
	return f.ToRead, f.ToGlance
}

// readRow is one file of the reading list.
type readRow struct {
	file *FileReport
	// index is the place of the file in the list, from 1.
	index int
	// closely and glance are line counts.
	closely, glance int
}

// rows are the files of the reading list, in order.
func (p *Plan) rows() []readRow {
	var out []readRow
	for i := range p.Files {
		f := &p.Files[i]
		if !f.Read {
			continue
		}
		row := readRow{file: f, index: len(out) + 1}
		row.closely, row.glance = readLines(f)
		out = append(out, row)
	}
	return out
}

// codeRows leaves the tests out: the signals are those of the code.
func codeRows(rows []readRow) []readRow {
	var out []readRow
	for _, r := range rows {
		if !r.file.Test {
			out = append(out, r)
		}
	}
	return out
}

func totals(rows []readRow) (closely, glance int) {
	for _, r := range rows {
		closely += r.closely
		glance += r.glance
	}
	return closely, glance
}

func isNew(f *FileReport) bool { return f.Status == "A" || f.Values[FeatureNewFile] > 0 }

// tag is the word after a path in the list: new, test, or nothing.
func tag(f *FileReport) string {
	switch {
	case f.Test:
		return "test"
	case isNew(f):
		return "new"
	}
	return ""
}

// summary is the first sentence of a plan: how much there is to read.
func (p *Plan) summary() string {
	return fmt.Sprintf("Read %d of %d changed %s, about %s.", p.ReadCount, p.Changed, plural(p.Changed, "file"), duration(p.ReadMinutes))
}

// The headers of the two columns of spans, and what the folded blocks say
// under their tables, in both formats.
const (
	headerClosely = "lines to read closely"
	headerGlance  = "lines to glance at"
)

var (
	legendClosely  = fmt.Sprintf("code lines in functions that gained a branch, or whose complexity is above %d", glanceComplexity)
	legendGlance   = "code lines in simpler functions, declarations, renames and moved blocks"
	legendRest     = "Blank lines, comments, imports and formatting are not counted"
	legendTime     = "Reading time: 20 lines a minute read closely, 60 glanced at, rounded up per file"
	legendComplex  = "highest cyclomatic complexity among the functions touched"
	legendUsedBy   = "files outside its directory that depend on this one"
	legendTestDone = "a test of the file is part of the change"
)

// mechanicalSentence says how many of the changed lines are to read closely,
// how many to glance at, and what was set aside: moves, renames, formatting,
// comments, imports. Empty when every line is to be read.
func (p *Plan) mechanicalSentence() string {
	if p.LinesChanged == 0 || (p.LinesToGlance == 0 && p.LinesToRead >= p.LinesChanged) {
		return ""
	}
	head := fmt.Sprintf("%d of %d changed lines to read", p.LinesToRead, p.LinesChanged)
	if p.LinesToGlance > 0 {
		head = fmt.Sprintf("%d of %d changed lines to read closely, %d to glance at", p.LinesToRead, p.LinesChanged, p.LinesToGlance)
	}
	parts := p.mechanicalParts()
	if len(parts) == 0 {
		return head + "."
	}
	return fmt.Sprintf("%s: %s.", head, strings.Join(parts, ", "))
}

// mechanicalParts names what was set aside: the mechanical natures with
// their hunk counts and the first renames.
func (p *Plan) mechanicalParts() []string {
	var parts []string
	for _, n := range []Nature{NatureMoved, NatureExtracted, NatureRename, NatureFormat, NatureComment, NatureImports} {
		if c, ok := p.Natures[n]; ok && c.Hunks > 0 {
			parts = append(parts, natureLabel(n, c.Hunks))
		}
	}
	for i, r := range p.Renames {
		if i == 2 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s ×%d", r.Pair, r.Hunks))
	}
	return parts
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

// textReading writes the three blocks for a terminal.
func (p *Plan) textReading(b *strings.Builder) {
	rows := p.rows()
	indexWidth := len(fmt.Sprintf("%d", len(rows)))
	indent := strings.Repeat(" ", 2+indexWidth+2)
	number := func(r readRow) string {
		text := fmt.Sprintf("%*d", indexWidth, r.index)
		if r.file.Test {
			return styleDim.Render(text)
		}
		return levelStyle(r.file.Level).Render(text)
	}

	// Where to read: one line per file, the spans to read closely, then
	// those to glance at.
	pathWidth, tagWidth, closelyWidth := 0, 0, len(headerClosely)
	closely, glance := make([]string, len(rows)), make([]string, len(rows))
	for i, r := range rows {
		pathWidth = max(pathWidth, len(r.file.Path))
		if t := tag(r.file); t != "" {
			tagWidth = max(tagWidth, 2+len(t))
		}
		closely[i] = p.spans(r.file, r.file.Ranges, false)
		glance[i] = p.spans(r.file, r.file.GlanceRanges, false)
		closelyWidth = max(closelyWidth, len(closely[i]))
	}
	header := fmt.Sprintf("%s%s  %-*s  %s", indent, strings.Repeat(" ", pathWidth+tagWidth), closelyWidth, headerClosely, headerGlance)
	b.WriteString(styleDim.Render(header) + "\n")
	for i, r := range rows {
		f := r.file
		word := strings.Repeat(" ", tagWidth)
		if t := tag(f); t != "" {
			style := styleDim
			if t == "new" {
				style = styleNew
			}
			word = "  " + style.Render(t) + strings.Repeat(" ", tagWidth-2-len(t))
		}
		line := fmt.Sprintf("  %s  %s%s%s", number(r), stylePath.Render(f.Path), strings.Repeat(" ", pathWidth-len(f.Path)), word)
		switch {
		case glance[i] != "":
			line += fmt.Sprintf("  %-*s  %s", closelyWidth, closely[i], styleDim.Render(glance[i]))
		case closely[i] != "":
			line += "  " + closely[i]
		}
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}

	// Why these files.
	if signals := codeRows(rows); len(signals) > 0 {
		cols := visibleColumns(signals)
		paths := make([]string, len(signals))
		for i, r := range signals {
			paths[i] = r.file.Path
		}
		names := shortNames(paths)
		nameWidth := 0
		grid := make([][]cell, len(signals))
		widths := make([]int, len(cols))
		for j, c := range cols {
			widths[j] = width(c.header)
		}
		for i, r := range signals {
			nameWidth = max(nameWidth, len(names[r.file.Path]))
			for j, c := range cols {
				grid[i] = append(grid[i], c.cell(r.file))
				widths[j] = max(widths[j], width(grid[i][j].plain))
			}
		}
		b.WriteString("\n" + styleTitle.Render("Why these files") + "\n")
		header := indent + strings.Repeat(" ", nameWidth)
		for j, c := range cols {
			header += "  " + c.header + strings.Repeat(" ", widths[j]-width(c.header))
		}
		b.WriteString(styleDim.Render(strings.TrimRight(header, " ")) + "\n")
		for i, r := range signals {
			name := names[r.file.Path]
			line := fmt.Sprintf("  %s  %s%s", number(r), stylePath.Render(name), strings.Repeat(" ", nameWidth-len(name)))
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

	// How much.
	total, totalGlance := totals(rows)
	fmt.Fprintf(b, "\n%s  %d %s to read closely, %d to glance at%s\n", styleTitle.Render("Totals"), total, plural(total, "line"), totalGlance, styleDim.Render(" (20 and 60 lines a minute)"))
	if parts := p.mechanicalParts(); len(parts) > 0 {
		fmt.Fprintf(b, "%s\n", styleDim.Render(fmt.Sprintf("Set aside in the whole change: %s.", strings.Join(parts, ", "))))
	}
}

// markdownReading writes the three blocks for a pull request comment: the
// table of where to read, and, folded, why these files and how much.
func (p *Plan) markdownReading(b *strings.Builder) {
	rows := p.rows()

	// Where to read.
	b.WriteString("| | File | Lines to read&nbsp;closely | Lines to glance&nbsp;at |\n|--:|:--|:--|:--|\n")
	for _, r := range rows {
		file := code(r.file.Path)
		if t := tag(r.file); t != "" {
			file += " *" + t + "*"
		}
		fmt.Fprintf(b, "| %d | %s | %s | %s |\n", r.index, file, p.spans(r.file, r.file.Ranges, true), p.spans(r.file, r.file.GlanceRanges, true))
	}

	// Why these files.
	if signals := codeRows(rows); len(signals) > 0 {
		cols := visibleColumns(signals, columnClone)
		paths := make([]string, len(signals))
		for i, r := range signals {
			paths[i] = r.file.Path
		}
		names := shortNames(paths)
		b.WriteString("\n<details><summary><b>Why these files</b></summary>\n\n| | File |")
		for _, c := range cols {
			header := strings.ToUpper(c.header[:1]) + c.header[1:]
			if c.header != "test changed" {
				header = strings.ReplaceAll(header, " ", noBreakSpace)
			}
			b.WriteString(" " + header + " |")
		}
		b.WriteString("\n|--:|:--|")
		for _, c := range cols {
			if c.header == "test changed" {
				b.WriteString(":-:|")
			} else {
				b.WriteString(":--|")
			}
		}
		b.WriteString("\n")
		for _, r := range signals {
			name := code(names[r.file.Path])
			if r.file.Clone != nil {
				name += "<br><sub>" + cloneNote(r.file) + "</sub>"
			}
			fmt.Fprintf(b, "| %d | %s |", r.index, name)
			for _, c := range cols {
				b.WriteString(" " + c.cell(r.file).markdown + " |")
			}
			b.WriteString("\n")
		}
		fmt.Fprintf(b, "\n<sub><b>Complexity</b>: %s. <b>Used by</b>: %s. <b>Test changed</b>: %s.</sub>\n\n</details>\n", legendComplex, legendUsedBy, legendTestDone)
	}

	// How much.
	closely, glance := totals(rows)
	fmt.Fprintf(b, "\n<details><summary><b>Totals</b>: %d %s to read closely, %d to glance at</summary>\n\n", closely, plural(closely, "line"), glance)
	b.WriteString("| | File | Lines to read closely | Lines to glance at |\n|--:|:--|--:|--:|\n")
	for _, r := range rows {
		fmt.Fprintf(b, "| %d | %s | %d | %d |\n", r.index, code(r.file.Path), r.closely, r.glance)
	}
	if len(rows) > 1 {
		fmt.Fprintf(b, "| | **Total** | **%d** | **%d** |\n", closely, glance)
	}
	fmt.Fprintf(b, "\n<sub><b>Read closely</b>: %s. <b>Glance at</b>: %s. %s. %s.</sub>\n\n</details>\n", legendClosely, legendGlance, legendRest, legendTime)
}
