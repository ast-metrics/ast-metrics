package analyzer

import (
	"testing"

	pb "github.com/ast-metrics/ast-metrics/pb"
)

// method builds a method using the given attributes, in a way that is close to what
// the parsers produce (operands prefixed with "this.").
func method(class string, name string, attributes ...string) *pb.StmtFunction {
	operands := make([]*pb.StmtOperand, 0, len(attributes))
	for _, attribute := range attributes {
		operands = append(operands, &pb.StmtOperand{Name: "this." + attribute})
	}

	lloc := int32(len(attributes))
	return &pb.StmtFunction{
		Name:        &pb.Name{Short: name, Qualified: class + "::" + name},
		Operands:    operands,
		LinesOfCode: &pb.LinesOfCode{LogicalLinesOfCode: lloc},
	}
}

func classWith(name string, methods ...*pb.StmtFunction) *pb.Stmts {
	return &pb.Stmts{
		StmtClass: []*pb.StmtClass{
			{
				Name: &pb.Name{Short: name, Qualified: name},
				Stmts: &pb.Stmts{
					StmtFunction: methods,
					Analyze:      &pb.Analyze{},
				},
			},
		},
		Analyze: &pb.Analyze{},
	}
}

func lcom4Of(t *testing.T, stmts *pb.Stmts) int32 {
	t.Helper()

	cohesion := stmts.StmtClass[0].Stmts.Analyze.ClassCohesion
	if cohesion == nil || cohesion.Lcom4 == nil {
		t.Fatal("LCOM4 has not been measured")
	}
	return *cohesion.Lcom4
}

func TestLifecycleMethodsAreExcludedPerLanguage(t *testing.T) {
	// In each case, useA and useB are two independent responsibilities, and the
	// lifecycle method touches both attributes.
	cases := []struct {
		language  string
		lifecycle string
	}{
		{"PHP", "__construct"},
		{"PHP", "__destruct"},
		{"PHP", "Example"}, // PHP 4 style constructor
		{"Python", "__init__"},
		{"Python", "__new__"},
		{"Python", "__del__"},
		{"TypeScript", "constructor"},
		{"Java", "Example"},
		{"Java", "finalize"},
		{"C#", "Example"}, // constructor, and destructor "~Example"
		{"C#", "Finalize"},
		{"Rust", "new"},
		{"Rust", "drop"},
		{"C++", "Example"},
		{"C++", "~Example"},
		{"", "__construct"}, // unknown language: generic policy
	}

	for _, testCase := range cases {
		t.Run(testCase.language+"/"+testCase.lifecycle, func(t *testing.T) {
			visitor := &LackOfCohesionOfMethodsVisitor{Language: testCase.language}
			stmts := classWith("Example",
				method("Example", testCase.lifecycle, "a", "b"),
				method("Example", "useA", "a"),
				method("Example", "useB", "b"),
			)

			visitor.Calculate(stmts)

			if got := lcom4Of(t, stmts); got != 2 {
				t.Errorf("Expected LCOM4=2, got %d", got)
			}
		})
	}
}

func TestALifecycleNameOfAnotherLanguageIsARegularMethod(t *testing.T) {
	// "new" is a constructor in Rust only: in PHP it is a method like any other,
	// and it does connect useA and useB.
	visitor := &LackOfCohesionOfMethodsVisitor{Language: "PHP"}
	stmts := classWith("Example",
		method("Example", "new", "a", "b"),
		method("Example", "useA", "a"),
		method("Example", "useB", "b"),
	)

	visitor.Calculate(stmts)

	if got := lcom4Of(t, stmts); got != 1 {
		t.Errorf("Expected LCOM4=1, got %d", got)
	}
}

func TestAMethodNamedAfterItsClassIsARegularMethodInGolang(t *testing.T) {
	visitor := &LackOfCohesionOfMethodsVisitor{Language: "Golang"}
	stmts := classWith("Example",
		method("Example", "Example", "a", "b"),
		method("Example", "useA", "a"),
		method("Example", "useB", "b"),
	)

	visitor.Calculate(stmts)

	if got := lcom4Of(t, stmts); got != 1 {
		t.Errorf("Expected LCOM4=1, got %d", got)
	}
}

func TestAClassWithoutMethodHasNoCohesionToMeasure(t *testing.T) {
	visitor := &LackOfCohesionOfMethodsVisitor{Language: "PHP"}
	stmts := classWith("Example")

	visitor.Calculate(stmts)

	if got := lcom4Of(t, stmts); got != 0 {
		t.Errorf("Expected LCOM4=0, got %d", got)
	}
}

func TestEmptyMethodsAreNotCountedAsComponents(t *testing.T) {
	visitor := &LackOfCohesionOfMethodsVisitor{Language: "PHP"}
	stmts := classWith("Example",
		method("Example", "useA", "a"),
		method("Example", "alsoUseA", "a"),
		method("Example", "stub"), // no attribute, no statement
	)

	visitor.Calculate(stmts)

	if got := lcom4Of(t, stmts); got != 1 {
		t.Errorf("Expected LCOM4=1, got %d", got)
	}
}

func TestAMethodWithoutMeasuredLinesOfCodeIsNotConsideredEmpty(t *testing.T) {
	// Parsers not filling LinesOfCode must not see all their methods dropped
	visitor := &LackOfCohesionOfMethodsVisitor{Language: "PHP"}
	useA := method("Example", "useA", "a")
	useA.LinesOfCode = nil
	useB := method("Example", "useB", "b")
	useB.LinesOfCode = nil
	stmts := classWith("Example", useA, useB)

	visitor.Calculate(stmts)

	if got := lcom4Of(t, stmts); got != 2 {
		t.Errorf("Expected LCOM4=2, got %d", got)
	}
}

func TestCallingAnExcludedMethodDoesNotConnectComponents(t *testing.T) {
	visitor := &LackOfCohesionOfMethodsVisitor{Language: "PHP"}
	useA := method("Example", "useA", "a")
	useA.MethodCalls = []*pb.StmtMethodCall{{Name: "this.stub"}}
	useB := method("Example", "useB", "b")
	useB.MethodCalls = []*pb.StmtMethodCall{{Name: "this.stub"}}
	stmts := classWith("Example", useA, useB, method("Example", "stub"))

	visitor.Calculate(stmts)

	if got := lcom4Of(t, stmts); got != 2 {
		t.Errorf("Expected LCOM4=2, got %d", got)
	}
}

func TestInternalCallsStillConnectComponents(t *testing.T) {
	visitor := &LackOfCohesionOfMethodsVisitor{Language: "PHP"}
	useA := method("Example", "useA", "a")
	useA.MethodCalls = []*pb.StmtMethodCall{{Name: "this.useB"}}
	stmts := classWith("Example", useA, method("Example", "useB", "b"))

	visitor.Calculate(stmts)

	if got := lcom4Of(t, stmts); got != 1 {
		t.Errorf("Expected LCOM4=1, got %d", got)
	}
}

func TestTheReceiverIsNotAnAttribute(t *testing.T) {
	// Some parsers report "self" / "this" as an operand: it must not be seen as an
	// attribute shared by every method of the class.
	visitor := &LackOfCohesionOfMethodsVisitor{Language: "Python"}
	stmts := classWith("Example",
		method("Example", "useA", "self", "a"),
		method("Example", "useB", "self", "b"),
	)

	visitor.Calculate(stmts)

	if got := lcom4Of(t, stmts); got != 2 {
		t.Errorf("Expected LCOM4=2, got %d", got)
	}
}

func TestShortNameFallsBackOnTheQualifiedName(t *testing.T) {
	cases := map[string]string{
		"Example::__construct":       "__construct",
		"App\\Example.constructor":   "constructor",
		"module\\Example::use_a":     "use_a",
		"Example.Example":            "Example",
		"withoutAnyScope":            "withoutAnyScope",
		"App\\Domain\\Example::useA": "useA",
	}

	for qualified, expected := range cases {
		if got := shortName(&pb.Name{Qualified: qualified}); got != expected {
			t.Errorf("shortName(%q) = %q, expected %q", qualified, got, expected)
		}
	}
}
