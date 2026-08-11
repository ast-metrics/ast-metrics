package cli

import (
	"fmt"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/ast-metrics/ast-metrics/internal/configuration"
	pb "github.com/ast-metrics/ast-metrics/pb"
	reportpkg "github.com/ast-metrics/ast-metrics/internal/report"
)

type ScreenEnd struct {
	isInteractive     bool
	files             []*pb.File
	projectAggregated analyzer.ProjectAggregated
	// program
	tea *tea.Program
	// reports
	Configuration configuration.Configuration
	reports       []reportpkg.GeneratedReport
}

func NewScreenEnd(
	isInteractive bool,
	files []*pb.File,
	projectAggregated analyzer.ProjectAggregated,
	configuration configuration.Configuration,
	reports []reportpkg.GeneratedReport,
) *ScreenEnd {
	return &ScreenEnd{
		isInteractive:     isInteractive,
		files:             files,
		projectAggregated: projectAggregated,
		Configuration:     configuration,
		reports:           reports,
	}
}

type modelEnd struct {
}

func (m modelEnd) Init() tea.Cmd {
	return nil
}

func (m *modelEnd) Reset(files []*pb.File, projectAggregated analyzer.ProjectAggregated) {
}

func (m modelEnd) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	return m, nil
}

func (m modelEnd) View() string {
	return ""
}

func (r *ScreenEnd) Render() {
	styleTitle := lipgloss.NewStyle().Foreground(lipgloss.Color("#00FFFF")).Bold(true)

	// List reports
	if r.Configuration.Reports.HasReports() {

		fmt.Println(styleTitle.Render("\nReports"))
		fmt.Println(styleTitle.Render("--------------------------------"))
		fmt.Println("\n📁 These reports have been generated:")

		for _, report := range r.reports {
			fmt.Println("\n  ✔ " + report.Path + " (" + report.Type + ")")
			fmt.Println("\n        " + report.Description)
			
			// Handle HTML report opening
			if report.Type == "html" && r.Configuration.Reports.OpenHtml {
				htmlPath := filepath.Join(report.Path, "index.html")
				if err := reportpkg.OpenHtmlReport(htmlPath); err != nil {
					fmt.Println("\n        " + reportpkg.GetOpenInstructions(htmlPath))
				}
			} else if report.Type == "html" {
				htmlPath := filepath.Join(report.Path, "index.html")
				fmt.Println("\n        " + reportpkg.GetOpenInstructions(htmlPath))
			}
		}

		fmt.Println("")
	} else {
		// The analysis produced a summary on stdout and nothing else. Say how to
		// get a durable report rather than writing a file nobody asked for.
		fmt.Println("\n💡 No report file was generated. Use --report-html=DIR, --report-json=FILE, or declare the reports you want in a configuration file.")
		fmt.Println("")
	}

	// Tips if configuration file does not exist
	if !r.Configuration.IsComingFromConfigFile {
		fmt.Println("\n💡 We noticed that you haven't yet created a configuration file. You can create a .ast-metrics.yaml configuration file by running: ast-metrics init")
		fmt.Println("")
	}

	// The lint verdict belongs to the summary report printed just above, so it
	// is deliberately not repeated here.

	fmt.Println("\n🌟 If you like AST Metrics, please consider starring the project on GitHub: https://github.com/ast-metrics/ast-metrics/. Thanks!")
	fmt.Println("")

}

func (r *ScreenEnd) Reset(files []*pb.File, projectAggregated analyzer.ProjectAggregated) {
}

func (r ScreenEnd) GetModel() tea.Model {
	return modelEnd{}
}

func (r ScreenEnd) GetScreenName() string {
	return "End"
}
