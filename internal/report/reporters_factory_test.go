package report

import (
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/stretchr/testify/assert"
)

func TestShouldAbleToFactoryReporters(t *testing.T) {
	t.Run("Should able to factory enabled reports", func(t *testing.T) {
		config := configuration.Configuration{}
		config.Reports = configuration.ConfigurationReport{
			Html:        "foo",
			Markdown:    "foo.md",
			Json:        "foo.json",
			OpenMetrics: "foo.txt",
		}

		factory := ReportersFactory{Configuration: &config}
		reporters := factory.Factory(&config)
		assert.Equal(t, 4, len(reporters))
	})

	t.Run("Should able to factory when no report is configured", func(t *testing.T) {
		config := configuration.Configuration{}
		factory := ReportersFactory{Configuration: &config}
		reporters := factory.Factory(&config)
		assert.Equal(t, 0, len(reporters))
	})

	t.Run("Should able to factory when only html report is configured", func(t *testing.T) {
		config := configuration.Configuration{}
		config.Reports = configuration.ConfigurationReport{
			Html:     "foo",
			Markdown: "",
		}

		factory := ReportersFactory{Configuration: &config}
		reporters := factory.Factory(&config)
		assert.Equal(t, 1, len(reporters))
		assert.Equal(t, "foo", reporters[0].(*HtmlReportGenerator).ReportPath)
	})

	t.Run("Should able to factory when only markdown report is configured", func(t *testing.T) {
		config := configuration.Configuration{}
		config.Reports = configuration.ConfigurationReport{
			Html:     "",
			Markdown: "foo.md",
		}

		factory := ReportersFactory{Configuration: &config}
		reporters := factory.Factory(&config)
		assert.Equal(t, 1, len(reporters))
		assert.Equal(t, "foo.md", reporters[0].(*MarkdownReportGenerator).ReportPath)
	})

	t.Run("Should pass the function rows option to the json report", func(t *testing.T) {
		config := configuration.Configuration{}
		config.Reports = configuration.ConfigurationReport{Json: "foo.json", JsonFunctions: true}

		factory := ReportersFactory{Configuration: &config}
		reporters := factory.Factory(&config)
		assert.Equal(t, 1, len(reporters))
		assert.True(t, reporters[0].(*JsonReportGenerator).Functions)
	})
}
