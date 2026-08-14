package command

// Analyzing the same sources twice must give the same answer. The pipeline
// parses, analyzes and aggregates on runtime.NumCPU() goroutines, and several
// stages index by a key that many files share, or sum floats, or truncate a
// sorted list: any of those turns "the order the workers happened to finish in"
// into a visible difference in the output.
//
// Both tests below run the real pipeline several times over the same corpus and
// require identical results. They re-parse on every run, because the analyzers
// write back into the *pb.File they visit: replaying the aggregation on the
// same in-memory tree would not measure anything.
//
// The corpus is built so the fragile paths are actually taken: dozens of
// classes share the same orphan weight, dozens of test files share the same
// fan-out, and both lists are longer than the top-20 the testing rules read, so
// the cut falls inside a group of equals.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/ast-metrics/ast-metrics/internal/configuration"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/php"
	"github.com/ast-metrics/ast-metrics/internal/storage"
	pb "github.com/ast-metrics/ast-metrics/pb"
)

const (
	determinismRuns          = 5
	determinismProdClasses   = 60
	determinismTestFiles     = 30
	determinismTestFanOut    = 6
	determinismDuplicateName = 4
)

func TestBaselineGenerationIsReproducible(t *testing.T) {
	srcDir, testDir := writeDeterminismCorpus(t)

	var first []byte
	for run := range determinismRuns {
		got := generateBaseline(t, srcDir, testDir, run)
		if run == 0 {
			first = got
			continue
		}
		if string(got) != string(first) {
			t.Fatalf("run %d produced a different baseline:\n%s", run, firstDifference(string(first), string(got)))
		}
	}

	// A baseline that captured nothing would pass the comparison above without
	// proving anything.
	if !strings.Contains(string(first), "max_orphan_weight") || !strings.Contains(string(first), "max_god_test_fan_out") {
		t.Fatalf("the corpus did not trigger the project-level rules, the test proves nothing:\n%s", first)
	}
}

func TestProjectAggregatesAreReproducible(t *testing.T) {
	srcDir, testDir := writeDeterminismCorpus(t)

	var first string
	for run := range determinismRuns {
		got := snapshotProject(aggregateCorpus(t, srcDir, testDir))
		if run == 0 {
			first = got
			continue
		}
		if got != first {
			t.Fatalf("run %d produced different aggregates:\n%s", run, firstDifference(first, got))
		}
	}
}

// writeDeterminismCorpus lays out a small PHP project in two directories, so
// that the per-directory aggregates are exercised too.
func writeDeterminismCorpus(t *testing.T) (srcDir, testDir string) {
	t.Helper()

	root := t.TempDir()
	srcDir = filepath.Join(root, "src")
	testDir = filepath.Join(root, "tests")
	for _, dir := range []string{srcDir, testDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("cannot create %s: %v", dir, err)
		}
	}

	// Production classes. The complexity only takes three values, so around
	// twenty classes share each orphan weight and the top-20 cut lands in the
	// middle of a tie.
	for i := range determinismProdClasses {
		name := fmt.Sprintf("Service%02d", i)
		var body strings.Builder
		for branch := range 2 + i%3 {
			fmt.Fprintf(&body, "        if ($x === %d) {\n            return %d;\n        }\n", branch, branch)
		}
		write(t, filepath.Join(srcDir, name+".php"), fmt.Sprintf(`<?php

namespace App\Domain;

class %s
{
    public function handle(int $x): int
    {
%s        return -1;
    }
}
`, name, body.String()))
	}

	// Several files declaring the same qualified class name. The test-quality
	// index keeps one entry per name, so whichever file is visited last wins.
	for i := range determinismDuplicateName {
		write(t, filepath.Join(srcDir, fmt.Sprintf("Duplicated%02d.php", i)), fmt.Sprintf(`<?php

namespace App\Domain;

class Duplicated
{
    public function handle(int $x): int
    {
        if ($x === %d) {
            return %d;
        }
        return -1;
    }
}
`, i, i))
	}

	// Test files, all with the same fan-out, and more of them than the top-20
	// god test list can hold.
	for i := range determinismTestFiles {
		var uses, calls strings.Builder
		for j := range determinismTestFanOut {
			target := fmt.Sprintf("Service%02d", (i*determinismTestFanOut+j)%determinismProdClasses)
			fmt.Fprintf(&uses, "use App\\Domain\\%s;\n", target)
			fmt.Fprintf(&calls, "        $sut%d = new %s();\n", j, target)
		}
		name := fmt.Sprintf("Service%02dTest", i)
		write(t, filepath.Join(testDir, name+".php"), fmt.Sprintf(`<?php

namespace App\Tests;

%s
class %s extends \PHPUnit\Framework\TestCase
{
    public function testItRuns(): void
    {
%s    }
}
`, uses.String(), name, calls.String()))
	}

	return srcDir, testDir
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("cannot write %s: %v", path, err)
	}
}

// determinismConfiguration returns a configuration whose thresholds are low
// enough that every rule under test reports something.
func determinismConfiguration(t *testing.T, srcDir, testDir string) *configuration.Configuration {
	t.Helper()

	work := storage.Default()
	work.Purge()
	work.Ensure()

	cfg := configuration.NewConfiguration()
	cfg.Storage = work
	cfg.SourcesToAnalyzePath = []string{srcDir, testDir}
	cfg.Requirements = configuration.NewConfigurationRequirements()

	intOf := func(i int) *int { return &i }
	floatOf := func(f float64) *float64 { return &f }
	cfg.Requirements.Rules.Volume.Loc = intOf(1)
	cfg.Requirements.Rules.Complexity.Cyclomatic = intOf(1)
	cfg.Requirements.Rules.Testing = &configuration.ConfigurationTestingRules{
		MinTraceability:   intOf(90),
		MinIsolationScore: intOf(90),
		MaxGodTestFanOut:  intOf(determinismTestFanOut - 1),
		MaxOrphanWeight:   floatOf(1),
	}

	return cfg
}

func generateBaseline(t *testing.T, srcDir, testDir string, run int) []byte {
	t.Helper()

	path := filepath.Join(t.TempDir(), fmt.Sprintf("baseline-%d.yaml", run))
	// A fresh runner on every run: the engine caches its file list, and reusing
	// one would hide any instability in the discovery order.
	cmd := NewBaselineCommand(determinismConfiguration(t, srcDir, testDir), bufio.NewWriter(os.Stdout), []engine.Engine{&php.PhpRunner{}})
	cmd.Path = path
	if err := cmd.Execute(); err != nil {
		t.Fatalf("run %d: %v", run, err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("run %d: baseline was not written: %v", run, err)
	}
	return data
}

func aggregateCorpus(t *testing.T, srcDir, testDir string) analyzer.ProjectAggregated {
	t.Helper()

	cfg := determinismConfiguration(t, srcDir, testDir)
	runner := &php.PhpRunner{}
	runner.SetConfiguration(cfg)
	if err := runner.Ensure(); err != nil {
		t.Fatalf("cannot prepare the PHP engine: %v", err)
	}
	parsed := runner.DumpAST()
	if err := runner.Finish(); err != nil {
		t.Fatalf("cannot finish the PHP engine: %v", err)
	}
	if len(parsed) == 0 {
		t.Fatalf("no file was parsed, the corpus is not where the engine looks")
	}

	aggregator := analyzer.NewAggregator(analyzer.AnalyzeFiles(parsed, nil), nil)
	aggregator.WithAnalyzedPaths(cfg.SourcesToAnalyzePath)
	return aggregator.Aggregates()
}

// snapshotProject renders everything the reports and the rules read, in a form
// where a single differing bit shows up as a differing line.
func snapshotProject(pa analyzer.ProjectAggregated) string {
	var sb strings.Builder
	snapshotAggregated(&sb, "byFile", pa.ByFile)
	snapshotAggregated(&sb, "byClass", pa.ByClass)
	for _, key := range sortedKeysOf(pa.ByProgrammingLanguage) {
		snapshotAggregated(&sb, "byLanguage["+key+"]", pa.ByProgrammingLanguage[key])
	}
	for _, key := range sortedKeysOf(pa.ByDirectory) {
		snapshotAggregated(&sb, "byDirectory["+key+"]", pa.ByDirectory[key])
	}
	return sb.String()
}

func snapshotAggregated(sb *strings.Builder, prefix string, a analyzer.Aggregated) {
	// Reflection rather than a hand-written list, so a metric added later is
	// covered without anyone having to remember this file.
	value := reflect.ValueOf(a)
	for i, field := range reflect.VisibleFields(value.Type()) {
		if !field.IsExported() {
			continue
		}
		switch typed := value.Field(i).Interface().(type) {
		case analyzer.AggregateResult:
			fmt.Fprintf(sb, "%s.%s sum=%v min=%v max=%v avg=%v n=%d\n", prefix, field.Name, typed.Sum, typed.Min, typed.Max, typed.Avg, typed.Counter)
		case int:
			fmt.Fprintf(sb, "%s.%s=%d\n", prefix, field.Name, typed)
		case float64:
			fmt.Fprintf(sb, "%s.%s=%v\n", prefix, field.Name, typed)
		}
	}

	for i, file := range a.ConcernedFiles {
		fmt.Fprintf(sb, "%s.file[%d]=%s %s\n", prefix, i, file.GetPath(), snapshotCoupling(file))
	}

	if a.Graph != nil {
		for _, id := range sortedKeysOf(a.Graph.Nodes) {
			fmt.Fprintf(sb, "%s.node[%s]=%s\n", prefix, id, strings.Join(a.Graph.Nodes[id].GetEdges(), ","))
		}
	}

	for _, source := range sortedKeysOf(a.FileDependencies.Efferent) {
		fmt.Fprintf(sb, "%s.efferent[%s]=%s\n", prefix, source, strings.Join(a.FileDependencies.Efferent[source], ","))
	}
	for _, target := range sortedKeysOf(a.FileDependencies.Afferent) {
		fmt.Fprintf(sb, "%s.afferent[%s]=%s\n", prefix, target, strings.Join(a.FileDependencies.Afferent[target], ","))
	}

	snapshotTestQuality(sb, prefix, a.TestQuality)
	snapshotCommunity(sb, prefix, a.Community)
}

func snapshotCoupling(file *pb.File) string {
	coupling := file.GetStmts().GetAnalyze().GetCoupling()
	if coupling == nil {
		return "coupling=none"
	}
	return fmt.Sprintf("afferent=%d efferent=%d instability=%v", coupling.GetAfferent(), coupling.GetEfferent(), coupling.GetInstability())
}

func snapshotTestQuality(sb *strings.Builder, prefix string, tq *analyzer.TestQualityMetrics) {
	if tq == nil {
		return
	}
	fmt.Fprintf(sb, "%s.tq isolation=%v traceability=%v tested=%d classes=%d\n", prefix, tq.GlobalIsolationScore, tq.TraceabilityPct, tq.NbTestedClasses, tq.NbProdClasses)
	for i, godTest := range tq.GodTests {
		fmt.Fprintf(sb, "%s.tq.godTest[%d]=%s fanOut=%d\n", prefix, i, godTest.FilePath, godTest.SUTFanOut)
	}
	for i, orphan := range tq.OrphanClasses {
		fmt.Fprintf(sb, "%s.tq.orphan[%d]=%s weight=%v file=%s\n", prefix, i, orphan.ClassName, orphan.Weight, orphan.FilePath)
	}
}

func snapshotCommunity(sb *strings.Builder, prefix string, community *analyzer.CommunityMetrics) {
	if community == nil {
		return
	}
	fmt.Fprintf(sb, "%s.community count=%d avgSize=%v maxSize=%d density=%v modularity=%v\n",
		prefix, community.CommunitiesCount, community.AvgSize, community.MaxSize, community.GraphDensity, community.ModularityQ)
	fmt.Fprintf(sb, "%s.community.matrixOrder=%s\n", prefix, strings.Join(community.MatrixOrder, ","))
	fmt.Fprintf(sb, "%s.community.boundaryNodes=%s\n", prefix, strings.Join(community.BoundaryNodes, ","))
	for _, id := range sortedKeysOf(community.Communities) {
		fmt.Fprintf(sb, "%s.community[%s]=%s purity=%v inbound=%d outbound=%d\n",
			prefix, id, strings.Join(community.Communities[id], ","),
			community.PurityPerCommunity[id], community.InboundEdgesPerComm[id], community.OutboundEdgesPerComm[id])
	}
	for i, edge := range community.EdgesBetweenCommunities {
		fmt.Fprintf(sb, "%s.community.edge[%d]=%v\n", prefix, i, edge)
	}
}

func sortedKeysOf[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

// firstDifference points at the first differing line, so a failure reads as one
// line instead of a whole report.
func firstDifference(expected, actual string) string {
	expectedLines := strings.Split(expected, "\n")
	actualLines := strings.Split(actual, "\n")
	for i := range max(len(expectedLines), len(actualLines)) {
		var left, right string
		if i < len(expectedLines) {
			left = expectedLines[i]
		}
		if i < len(actualLines) {
			right = actualLines[i]
		}
		if left != right {
			return fmt.Sprintf("line %d\n  first run: %s\n  this run:  %s", i+1, left, right)
		}
	}
	return "the two outputs are identical, which contradicts the comparison above"
}
