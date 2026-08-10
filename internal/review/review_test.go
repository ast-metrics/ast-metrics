package review

import (
	"encoding/json"
	"strings"
	"testing"

	requirement "github.com/ast-metrics/ast-metrics/internal/analyzer/requirement"
	pb "github.com/ast-metrics/ast-metrics/pb"
	"github.com/stretchr/testify/assert"
)

func newFunction(name string, ccn int32, line int32) *pb.StmtFunction {
	c := ccn
	return &pb.StmtFunction{
		Name:     &pb.Name{Short: name},
		Location: &pb.StmtLocationInFile{StartLine: line},
		Stmts: &pb.Stmts{
			Analyze: &pb.Analyze{
				Complexity: &pb.Complexity{Cyclomatic: &c},
			},
		},
	}
}

func newFile(path string, checksum string, functions ...*pb.StmtFunction) *pb.File {
	return &pb.File{
		Path:     path,
		Checksum: checksum,
		Stmts: &pb.Stmts{
			StmtFunction: functions,
			Analyze:      &pb.Analyze{},
		},
	}
}

func withMaintainability(file *pb.File, mi float64) *pb.File {
	v := mi
	file.Stmts.Analyze.Maintainability = &pb.Maintainability{MaintainabilityIndex: &v}
	return file
}

func withCoupling(file *pb.File, efferent int32) *pb.File {
	file.Stmts.Analyze.Coupling = &pb.Coupling{Efferent: efferent}
	return file
}

func TestCompareDetectsComplexityRegression(t *testing.T) {
	base := []*pb.File{newFile("/base/svc.go", "aaa", newFunction("Pay", 8, 10))}
	head := []*pb.File{newFile("/head/svc.go", "bbb", newFunction("Pay", 15, 10))}

	result := Compare(head, base, "/head", "/base", DefaultOptions())

	assert.Equal(t, 1, result.Summary.FilesChanged)
	assert.Len(t, result.Regressions, 1)
	finding := result.Regressions[0]
	assert.Equal(t, "complexity-regression", finding.Rule)
	assert.Equal(t, "svc.go", finding.File)
	assert.Equal(t, "Pay", finding.Subject)
	assert.Equal(t, SeverityMedium, finding.Severity)
	assert.Contains(t, finding.Message, "8 -> 15")
	assert.Equal(t, 10, finding.Line)
}

func TestCompareComplexityJumpIsHigh(t *testing.T) {
	base := []*pb.File{newFile("/base/svc.go", "aaa", newFunction("Pay", 5, 1))}
	head := []*pb.File{newFile("/head/svc.go", "bbb", newFunction("Pay", 16, 1))}

	result := Compare(head, base, "/head", "/base", DefaultOptions())

	assert.Len(t, result.Regressions, 1)
	assert.Equal(t, SeverityHigh, result.Regressions[0].Severity)
}

func TestCompareIgnoresUnchangedDebt(t *testing.T) {
	// the function is complex in both versions, with the same value: not reported
	base := []*pb.File{newFile("/base/svc.go", "aaa", newFunction("Pay", 30, 1))}
	head := []*pb.File{newFile("/head/svc.go", "bbb", newFunction("Pay", 30, 1))}

	result := Compare(head, base, "/head", "/base", DefaultOptions())

	assert.Empty(t, result.Regressions)
}

func TestCompareSkipsFilesWithSameChecksum(t *testing.T) {
	base := []*pb.File{newFile("/base/svc.go", "same", newFunction("Pay", 30, 1))}
	head := []*pb.File{newFile("/head/svc.go", "same", newFunction("Pay", 50, 1))}

	result := Compare(head, base, "/head", "/base", DefaultOptions())

	assert.Empty(t, result.Regressions)
	assert.Equal(t, 0, result.Summary.FilesChanged)
}

func TestCompareDetectsNewComplexFunctionInNewFile(t *testing.T) {
	head := []*pb.File{newFile("/head/new.go", "bbb", newFunction("Handle", 26, 3))}

	result := Compare(head, nil, "/head", "/base", DefaultOptions())

	assert.Equal(t, 1, result.Summary.FilesAdded)
	assert.Len(t, result.Regressions, 1)
	assert.Equal(t, "new-complex-function", result.Regressions[0].Rule)
	assert.Equal(t, SeverityHigh, result.Regressions[0].Severity)
}

func TestCompareDetectsNewComplexFunctionInModifiedFile(t *testing.T) {
	base := []*pb.File{newFile("/base/svc.go", "aaa", newFunction("Pay", 2, 1))}
	head := []*pb.File{newFile("/head/svc.go", "bbb", newFunction("Pay", 2, 1), newFunction("Refund", 12, 30))}

	result := Compare(head, base, "/head", "/base", DefaultOptions())

	assert.Len(t, result.Regressions, 1)
	assert.Equal(t, "new-complex-function", result.Regressions[0].Rule)
	assert.Equal(t, "Refund", result.Regressions[0].Subject)
}

func TestCompareDetectsMaintainabilityRegression(t *testing.T) {
	base := []*pb.File{withMaintainability(newFile("/base/svc.go", "aaa"), 80)}
	head := []*pb.File{withMaintainability(newFile("/head/svc.go", "bbb"), 60)}

	result := Compare(head, base, "/head", "/base", DefaultOptions())

	assert.Len(t, result.Regressions, 1)
	assert.Equal(t, "maintainability-regression", result.Regressions[0].Rule)
	assert.Equal(t, SeverityHigh, result.Regressions[0].Severity)
}

func TestCompareDetectsCouplingRegression(t *testing.T) {
	base := []*pb.File{withCoupling(newFile("/base/svc.go", "aaa"), 2)}
	head := []*pb.File{withCoupling(newFile("/head/svc.go", "bbb"), 6)}

	result := Compare(head, base, "/head", "/base", DefaultOptions())

	assert.Len(t, result.Regressions, 1)
	assert.Equal(t, "coupling-regression", result.Regressions[0].Rule)
}

func TestCompareDetectsImprovements(t *testing.T) {
	base := []*pb.File{newFile("/base/svc.go", "aaa", newFunction("Pay", 15, 1))}
	head := []*pb.File{newFile("/head/svc.go", "bbb", newFunction("Pay", 5, 1))}

	result := Compare(head, base, "/head", "/base", DefaultOptions())

	assert.Empty(t, result.Regressions)
	assert.Len(t, result.Improvements, 1)
	assert.Equal(t, "complexity-improvement", result.Improvements[0].Rule)
}

func TestCompareCountsDeletedFiles(t *testing.T) {
	base := []*pb.File{newFile("/base/old.go", "aaa")}

	result := Compare(nil, base, "/head", "/base", DefaultOptions())

	assert.Equal(t, 1, result.Summary.FilesDeleted)
}

func TestCompareMatchesMethodsInsideClasses(t *testing.T) {
	build := func(root string, checksum string, ccn int32) *pb.File {
		file := newFile(root+"/svc.php", checksum)
		file.Stmts.StmtClass = []*pb.StmtClass{
			{
				Name: &pb.Name{Short: "CheckoutService"},
				Stmts: &pb.Stmts{
					StmtFunction: []*pb.StmtFunction{newFunction("pay", ccn, 42)},
				},
			},
		}
		return file
	}
	base := []*pb.File{build("/base", "aaa", 8)}
	head := []*pb.File{build("/head", "bbb", 15)}

	result := Compare(head, base, "/head", "/base", DefaultOptions())

	assert.Len(t, result.Regressions, 1)
	assert.Equal(t, "CheckoutService::pay", result.Regressions[0].Subject)
}

func TestCompareDoesNotDuplicateMethodsExposedAtFileLevel(t *testing.T) {
	// The PHP engine exposes class methods both inside the class and at
	// file level: the review must report each method once, with its
	// qualified name.
	build := func(root string, checksum string, ccn int32) *pb.File {
		method := newFunction("describeVerb", ccn, 40)
		file := newFile(root+"/EventLogger.php", checksum, method)
		file.Stmts.StmtClass = []*pb.StmtClass{
			{
				Name:  &pb.Name{Short: "EventLogger"},
				Stmts: &pb.Stmts{StmtFunction: []*pb.StmtFunction{method}},
			},
		}
		return file
	}
	base := []*pb.File{build("/base", "aaa", 2)}
	head := []*pb.File{build("/head", "bbb", 17)}

	result := Compare(head, base, "/head", "/base", DefaultOptions())

	assert.Len(t, result.Regressions, 1)
	assert.Equal(t, "EventLogger::describeVerb", result.Regressions[0].Subject)
}

func TestFindingsAreSortedBySeverityThenFile(t *testing.T) {
	base := []*pb.File{
		newFile("/base/a.go", "a1", newFunction("A", 8, 1)),
		newFile("/base/z.go", "z1", newFunction("Z", 8, 1)),
	}
	head := []*pb.File{
		newFile("/head/a.go", "a2", newFunction("A", 12, 1)),  // medium
		newFile("/head/z.go", "z2", newFunction("Z", 30, 1)),  // high
	}

	result := Compare(head, base, "/head", "/base", DefaultOptions())

	assert.Len(t, result.Regressions, 2)
	assert.Equal(t, SeverityHigh, result.Regressions[0].Severity)
	assert.Equal(t, "z.go", result.Regressions[0].File)
	assert.Equal(t, "a.go", result.Regressions[1].File)
}

func TestEvaluateGate(t *testing.T) {
	result := Result{Regressions: []Finding{{Severity: SeverityMedium}}}

	assert.Equal(t, "passed", result.EvaluateGate("never"))
	assert.Equal(t, "passed", result.EvaluateGate(""))
	assert.Equal(t, "passed", result.EvaluateGate("high"))
	assert.Equal(t, "failed", result.EvaluateGate("medium"))
	assert.Equal(t, "failed", result.EvaluateGate("any"))
}

func TestDiffLintKeepsOnlyNewViolations(t *testing.T) {
	baseOutcomes := []requirement.RuleOutcome{
		{Rule: "coupling", File: "/base/a.php", Message: "Coupling of 12 is too high in file /base/a.php"},
	}
	headOutcomes := []requirement.RuleOutcome{
		// same violation, metric drifted: still existing debt
		{Rule: "coupling", File: "/head/a.php", Message: "Coupling of 14 is too high in file /head/a.php"},
		// genuinely new violation
		{Rule: "forbidden-dependency", File: "/head/b.php", Message: "Controller -> Repository is forbidden", Severity: requirement.SeverityHigh},
	}

	findings := DiffLint(headOutcomes, baseOutcomes, "/head", "/base")

	assert.Len(t, findings, 1)
	assert.Equal(t, "new-violation:forbidden-dependency", findings[0].Rule)
	assert.Equal(t, "b.php", findings[0].File)
	assert.Equal(t, SeverityHigh, findings[0].Severity)
}

func TestMarkdownOutput(t *testing.T) {
	base := []*pb.File{newFile("/base/svc.go", "aaa", newFunction("Pay", 8, 10))}
	head := []*pb.File{newFile("/head/svc.go", "bbb", newFunction("Pay", 15, 10))}

	result := Compare(head, base, "/head", "/base", DefaultOptions())
	result.BaseRef = "origin/main"
	result.Gate = result.EvaluateGate("never")

	md := result.Markdown(5)
	assert.Contains(t, md, "AST Metrics found regressions")
	assert.Contains(t, md, "Pay")
	assert.Contains(t, md, "svc.go:10")
	assert.Contains(t, md, "Complexity: +7")
}

func TestTextOutputCapsFindings(t *testing.T) {
	head := []*pb.File{}
	base := []*pb.File{}
	for _, name := range []string{"a", "b", "c"} {
		base = append(base, newFile("/base/"+name+".go", name+"1", newFunction(name, 8, 1)))
		head = append(head, newFile("/head/"+name+".go", name+"2", newFunction(name, 15, 1)))
	}

	result := Compare(head, base, "/head", "/base", DefaultOptions())
	result.Gate = "passed"

	text := result.Text(2)
	assert.Contains(t, text, "and 1 more")
	assert.Equal(t, 2, strings.Count(text, "Suggested action"))
}

func TestJSONOutputIsComplete(t *testing.T) {
	base := []*pb.File{newFile("/base/svc.go", "aaa", newFunction("Pay", 8, 10))}
	head := []*pb.File{newFile("/head/svc.go", "bbb", newFunction("Pay", 15, 10))}

	result := Compare(head, base, "/head", "/base", DefaultOptions())
	result.Gate = "passed"

	out, err := result.JSON()
	assert.NoError(t, err)

	var decoded Result
	assert.NoError(t, json.Unmarshal([]byte(out), &decoded))
	assert.Equal(t, MethodologyVersion, decoded.MethodologyVersion)
	assert.Len(t, decoded.Regressions, 1)
}

func TestToRuleOutcomes(t *testing.T) {
	result := Result{Regressions: []Finding{
		{Rule: "complexity-regression", File: "svc.go", Line: 10, Subject: "Pay", Message: "Cyclomatic complexity: 8 -> 15", Severity: SeverityHigh},
	}}

	outcomes := result.ToRuleOutcomes()

	assert.Len(t, outcomes, 1)
	assert.Equal(t, "complexity-regression", outcomes[0].Rule)
	assert.Equal(t, "Pay: Cyclomatic complexity: 8 -> 15", outcomes[0].Message)
	assert.Equal(t, requirement.SeverityHigh, outcomes[0].Severity)
}
