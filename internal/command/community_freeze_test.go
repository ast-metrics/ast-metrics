package command

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	requirement "github.com/ast-metrics/ast-metrics/internal/analyzer/requirement"
	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/php"
	"github.com/ast-metrics/ast-metrics/internal/storage"
)

// writePhpModule writes four tightly linked classes of a namespace into dir:
// A uses B and C, B uses C and D, C uses D. Extra uses let a class depend on
// classes outside the module.
func writePhpModule(t *testing.T, dir, namespace string, extra map[string][]string) {
	t.Helper()
	deps := map[string][]string{
		"A": {namespace + `\B`, namespace + `\C`},
		"B": {namespace + `\C`, namespace + `\D`},
		"C": {namespace + `\D`},
		"D": {},
	}
	folder := filepath.Join(dir, filepath.Base(strings.ReplaceAll(namespace, `\`, "/")))
	if err := os.MkdirAll(folder, 0755); err != nil {
		t.Fatalf("cannot create %s: %v", folder, err)
	}
	for _, name := range []string{"A", "B", "C", "D"} {
		uses := append(append([]string{}, deps[name]...), extra[name]...)
		var sb strings.Builder
		fmt.Fprintf(&sb, "<?php\nnamespace %s;\n\n", namespace)
		for _, u := range uses {
			fmt.Fprintf(&sb, "use %s;\n", u)
		}
		fmt.Fprintf(&sb, "\nfinal class %s {\n    public function __construct(", name)
		for i, u := range uses {
			if i > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(&sb, "%s $p%d", u[strings.LastIndex(u, `\`)+1:], i)
		}
		sb.WriteString(") {}\n}\n")
		if err := os.WriteFile(filepath.Join(folder, name+".php"), []byte(sb.String()), 0644); err != nil {
			t.Fatalf("cannot write %s: %v", name, err)
		}
	}
}

// evaluateCrossings runs the same pipeline as lint on cfg and returns the
// evaluation, so the test can count the errors instead of reading an exit code.
func evaluateCrossings(t *testing.T, cfg *configuration.Configuration) requirement.EvaluationResult {
	t.Helper()
	runner := &php.PhpRunner{}
	runner.SetConfiguration(cfg)
	if err := runner.Ensure(); err != nil {
		t.Fatalf("engine: %v", err)
	}
	files := runner.DumpAST()
	if err := runner.Finish(); err != nil {
		t.Fatalf("engine: %v", err)
	}
	results := analyzer.AnalyzeFiles(files, nil)
	aggregated := analyzer.NewAggregator(results, nil).Aggregates()
	evaluator := requirement.NewRequirementsEvaluator(*cfg.Requirements)
	if path := requirement.ResolveBaselinePath(cfg.Requirements.Baseline); path != "" {
		baseline, err := requirement.LoadBaseline(path)
		if err != nil {
			t.Fatalf("baseline: %v", err)
		}
		evaluator.Baseline = baseline
	}
	return evaluator.Evaluate(results, requirement.ProjectAggregated{ProjectCtx: buildProjectContext(aggregated)})
}

func TestNoCrossCommunityDependencies_FreezesTheCrossingsWithTheBaseline(t *testing.T) {
	work := storage.Default()
	work.Purge()
	work.Ensure()

	src := t.TempDir()
	writePhpModule(t, src, `App\Billing`, map[string][]string{"A": {`App\Catalog\D`}})
	writePhpModule(t, src, `App\Catalog`, nil)

	// Only a project rule is configured: no per-file rule at all.
	cfg := configuration.NewConfiguration()
	cfg.Storage = work
	cfg.Requirements = configuration.NewConfigurationRequirements()
	enabled := true
	cfg.Requirements.Rules.Architecture.NoCrossCommunityDependencies = &enabled
	cfg.SourcesToAnalyzePath = []string{src}
	runners := []engine.Engine{&php.PhpRunner{}}
	outWriter := bufio.NewWriter(os.Stdout)

	// Today: one crossing, Billing\A → Catalog\D, reported with its file.
	before := evaluateCrossings(t, cfg)
	if len(before.Errors) != 1 {
		t.Fatalf("expected one crossing, got %+v", before.Errors)
	}
	crossing := before.Errors[0]
	if crossing.Rule != "no_cross_community_dependencies" {
		t.Errorf("unexpected rule: %s", crossing.Rule)
	}
	if !strings.HasSuffix(crossing.File, filepath.Join("Billing", "A.php")) {
		t.Errorf("the referencing file should be named, got %q", crossing.File)
	}
	if crossing.Message != "A depends on D, which sits in another community" {
		t.Errorf("unexpected message: %s", crossing.Message)
	}
	if err := NewLintCommand(cfg, outWriter, runners).Execute(); err == nil {
		t.Fatalf("lint should fail before the baseline")
	}

	// Freeze it.
	baselinePath := filepath.Join(t.TempDir(), "baseline.yaml")
	baselineCmd := NewBaselineCommand(cfg, outWriter, runners)
	baselineCmd.Path = baselinePath
	if err := baselineCmd.Execute(); err != nil {
		t.Fatalf("baseline: %v", err)
	}
	data, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("baseline file was not written: %v", err)
	}
	if !strings.Contains(string(data), "rule: no_cross_community_dependencies") || !strings.Contains(string(data), crossing.Message) {
		t.Fatalf("the baseline should hold the crossing:\n%s", data)
	}

	// Nothing new: lint passes and the crossing counts as frozen.
	cfg.Requirements.Baseline = baselinePath
	frozen := evaluateCrossings(t, cfg)
	if len(frozen.Errors) != 0 {
		t.Fatalf("expected no new crossing, got %+v", frozen.Errors)
	}
	if frozen.BaselinedByRule["no_cross_community_dependencies"] != 1 {
		t.Errorf("expected one frozen crossing, got %v", frozen.BaselinedByRule)
	}
	if err := NewLintCommand(cfg, outWriter, runners).Execute(); err != nil {
		t.Fatalf("lint should pass once the crossing is frozen: %v", err)
	}

	// A new crossing, Billing\B → Catalog\C: exactly one error, the old one
	// still frozen.
	writePhpModule(t, src, `App\Billing`, map[string][]string{"A": {`App\Catalog\D`}, "B": {`App\Catalog\C`}})
	after := evaluateCrossings(t, cfg)
	if len(after.Errors) != 1 {
		t.Fatalf("expected exactly one new crossing, got %+v", after.Errors)
	}
	if after.Errors[0].Message != "B depends on C, which sits in another community" {
		t.Errorf("unexpected new crossing: %s", after.Errors[0].Message)
	}
	if after.BaselinedByRule["no_cross_community_dependencies"] != 1 {
		t.Errorf("the first crossing should stay frozen, got %v", after.BaselinedByRule)
	}
	if err := NewLintCommand(cfg, outWriter, runners).Execute(); err == nil {
		t.Fatalf("lint should fail on the new crossing")
	}
}
