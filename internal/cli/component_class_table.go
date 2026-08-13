package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ComponentTableClass struct {
	isInteractive   bool
	files           []*pb.File
	sortColumnIndex int
	table           table.Model
	search          ComponentSearch
}

// index of cols
var cols = map[string]int{
	"Name":            0,
	"Commits":         1,
	"Authors":         2,
	"Methods":         3,
	"LLoc":            4,
	"Cyclomatic":      5,
	"Halstead Length": 6,
	"Halstead Volume": 7,
	"Maintainability": 8,
}

func NewComponentTableClass(isInteractive bool, files []*pb.File) *ComponentTableClass {
	v := &ComponentTableClass{
		isInteractive:   isInteractive,
		files:           files,
		sortColumnIndex: 0,
	}

	v.Init()
	return v
}

func (v *ComponentTableClass) Render() string {

	var baseStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240"))

	text := StyleHelp(`
		Use arrows to navigate and esc to quit.
		Press following keys to sort by column:
		   (n) by name     (l) by LLOC		(c) by cyclomatic complexity
		   (m) by methods  (i) by maintainability index
		Press (ctrl+F) to search
	   `).Render() + "\n"

	// search box
	text += v.search.Render()

	// Results
	// The results
	if len(v.table.Rows()) == 0 {

		if v.search.HasSearch() {
			text += "No class found with the search: " + v.search.Expression + ".\n" + StyleHelp(`
				Press Ctrl+C or Esc to quit.
			`).Render()
		} else {
			text += "No class found.\n" + StyleHelp(`
				Press Ctrl+C or Esc to quit.
			`).Render()
		}
	} else {
		text = text + baseStyle.Render(v.table.View())
	}

	return text
}

func (v *ComponentTableClass) Init() {

	// search
	if &v.search == nil {
		v.search = ComponentSearch{Expression: ""}
	}

	columns := []table.Column{
		{Title: "Class", Width: 35},
		{Title: "Commits", Width: 6},
		{Title: "Authors", Width: 6},
		{Title: "Methods", Width: 9},
		{Title: "LLoc", Width: 6},
		{Title: "Cyclomatic", Width: 9},
		{Title: "H. Length", Width: 9},
		{Title: "H. Volume", Width: 9},
		{Title: "Maint.", Width: 9},
	}

	rows := []table.Row{}
	for _, file := range v.files {

		nbCommits := 0
		nbCommitters := 0
		if file.Commits != nil {
			nbCommits = int(file.Commits.Count)
			nbCommitters = int(file.Commits.CountCommiters)
		}

		classes := engine.GetClassesInFile(file)
		if classes == nil {
			continue
		}

		for _, class := range classes {

			if class == nil || class.Name == nil {
				continue
			}

			// Search
			if v.search.HasSearch() {
				// search by file name
				if !v.search.Match(class.Name.Qualified) {
					continue
				}
			}

			if class.Stmts == nil {
				continue
			}
			if class.Stmts.Analyze == nil {
				continue
			}

			nbFunctions := 0
			if class.Stmts.StmtFunction != nil {
				nbFunctions = len(class.Stmts.StmtFunction)
			}

			analyze := class.Stmts.Analyze
			volume := analyze.GetVolume()
			complexity := analyze.GetComplexity()
			maintainability := analyze.GetMaintainability()
			maintainabilityIndex := float64(0)
			if maintainability != nil && maintainability.MaintainabilityIndex != nil {
				maintainabilityIndex = maintainability.GetMaintainabilityIndex()
			}

			rows = append(rows, table.Row{
				class.Name.Qualified,
				strconv.Itoa(nbCommits),
				strconv.Itoa(nbCommitters),
				strconv.Itoa(nbFunctions),
				strconv.Itoa(int(volume.GetLoc())),
				strconv.Itoa(int(complexity.GetCyclomatic())),
				strconv.Itoa(int(volume.GetHalsteadLength())),
				fmt.Sprintf("%.2f", ToFixed(volume.GetHalsteadVolume(), 2)),
				DecorateMaintainabilityIndex(int(maintainabilityIndex), analyze),
			})
		}
	}

	// sort by name by default
	sort.Slice(rows, func(i, j int) bool {
		return rows[i][0] < rows[j][0]
	})

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(15),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	v.table = t
}

func (v *ComponentTableClass) Sort(sortColumnIndex int) {
	// Sort rows by second column (Loc)
	rows := v.table.Rows()
	sort.Slice(rows, func(i, j int) bool {

		if sortColumnIndex == 0 {
			return rows[i][sortColumnIndex] < rows[j][sortColumnIndex]
		}

		if sortColumnIndex == cols["Maintainability"] {
			// replace emoji
			a := strings.Replace(rows[i][sortColumnIndex], "🔴 ", "", 1)
			a = strings.Replace(a, "🟡 ", "", 1)
			a = strings.Replace(a, "🟢 ", "", 1)
			b := strings.Replace(rows[j][sortColumnIndex], "🔴 ", "", 1)
			b = strings.Replace(b, "🟡 ", "", 1)
			b = strings.Replace(b, "🟢 ", "", 1)

			aInt, _ := strconv.Atoi(a)
			bInt, _ := strconv.Atoi(b)
			return aInt < bInt
		}

		a, _ := strconv.Atoi(rows[i][sortColumnIndex])
		b, _ := strconv.Atoi(rows[j][sortColumnIndex])

		return a > b
	})

	v.sortColumnIndex = 0
	v.table.SetRows(rows)
}

func (v *ComponentTableClass) SortByName() {
	v.Sort(cols["Name"])
}

func (v *ComponentTableClass) SortByLloc() {
	v.Sort(cols["LLOC"])
}

func (v *ComponentTableClass) SortByNumberOfMethods() {
	v.Sort(cols["Methods"])
}

func (v *ComponentTableClass) SortByMaintainabilityIndex() {
	v.Sort(cols["Maintainability"])
}

func (v *ComponentTableClass) SortByCyclomaticComplexity() {
	v.Sort(cols["Cyclomatic"])
}

func (v *ComponentTableClass) Update(msg tea.Msg) {

	v.search.Update(msg)
	if v.search.IsEnabled() {
		// Stop here, and make search do its job
		// Apply search
		v.Init()
		v.table, _ = v.table.Update(msg)
		return
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "i":
			v.SortByMaintainabilityIndex()
		case "c":
			v.SortByCyclomaticComplexity()
		case "l":
			v.SortByLloc()
		case "m":
			v.SortByNumberOfMethods()
		case "n":
			v.SortByName()
		case "ctrl+f":
			v.search.Toggle("")
		}
	case DoRefreshModel:
		// refresh the model
		v.files = msg.files
		v.Init()
	}

	v.table, _ = v.table.Update(msg)
}
