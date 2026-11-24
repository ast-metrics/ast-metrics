package golang

import (
	"path/filepath"
	"testing"
)

// BenchmarkParseLargeFiles parses the largest Go sources of this repository,
// which is what a run spends its time on when the analysis itself is cached.
func BenchmarkParseLargeFiles(b *testing.B) {
	root := filepath.Join("..", "..", "..")
	files := []string{
		filepath.Join(root, "internal", "report", "html_report_generator.go"),
		filepath.Join(root, "internal", "analyzer", "community_aggregator.go"),
		filepath.Join(root, "internal", "analyzer", "aggregator.go"),
		filepath.Join(root, "internal", "command", "ai_dataset_command.go"),
		filepath.Join(root, "cmd", "ast-metrics", "main.go"),
	}
	r := &GolangRunner{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, f := range files {
			if _, err := r.Parse(f); err != nil {
				b.Fatal(err)
			}
		}
	}
}
