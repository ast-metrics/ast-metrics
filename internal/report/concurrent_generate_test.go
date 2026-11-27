package report

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerate_ConcurrentRunsDoNotShareTemplates checks that two reports
// generated at the same time each render in full. They used to materialize the
// embedded templates in one directory shared by every run, so one truncated
// the files the other was still loading.
//
// Only the rendering runs concurrently: the aggregator mutates the files it is
// given, so each run gets its own inputs, built up front.
func TestGenerate_ConcurrentRunsDoNotShareTemplates(t *testing.T) {
	const runs = 4

	type input struct {
		dir   string
		files []*pb.File
		pa    analyzer.ProjectAggregated
	}
	inputs := make([]input, runs)
	for i := range inputs {
		files := []*pb.File{{Path: "/project/src/a.go", ProgrammingLanguage: "Golang", Stmts: &pb.Stmts{}}}
		inputs[i] = input{
			dir:   t.TempDir(),
			files: files,
			pa:    analyzer.NewAggregator(files, nil).Aggregates(),
		}
	}

	errs := make([]error, runs)
	var wg sync.WaitGroup
	for i := range inputs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = NewHtmlReportGenerator(inputs[i].dir).Generate(inputs[i].files, inputs[i].pa)
		}(i)
	}
	wg.Wait()

	for i, in := range inputs {
		require.NoError(t, errs[i], "run %d", i)
		for _, page := range []string{"index.html", "communities.html", "risks.html"} {
			body, err := os.ReadFile(filepath.Join(in.dir, page))
			require.NoError(t, err, "run %d, page %s", i, page)
			assert.NotEmpty(t, body, "run %d, page %s", i, page)
		}
	}
}
