package report

import (
	"embed"
	"fmt"
	"os"
	"sort"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/flosch/pongo2/v5"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

var (
	//go:embed templates/*
	mdContent embed.FS
)

// markdownFiltersOnce guards the registration of the pongo2 filters, which land
// in a process-global map.
var markdownFiltersOnce sync.Once

type MarkdownReportGenerator struct {
	// The path where the report will be generated
	ReportPath string
}

func NewMarkdownReportGenerator(reportPath string) Reporter {
	return &MarkdownReportGenerator{
		ReportPath: reportPath,
	}
}

func (v *MarkdownReportGenerator) Generate(files []*pb.File, projectAggregated analyzer.ProjectAggregated) ([]GeneratedReport, error) {

	// Ensure report is required
	if v.ReportPath == "" {
		return nil, nil
	}

	// Copy the templates from embed to a directory of this run's own, for the
	// reasons given in the HTML generator.
	templateDir, err := os.MkdirTemp("", "ast-metrics-templates-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(templateDir)
	for _, file := range []string{"index.md"} {
		// read the file
		mdContent, err := mdContent.ReadFile(fmt.Sprintf("templates/markdown/%s", file))
		if err != nil {
			return nil, err
		}

		// write the file to temporary folder (/tmp)
		err = os.WriteFile(fmt.Sprintf("%s/%s", templateDir, file), mdContent, 0644)
		if err != nil {
			return nil, err
		}
	}

	// Define loader in order to retrieve templates in the Report/Html/templates folder
	loader := pongo2.MustNewLocalFileSystemLoader(templateDir)
	templates := pongo2.NewSet(templateDir, loader)

	// Custom filters. The pongo2 filter map is process-global, so it is written
	// once however many reports are generated, concurrently or not.
	markdownFiltersOnce.Do(v.RegisterFilters)

	// Compile the index.md template
	tpl, err := templates.FromFile("index.md")
	if err != nil {
		log.Error(err)
		return nil, err
	}
	// Render it, passing projectAggregated and files as context
	out, err := tpl.Execute(pongo2.Context{"projectAggregated": projectAggregated, "files": files})
	if err != nil {
		log.Error(err)
		return nil, err
	}

	// Write the result to the file
	file, err := os.Create(v.ReportPath)
	if err != nil {
		log.Error(err)
		return nil, err
	}
	defer file.Close()
	file.WriteString(out)

	// cleanup temporary folder
	err = os.RemoveAll(templateDir)
	if err != nil {
		log.Error(err)
		return nil, err
	}

	reports := []GeneratedReport{
		{
			Path:        v.ReportPath,
			Type:        "file",
			Description: "The markdown report is useful for CI/CD pipelines, displaying the results in a human-readable format.",
			Icon:        "📄",
		},
	}
	return reports, nil

}

func (v *MarkdownReportGenerator) RegisterFilters() {

	pongo2.RegisterFilter("sortRisk", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
		// get the list to sort
		// create new empty list
		list := make([]*pb.File, 0)

		// append to the list when file contains at lease one class
		for _, file := range in.Interface().([]*pb.File) {
			if file.Stmts.StmtClass == nil {
				continue
			}

			list = append(list, file)
		}

		// sort the list
		sort.Slice(list, func(i, j int) bool {

			if list[i].Stmts.Analyze.Risk == nil {
				return false
			}

			if list[i].Stmts.StmtClass == nil {
				return true
			}

			if list[j].Stmts.StmtClass == nil {
				return true
			}

			class1 := list[i].Stmts.StmtClass[0]
			class2 := list[j].Stmts.StmtClass[0]

			if class1.Stmts.Analyze.Risk == nil {
				return false
			}
			if class2.Stmts.Analyze.Risk == nil {
				return true
			}

			return class1.Stmts.Analyze.Risk.Score > class2.Stmts.Analyze.Risk.Score
		})

		// keep only the first 10
		if len(list) > 10 {
			list = list[:10]
		}

		return pongo2.AsValue(list), nil
	})

	// filter to format number. Ex: 1234 -> 1 K
	if !pongo2.FilterExists("stringifyNumber") {
		pongo2.RegisterFilter("stringifyNumber", func(in *pongo2.Value, param *pongo2.Value) (out *pongo2.Value, err *pongo2.Error) {
			// get the number to format
			number := in.Integer()

			// format it
			if number > 1000000 {
				return pongo2.AsValue(fmt.Sprintf("%.1f M", float64(number)/1000000)), nil
			} else if number > 1000 {
				return pongo2.AsValue(fmt.Sprintf("%.1f K", float64(number)/1000)), nil
			}

			return pongo2.AsValue(number), nil
		})
	}
}
