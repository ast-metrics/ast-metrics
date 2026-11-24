package report

import (
	"github.com/ast-metrics/ast-metrics/internal/configuration"
)

type ReportersFactory struct {
	Configuration *configuration.Configuration
}

func (v *ReportersFactory) Factory(configuration *configuration.Configuration) []Reporter {
	reporters := []Reporter{}
	if v.Configuration.Reports.HasReports() {
		if v.Configuration.Reports.Html != "" {
			reporters = append(reporters, NewHtmlReportGenerator(v.Configuration.Reports.Html))
		}
		if v.Configuration.Reports.Markdown != "" {
			reporters = append(reporters, NewMarkdownReportGenerator(v.Configuration.Reports.Markdown))
		}
		if v.Configuration.Reports.Json != "" {
			reporters = append(reporters, NewJsonReportGenerator(v.Configuration.Reports.Json, v.Configuration.Reports.JsonFunctions))
		}
		if v.Configuration.Reports.OpenMetrics != "" {
			reporters = append(reporters, NewOpenMetricsReportGenerator(v.Configuration.Reports.OpenMetrics))
		}
		if v.Configuration.Reports.Sarif != "" {
			reporters = append(reporters, NewSarifReportGenerator(v.Configuration.Reports.Sarif, v.Configuration.Reports.SarifMaxLevel))
		}
	}

	return reporters
}
