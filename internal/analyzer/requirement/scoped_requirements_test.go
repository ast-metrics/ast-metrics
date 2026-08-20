package requirement

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/configuration"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/stretchr/testify/assert"
)

// fileWithComplexity builds the smallest file the cyclomatic rule can judge.
func fileWithComplexity(path string, complexity int32) *pb.File {
	return &pb.File{
		Path: path,
		Stmts: &pb.Stmts{
			Analyze: &pb.Analyze{
				Complexity: &pb.Complexity{Cyclomatic: &complexity},
			},
		},
	}
}

func writeConfiguration(t *testing.T, directory string, content string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, ".ast-metrics.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScopedRequirementsJudgeEachProjectByItsOwnRules(t *testing.T) {
	root := t.TempDir()
	front := filepath.Join(root, "front")
	back := filepath.Join(root, "back")

	// front is strict, back holds no configuration and falls back on the root
	// one, which is lenient.
	writeConfiguration(t, front, `
requirements:
  rules:
    complexity:
      max_cyclomatic: 5`)
	if err := os.MkdirAll(back, 0o755); err != nil {
		t.Fatal(err)
	}

	loader := configuration.NewConfigurationLoader()
	config, err := loader.Import(`
requirements:
  rules:
    complexity:
      max_cyclomatic: 50`)
	assert.Nil(t, err)

	config.SourcesToAnalyzePath = []string{front, back}
	assert.Nil(t, config.ResolveScopes())

	files := []*pb.File{
		fileWithComplexity(filepath.Join(front, "a.go"), 10),
		fileWithComplexity(filepath.Join(back, "b.go"), 10),
	}

	evaluation := NewScopedRequirementsEvaluator(config).Evaluate(files, ProjectAggregated{})

	// Only the front file breaks its own budget. The back file is judged by the
	// root configuration, which allows it.
	assert.Equal(t, 1, len(evaluation.Errors))
	assert.Equal(t, filepath.Join(front, "a.go"), evaluation.Errors[0].File)
}

func TestScopedRequirementsGiveNestedProjectItsOwnFiles(t *testing.T) {
	root := t.TempDir()
	front := filepath.Join(root, "front")
	writeConfiguration(t, front, `
requirements:
  rules:
    complexity:
      max_cyclomatic: 5`)

	loader := configuration.NewConfigurationLoader()
	config, err := loader.Import(`
requirements:
  rules:
    complexity:
      max_cyclomatic: 50`)
	assert.Nil(t, err)

	// The root is analyzed too, and contains front. The most specific source
	// owns its files.
	config.SourcesToAnalyzePath = []string{root, front}
	assert.Nil(t, config.ResolveScopes())

	files := []*pb.File{
		fileWithComplexity(filepath.Join(root, "a.go"), 10),
		fileWithComplexity(filepath.Join(front, "b.go"), 10),
	}

	evaluation := NewScopedRequirementsEvaluator(config).Evaluate(files, ProjectAggregated{})

	assert.Equal(t, 1, len(evaluation.Errors))
	assert.Equal(t, filepath.Join(front, "b.go"), evaluation.Errors[0].File)
}

func TestRequirementsExcludeSkipsTheMatchingFiles(t *testing.T) {
	loader := configuration.NewConfigurationLoader()
	config, err := loader.Import(`
requirements:
  exclude:
    - /tests/
  rules:
    complexity:
      max_cyclomatic: 5`)
	assert.Nil(t, err)

	files := []*pb.File{
		fileWithComplexity("/app/src/a.go", 10),
		fileWithComplexity("/app/tests/b.go", 10),
	}

	evaluation := NewRequirementsEvaluator(*config.Requirements).Evaluate(files, ProjectAggregated{})

	assert.Equal(t, 1, len(evaluation.Errors))
	assert.Equal(t, "/app/src/a.go", evaluation.Errors[0].File)
}

func TestRequirementsExcludeIgnoresAnUnusablePattern(t *testing.T) {
	loader := configuration.NewConfigurationLoader()
	config, err := loader.Import(`
requirements:
  exclude:
    - "[unclosed"
  rules:
    complexity:
      max_cyclomatic: 5`)
	assert.Nil(t, err)

	files := []*pb.File{fileWithComplexity("/app/src/a.go", 10)}

	// The pattern cannot be compiled: it is dropped, and the analysis goes on.
	evaluation := NewRequirementsEvaluator(*config.Requirements).Evaluate(files, ProjectAggregated{})

	assert.Equal(t, 1, len(evaluation.Errors))
}

func TestScopeConfigurationIsReadForItsOwnFilesOnly(t *testing.T) {
	root := t.TempDir()
	front := filepath.Join(root, "front")
	writeConfiguration(t, front, `
sources:
  - ./
comparewith: main
watching: true
reports:
  markdown: ./front-report.md
requirements:
  baseline: ./front-baseline.yaml
  rules:
    complexity:
      max_cyclomatic: 5`)

	config := configuration.NewConfiguration()
	config.SourcesToAnalyzePath = []string{front}
	assert.Nil(t, config.ResolveScopes())

	assert.Equal(t, 1, len(config.Scopes))
	scoped := config.Scopes[0].Configuration

	// Kept: what the file says about its own files.
	assert.Equal(t, 5, *scoped.Requirements.Rules.Complexity.Cyclomatic)

	// Dropped: what describes a run rather than a scope. The same file has to
	// keep working when that project is analyzed on its own.
	assert.Empty(t, scoped.SourcesToAnalyzePath)
	assert.Equal(t, "", scoped.CompareWith)
	assert.Equal(t, false, scoped.Watching)
	assert.Equal(t, "", scoped.Reports.Markdown)
	assert.Equal(t, "", scoped.Requirements.Baseline)
}

func TestScopeConfigurationKeepsTheDefaultExcludes(t *testing.T) {
	root := t.TempDir()
	front := filepath.Join(root, "front")
	writeConfiguration(t, front, `
requirements:
  rules:
    complexity:
      max_cyclomatic: 5`)

	config := configuration.NewConfiguration()
	config.SourcesToAnalyzePath = []string{front}
	assert.Nil(t, config.ResolveScopes())

	assert.Contains(t, config.Scopes[0].Configuration.ExcludePatterns, "/vendor/")
}

func BenchmarkEvaluate(b *testing.B) {
	loader := configuration.NewConfigurationLoader()
	config, err := loader.Import(`
requirements:
  exclude:
    - /tests/
  rules:
    complexity:
      max_cyclomatic: 5
    volume:
      max_loc: 100`)
	if err != nil {
		b.Fatal(err)
	}

	files := make([]*pb.File, 0, 2000)
	for index := 0; index < 2000; index++ {
		files = append(files, fileWithComplexity(filepath.Join("/app/src", "file.go"), int32(index%20)))
	}

	evaluator := NewRequirementsEvaluator(*config.Requirements)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		evaluator.Evaluate(files, ProjectAggregated{})
	}
}
