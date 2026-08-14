package typescript_test

import (
	"slices"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/analyzer"
	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/internal/engine/typescript"
)

const sampleTsClass = `class Basket {
    private items: string[] = [];
    private owner: string;

    constructor(owner: string) {
        this.owner = owner;
    }

    add(item: string): number {
        this.items.push(item);
        return this.items.length;
    }

    count(): number {
        return this.items.length;
    }

    rename(label: string): void {
        this.owner = label;
        this.trace(label);
    }

    private trace(message: string): void {
        console.log(message);
    }
}
`

func Test_Ts_Class_Has_Lack_Of_Cohesion(t *testing.T) {
	file, err := engine.CreateTestFileWithCode(&typescript.TypeScriptRunner{}, sampleTsClass)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	analyzer.AnalyzeFile(file)

	if len(file.Stmts.StmtClass) != 1 {
		t.Fatalf("expected 1 class, got %d", len(file.Stmts.StmtClass))
	}
	class := file.Stmts.StmtClass[0]
	if class.Stmts.Analyze.ClassCohesion == nil || class.Stmts.Analyze.ClassCohesion.Lcom4 == nil {
		t.Fatalf("expected LCOM4 to be computed on the class Basket")
	}
	// add and count share the attribute items; constructor, rename and trace are
	// tied together by owner and by the call to trace. The methods typed the same
	// way ("number", "string", "void") do not create any other component.
	expected := int32(2)
	if *class.Stmts.Analyze.ClassCohesion.Lcom4 != expected {
		t.Fatalf("expected LCOM4=%d, got %d", expected, *class.Stmts.Analyze.ClassCohesion.Lcom4)
	}
}

func Test_Ts_Attribute_Access_Is_An_Operand_And_Not_A_Call(t *testing.T) {
	adapter := typescript.NewTreeSitterAdapter([]byte(sampleTsClass))
	src := []byte(sampleTsClass)

	// `rename(label: string): void {` ... on lines 17 to 20
	calls := adapter.ExtractMethodCalls(src, 17, 20)
	if !slices.Contains(calls, "this.trace") {
		t.Fatalf("expected the call this.trace to be reported, got %v", calls)
	}
	if slices.Contains(calls, "this.owner") {
		t.Fatalf("an attribute access is not a method call, got %v", calls)
	}

	_, operands := adapter.ExtractOperatorsOperands(src, 17, 20)
	if !slices.Contains(operands, "this.owner") {
		t.Fatalf("expected the attribute access this.owner to be an operand, got %v", operands)
	}
	if slices.Contains(operands, "this.trace") {
		t.Fatalf("a call on this is not an operand, got %v", operands)
	}
	// the type annotations of the signature are not operands
	for _, leaked := range []string{"string", "void"} {
		if slices.Contains(operands, leaked) {
			t.Fatalf("the type %q leaked into the operands: %v", leaked, operands)
		}
	}

	// `add(item: string): number {` ... on lines 9 to 12: the attribute is read
	// through a chain and through a call
	_, operands = adapter.ExtractOperatorsOperands(src, 9, 12)
	if !slices.Contains(operands, "this.items") {
		t.Fatalf("expected this.items.push() and this.items.length to read this.items, got %v", operands)
	}
}

func Test_Ts_Type_Annotations_Are_Not_Operands(t *testing.T) {
	src := []byte(`function convert(a: number, flag: boolean): number {
    const map = new Map<string, number>();
    const label: string = flag ? left : right;
    const currency = raw as Currency;
    if (a < threshold && a > floor) {
        return { mode: mode, total: a };
    }
}
`)
	adapter := typescript.NewTreeSitterAdapter(src)
	operators, operands := adapter.ExtractOperatorsOperands(src, 1, 8)

	// the types, wherever they stand (parameter, result, declaration, generic
	// argument, "as" cast), never leak into the operands
	for _, leaked := range []string{"number", "boolean", "string", "Currency"} {
		if slices.Contains(operands, leaked) {
			t.Errorf("the type %q leaked into the operands: %v", leaked, operands)
		}
	}
	// the names, the values and the object literal keys are operands
	for _, expected := range []string{"convert", "a", "flag", "map", "Map", "label", "left", "right", "currency", "raw", "threshold", "floor", "mode", "total"} {
		if !slices.Contains(operands, expected) {
			t.Errorf("expected the operand %q, got %v", expected, operands)
		}
	}
	// "<" and ">" are comparisons here, not generics
	for _, expected := range []string{"<", ">", "&&"} {
		if !slices.Contains(operators, expected) {
			t.Errorf("expected the operator %q, got %v", expected, operators)
		}
	}
}

func Test_Ts_Operands_Keep_Member_Access_Chains(t *testing.T) {
	src := []byte(`class Chains {
    run(message: string) {
        this.total = this.total + 1;
        console.log(message);
        const size = this.items.length;
        return this;
    }
}
`)
	adapter := typescript.NewTreeSitterAdapter(src)
	_, operands := adapter.ExtractOperatorsOperands(src, 2, 7)

	for _, expected := range []string{"this.total", "console.log", "message", "size", "this.items"} {
		if !slices.Contains(operands, expected) {
			t.Errorf("expected the operand %q, got %v", expected, operands)
		}
	}
	// a chain is a single operand: its segments are not reported on their own,
	// and a bare "this" does not tell which attribute is used
	for _, unwanted := range []string{"this", "total", "console", "log", "items", "length"} {
		if slices.Contains(operands, unwanted) {
			t.Errorf("did not expect the operand %q, got %v", unwanted, operands)
		}
	}
}
