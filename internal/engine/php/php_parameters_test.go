package php

import (
	"strings"
	"testing"

	"github.com/ast-metrics/ast-metrics/internal/engine"
	"github.com/ast-metrics/ast-metrics/pb"
)

// assertParameters checks the parameters of the only method of the analyzed
// file, the count as much as the names: max_parameters_per_method reports the
// count, and a name counted twice is a parameter that does not exist.
func assertParameters(t *testing.T, src string, expected ...string) {
	t.Helper()

	r := &PhpRunner{}
	file, _ := engine.CreateTestFileWithCode(r, src)

	fn := onlyFunction(t, file)
	got := make([]string, 0, len(fn.Parameters))
	for _, p := range fn.Parameters {
		got = append(got, p.Name)
	}
	if strings.Join(got, ",") != strings.Join(expected, ",") {
		t.Errorf("Expected parameters [%s], got [%s]", strings.Join(expected, ", "), strings.Join(got, ", "))
	}
}

func onlyFunction(t *testing.T, file *pb.File) *pb.StmtFunction {
	t.Helper()

	if len(file.Stmts.StmtClass) > 0 {
		functions := file.Stmts.StmtClass[0].Stmts.StmtFunction
		if len(functions) != 1 {
			t.Fatalf("Expected one method, got %d", len(functions))
		}
		return functions[0]
	}
	if len(file.Stmts.StmtFunction) != 1 {
		t.Fatalf("Expected one function, got %d", len(file.Stmts.StmtFunction))
	}
	return file.Stmts.StmtFunction[0]
}

func Test_Promoted_Properties_Are_Counted_Once(t *testing.T) {
	assertParameters(t, `
<?php
class ValueObject {
    public function __construct(
        public Money $amount,
        public Currency $currency,
        private readonly string $label,
        protected int $precision,
    ) {}
}
`, "$amount", "$currency", "$label", "$precision")
}

// A promoted property with asymmetric visibility (PHP 8.4) carries two
// modifiers and one name, and must be counted once.
func Test_Asymmetric_Visibility_Does_Not_Inflate_The_Count(t *testing.T) {
	assertParameters(t, `
<?php
class ValueObject {
    public function __construct(
        public(set) string $name,
        public(set) int $age,
    ) {}
}
`, "$name", "$age")
}

func Test_Types_And_Default_Values_Are_Not_Parameters(t *testing.T) {
	assertParameters(t, `
<?php
function build(array $options = ['retries' => 3], ?Client $client = null, string ...$tags) {}
`, "$options", "$client", "$tags")
}

func Test_A_Method_Without_Parameters_Has_None(t *testing.T) {
	assertParameters(t, `
<?php
class Example {
    public function run() {}
}
`)
}
